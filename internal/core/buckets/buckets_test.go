package buckets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/command"
	"github.com/leobsst/frankenphp-cli/internal/command/commandtest"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/events"
	"github.com/leobsst/frankenphp-cli/internal/core/hosts"
	"github.com/leobsst/frankenphp-cli/internal/core/s3"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

const globalKey = "GKglobal"

type fakeBucket struct {
	id      string
	objects int64
	public  bool
	keys    map[string]s3.Perms // access key ID -> permissions
}

// fakeGarage is a stateful Garage admin API on a local test server: no
// network.
type fakeGarage struct {
	buckets map[string]*fakeBucket // by name
	keys    map[string]string      // access key ID -> name
	nextID  int
}

func (f *fakeGarage) byID(id string) (string, *fakeBucket) {
	for name, b := range f.buckets {
		if b.id == id {
			return name, b
		}
	}
	return "", nil
}

func (f *fakeGarage) bucketJSON(name string, b *fakeBucket) map[string]any {
	keys := []map[string]any{}
	for id, p := range b.keys {
		keys = append(keys, map[string]any{"accessKeyId": id, "name": f.keys[id], "permissions": p})
	}
	return map[string]any{"id": b.id, "globalAliases": []string{name}, "websiteAccess": b.public,
		"keys": keys, "objects": b.objects}
}

func (f *fakeGarage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var body map[string]any
	if r.Method == http.MethodPost {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	notFound := func() {
		w.WriteHeader(http.StatusNotFound)
		reply(map[string]string{"code": "NotFound", "message": "not found"})
	}
	switch strings.TrimPrefix(r.URL.Path, "/v2/") {
	case "ListBuckets":
		var items []map[string]string
		for _, b := range f.buckets {
			items = append(items, map[string]string{"id": b.id})
		}
		reply(items)
	case "GetBucketInfo":
		name, b := f.byID(q.Get("id"))
		if alias := q.Get("globalAlias"); alias != "" {
			name, b = alias, f.buckets[alias]
		}
		if b == nil {
			notFound()
			return
		}
		reply(f.bucketJSON(name, b))
	case "CreateBucket":
		f.nextID++
		name := body["globalAlias"].(string)
		f.buckets[name] = &fakeBucket{id: "b" + string(rune('0'+f.nextID)), keys: map[string]s3.Perms{}}
		reply(f.bucketJSON(name, f.buckets[name]))
	case "DeleteBucket":
		name, _ := f.byID(q.Get("id"))
		delete(f.buckets, name)
	case "UpdateBucket":
		_, b := f.byID(q.Get("id"))
		b.public = body["websiteAccess"].(map[string]any)["enabled"].(bool)
	case "AllowBucketKey":
		_, b := f.byID(body["bucketId"].(string))
		p := body["permissions"].(map[string]any)
		b.keys[body["accessKeyId"].(string)] = s3.Perms{Owner: p["owner"] == true, Read: p["read"] == true, Write: p["write"] == true}
	case "ListKeys":
		var items []map[string]string
		for id, name := range f.keys {
			items = append(items, map[string]string{"id": id, "name": name})
		}
		reply(items)
	case "CreateKey":
		id := "GK" + body["name"].(string)
		f.keys[id] = body["name"].(string)
		reply(map[string]string{"accessKeyId": id, "name": f.keys[id], "secretAccessKey": "secret"})
	case "GetKeyInfo":
		name, ok := f.keys[q.Get("id")]
		if !ok {
			notFound()
			return
		}
		reply(map[string]any{"accessKeyId": q.Get("id"), "name": name})
	case "DeleteKey":
		if _, ok := f.keys[q.Get("id")]; !ok {
			notFound()
			return
		}
		delete(f.keys, q.Get("id"))
	default:
		t := "unexpected endpoint " + r.URL.Path
		http.Error(w, t, http.StatusTeapot)
	}
}

type testService struct {
	*Service
	garage    *fakeGarage
	hostsFile string
}

// newService returns the service of a temporary data directory, on a fake
// Garage, writing a temporary hosts file.
func newService(t *testing.T, f *fakeGarage) *testService {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	hostsFile := filepath.Join(dir, "hosts")
	if err := os.WriteFile(hostsFile, []byte("127.0.0.1\tlocalhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := &commandtest.Runner{Handle: func(c command.Cmd) (command.Result, error) {
		if c.Name == "sudo" && c.Args[0] == "tee" {
			if err := os.WriteFile(c.Args[1], c.Stdin, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return command.Result{}, nil
	}}
	log := events.NewLog(nil)
	return &testService{
		Service: &Service{
			Dir: dir, Env: env.FromMap(map[string]string{s3.AccessKeyKey: globalKey}), Store: st,
			Hosts:  &hosts.Manager{Path: hostsFile, GOOS: "darwin", Run: run, Log: log, IsAdmin: func() bool { return false }},
			Garage: &s3.Garage{BaseURL: srv.URL, Token: "token", HTTP: srv.Client()},
			Log:    log,
		},
		garage: f, hostsFile: hostsFile,
	}
}

func newGarage() *fakeGarage {
	return &fakeGarage{buckets: map[string]*fakeBucket{}, keys: map[string]string{globalKey: s3.GlobalKeyName}}
}

// A new bucket is reachable with the global key and through its host; an
// existing one is left alone.
func TestCreateGrantsTheGlobalKeyAndAddsTheHosts(t *testing.T) {
	f := newGarage()
	f.buckets["media"] = &fakeBucket{id: "b0", keys: map[string]s3.Perms{}}
	x := newService(t, f)
	ctx := context.Background()

	created, err := x.Create(ctx, []string{"media", "docs"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(created, []string{"docs"}) {
		t.Errorf("created = %v, want [docs]", created)
	}
	docs := f.buckets["docs"]
	if docs.keys[globalKey] != (s3.Perms{Owner: true, Read: true, Write: true}) || !docs.public {
		t.Errorf("docs = %+v: want the global key as owner, and public", docs)
	}
	if f.buckets["media"].public {
		t.Error("the existing bucket was changed")
	}
	data, _ := os.ReadFile(x.hostsFile)
	for _, b := range []string{"docs", "media"} {
		if !strings.Contains(string(data), s3.BucketHost(b)) {
			t.Errorf("no hosts entry for %s:\n%s", s3.BucketHost(b), data)
		}
	}
	if cached, _ := x.Store.S3Buckets(); !slices.Equal(cached, []string{"docs", "media"}) {
		t.Errorf("cached buckets = %v", cached)
	}
}

// Deleting a bucket deletes its own keys, not the others, and its host.
func TestDeleteRemovesTheBucketKeysAndHost(t *testing.T) {
	f := newGarage()
	x := newService(t, f)
	ctx := context.Background()
	if _, err := x.Create(ctx, []string{"docs", "media"}, false); err != nil {
		t.Fatal(err)
	}
	for _, b := range []string{"docs", "media"} {
		if _, err := x.CreateKey(ctx, b, "app", false); err != nil {
			t.Fatal(err)
		}
	}

	if err := x.Delete(ctx, []string{"docs"}, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.buckets["docs"]; ok {
		t.Error("bucket not deleted")
	}
	if _, ok := f.keys["GKdocs/app"]; ok {
		t.Error("the key of the deleted bucket is left")
	}
	for _, id := range []string{"GKmedia/app", globalKey} {
		if _, ok := f.keys[id]; !ok {
			t.Errorf("key %s deleted", id)
		}
	}
	data, _ := os.ReadFile(x.hostsFile)
	if strings.Contains(string(data), s3.BucketHost("docs")) {
		t.Errorf("hosts entry of the deleted bucket left:\n%s", data)
	}
}

func TestDeleteRefusesANonEmptyBucketWithoutForce(t *testing.T) {
	f := newGarage()
	f.buckets["docs"] = &fakeBucket{id: "b0", objects: 3, keys: map[string]s3.Perms{}}
	x := newService(t, f)
	err := x.Delete(context.Background(), []string{"docs"}, false)
	if err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("err = %v, want not empty", err)
	}
	if _, ok := f.buckets["docs"]; !ok {
		t.Error("bucket deleted")
	}
}

// The global key comes from the configuration: it is listed as allowed on
// every bucket, and cannot be deleted.
func TestGlobalKey(t *testing.T) {
	f := newGarage()
	f.keys["GKother"] = "docs/app"
	x := newService(t, f)
	ctx := context.Background()

	keys, err := x.Keys(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if k.Global != (k.ID == globalKey) {
			t.Errorf("key %s: Global = %v", k.ID, k.Global)
		}
	}
	if err := x.DeleteKeys(ctx, []string{"GKother", globalKey}); err == nil {
		t.Fatal("global key deleted")
	}
	if len(f.keys) != 2 {
		t.Errorf("keys = %v: none must be deleted when the global key is asked", f.keys)
	}
}

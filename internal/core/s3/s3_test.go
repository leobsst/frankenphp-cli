package s3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The "GET Bucket (List Objects)" example of the AWS Signature Version 4
// documentation.
func TestSignMatchesAWSExample(t *testing.T) {
	o := &Objects{
		Region:    "us-east-1",
		AccessKey: "AKIAIOSFODNN7EXAMPLE",
		SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Now:       func() time.Time { return time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC) },
	}
	req, _ := http.NewRequest(http.MethodGet, "http://examplebucket.s3.amazonaws.com/?max-keys=2&prefix=J", nil)
	o.sign(req, "/", "max-keys=2&prefix=J", nil)
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;x-amz-content-sha256;x-amz-date, " +
		"Signature=34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7"
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization =\n%s\nwant\n%s", got, want)
	}
}

func TestValidateBucket(t *testing.T) {
	for _, name := range []string{"media", "my-app-2", "abc"} {
		if err := ValidateBucket(name); err != nil {
			t.Errorf("ValidateBucket(%q) = %v", name, err)
		}
	}
	// Dots would break the wildcard certificate of the bucket hosts
	for _, name := range []string{"ab", "Media", "my.bucket", "-media", "media-", "xn--media", strings.Repeat("a", 64)} {
		if ValidateBucket(name) == nil {
			t.Errorf("ValidateBucket(%q) accepted", name)
		}
	}
}

func TestGeneratedCredentialsMatchGarageFormats(t *testing.T) {
	if err := ValidateAccessKeyID(newValue(AccessKeyKey)); err != nil {
		t.Error(err)
	}
	for _, key := range []string{SecretKeyKey, RPCSecretKey} {
		if err := ValidateHexSecret(newValue(key)); err != nil {
			t.Errorf("%s: %v", key, err)
		}
	}
}

// fakeGarage answers the admin API endpoints with fixed data and records
// the POST bodies.
type fakeGarage struct {
	roles  int
	keys   map[string]bool
	posted map[string][]map[string]any
}

func (f *fakeGarage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer token" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	endpoint := strings.TrimPrefix(r.URL.Path, "/v2/")
	if r.Method == http.MethodPost {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.posted[endpoint] = append(f.posted[endpoint], body)
	}
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch endpoint {
	case "GetClusterLayout":
		roles := make([]map[string]string, f.roles)
		reply(map[string]any{"version": 3, "roles": roles})
	case "GetClusterStatus":
		reply(map[string]any{"layoutVersion": 3, "nodes": []map[string]any{{"id": "node1"}}})
	case "GetKeyInfo":
		if !f.keys[r.URL.Query().Get("id")] {
			w.WriteHeader(http.StatusNotFound)
			reply(map[string]string{"code": "NoSuchAccessKey", "message": "not found"})
			return
		}
		reply(map[string]any{"accessKeyId": r.URL.Query().Get("id")})
	case "ListBuckets":
		reply([]map[string]string{{"id": "b1"}, {"id": "b2"}})
	case "GetBucketInfo":
		b := map[string]any{"id": r.URL.Query().Get("id"), "globalAliases": []string{"media"}}
		if b["id"] == "b2" {
			b["globalAliases"] = []string{"docs"}
			b["keys"] = []map[string]any{{"accessKeyId": "GKglobal",
				"permissions": map[string]bool{"owner": true, "read": true, "write": true}}}
		}
		reply(b)
	default:
		reply(map[string]any{})
	}
}

func newFakeGarage(t *testing.T, f *fakeGarage) *Garage {
	f.posted = map[string][]map[string]any{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &Garage{BaseURL: srv.URL, Token: "token", HTTP: srv.Client()}
}

func TestEnsureLayoutAssignsTheNodeOnce(t *testing.T) {
	f := &fakeGarage{}
	g := newFakeGarage(t, f)
	if err := g.EnsureLayout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(f.posted["UpdateClusterLayout"]); n != 1 {
		t.Fatalf("UpdateClusterLayout called %d times", n)
	}
	role := f.posted["UpdateClusterLayout"][0]["roles"].([]any)[0].(map[string]any)
	if role["id"] != "node1" || role["zone"] == "" {
		t.Errorf("role = %v", role)
	}
	if v := f.posted["ApplyClusterLayout"][0]["version"]; v != float64(4) {
		t.Errorf("applied version %v, want 4", v)
	}

	// Already assigned: nothing changes
	f.roles, f.posted = 1, map[string][]map[string]any{}
	if err := g.EnsureLayout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.posted) != 0 {
		t.Errorf("layout changed again: %v", f.posted)
	}
}

func TestEnsureGlobalKeyImportsAndAllowsEveryBucket(t *testing.T) {
	f := &fakeGarage{keys: map[string]bool{}}
	g := newFakeGarage(t, f)
	if err := g.EnsureGlobalKey(context.Background(), "GKglobal", "secret"); err != nil {
		t.Fatal(err)
	}
	if imported := f.posted["ImportKey"]; len(imported) != 1 || imported[0]["accessKeyId"] != "GKglobal" {
		t.Errorf("ImportKey = %v", imported)
	}
	// b2 already grants every permission to the key: only b1 is allowed
	allowed := f.posted["AllowBucketKey"]
	if len(allowed) != 1 || allowed[0]["bucketId"] != "b1" {
		t.Errorf("AllowBucketKey = %v", allowed)
	}

	// A known key is not imported again
	f.keys["GKglobal"], f.posted = true, map[string][]map[string]any{}
	if err := g.EnsureGlobalKey(context.Background(), "GKglobal", "secret"); err != nil {
		t.Fatal(err)
	}
	if len(f.posted["ImportKey"]) != 0 {
		t.Error("known key imported again")
	}
}

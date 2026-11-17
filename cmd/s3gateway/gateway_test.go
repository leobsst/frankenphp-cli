package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/sigv4"
)

const (
	region       = "us-east-1"
	globalKey    = "GKglobal"
	globalSecret = "globalsecret"
	readerKey    = "GKreader"
	readerSecret = "readersecret"
)

// fakeGarage plays Garage's S3 API (signature presence only: Garage checks
// it for real), admin API and website endpoint.
type fakeGarage struct {
	mu      sync.Mutex
	objects map[string]string // bucket/key -> body
	types   map[string]string // bucket/key -> Content-Type
	webHost string            // Host of the last website request
	webPath string
}

func (f *fakeGarage) s3(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") == "" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/")
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		f.objects[path] = string(body)
		f.types[path] = r.Header.Get("Content-Type")
	case r.Method == http.MethodPost && q.Has("delete"):
		var req struct {
			Keys []string `xml:"Object>Key"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = xml.Unmarshal(body, &req)
		bucket, _, _ := strings.Cut(path, "/")
		for _, k := range req.Keys {
			delete(f.objects, bucket+"/"+k)
		}
		_, _ = io.WriteString(w, "<DeleteResult></DeleteResult>")
	case r.Method == http.MethodPost && q.Has("uploads"):
		_, _ = io.WriteString(w, "<InitiateMultipartUploadResult><UploadId>up1</UploadId></InitiateMultipartUploadResult>")
	case r.Method == http.MethodPost && q.Has("uploadId"):
		f.objects[path] = "assembled"
		_, _ = io.WriteString(w, "<CompleteMultipartUploadResult><Key>k</Key></CompleteMultipartUploadResult>")
	case r.Method == http.MethodDelete:
		delete(f.objects, path)
		w.WriteHeader(http.StatusNoContent)
	default:
		body, ok := f.objects[path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", f.types[path])
		_, _ = io.WriteString(w, body)
	}
}

func (f *fakeGarage) admin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer token" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	q := r.URL.Query()
	switch strings.TrimPrefix(r.URL.Path, "/v2/") {
	case "GetBucketInfo":
		switch q.Get("globalAlias") {
		case "media":
			reply(map[string]any{"id": "b1"})
		case "site":
			reply(map[string]any{"id": "b2", "websiteAccess": true})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	case "GetKeyInfo":
		perms := map[string]bool{"read": true, "write": true, "owner": true}
		secret := globalSecret
		switch q.Get("id") {
		case globalKey:
		case readerKey:
			perms, secret = map[string]bool{"read": true}, readerSecret
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		reply(map[string]any{"accessKeyId": q.Get("id"), "secretAccessKey": secret,
			"buckets": []map[string]any{{"id": "b1", "permissions": perms}}})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeGarage) web(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.webHost, f.webPath = r.Host, r.URL.Path
	f.mu.Unlock()
	_, _ = io.WriteString(w, "website")
}

// setup starts a fake Garage and a gateway in front of it.
func setup(t *testing.T) (*fakeGarage, *httptest.Server) {
	t.Helper()
	f := &fakeGarage{objects: map[string]string{}, types: map[string]string{}}
	s3 := httptest.NewServer(http.HandlerFunc(f.s3))
	admin := httptest.NewServer(http.HandlerFunc(f.admin))
	web := httptest.NewServer(http.HandlerFunc(f.web))
	t.Cleanup(func() { s3.Close(); admin.Close(); web.Close() })
	g, err := New(Config{
		S3URL: s3.URL, WebURL: web.URL, AdminURL: admin.URL, AdminToken: "token",
		AccessKey: globalKey, SecretKey: globalSecret, Region: region,
		RootDomain: "s3.frankenmanager.test", ACLDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(g)
	t.Cleanup(gw.Close)
	return f, gw
}

// do sends a request to the gateway, signed with key unless it is empty.
func do(t *testing.T, gw *httptest.Server, method, path, key string, header http.Header, body string) *http.Response {
	t.Helper()
	u, _ := url.Parse(gw.URL + path)
	req, err := http.NewRequest(method, u.String(), strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	if key != "" {
		secret := map[string]string{globalKey: globalSecret, readerKey: readerSecret}[key]
		c := sigv4.Credentials{AccessKey: key, SecretKey: secret, Region: region}
		sigv4.Sign(req, c, sigv4.EncodePath(u.Path), sigv4.CanonicalQuery(u.Query()),
			sigv4.SHA256Hex([]byte(body)), time.Now())
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

func acl(v string) http.Header { return http.Header{"X-Amz-Acl": {v}} }

func expect(t *testing.T, res *http.Response, status int) string {
	t.Helper()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != status {
		t.Fatalf("%s %s: HTTP %d, want %d: %s", res.Request.Method, res.Request.URL.Path, res.StatusCode, status, body)
	}
	return string(body)
}

func TestPublicObjectInAPrivateBucket(t *testing.T) {
	_, gw := setup(t)
	h := acl("public-read")
	h.Set("Content-Type", "image/png")
	expect(t, do(t, gw, http.MethodPut, "/media/img/a b.png", globalKey, h, "png"), 200)
	expect(t, do(t, gw, http.MethodPut, "/media/secret.txt", globalKey, acl("private"), "secret"), 200)

	res := do(t, gw, http.MethodGet, "/media/img/a%20b.png", "", nil, "")
	if body := expect(t, res, 200); body != "png" || res.Header.Get("Content-Type") != "image/png" {
		t.Errorf("public object: %q, %s", body, res.Header.Get("Content-Type"))
	}
	expect(t, do(t, gw, http.MethodGet, "/media/secret.txt", "", nil, ""), 403)
	expect(t, do(t, gw, http.MethodGet, "/media/missing.txt", "", nil, ""), 403)
	expect(t, do(t, gw, http.MethodGet, "/unknown/a.txt", "", nil, ""), 404)
	// The owner still reads the private one
	expect(t, do(t, gw, http.MethodGet, "/media/secret.txt", globalKey, nil, ""), 200)
}

func TestOverwriteWithoutACLMakesTheObjectPrivate(t *testing.T) {
	_, gw := setup(t)
	expect(t, do(t, gw, http.MethodPut, "/media/a.txt", globalKey, acl("public-read"), "v1"), 200)
	expect(t, do(t, gw, http.MethodGet, "/media/a.txt", "", nil, ""), 200)
	expect(t, do(t, gw, http.MethodPut, "/media/a.txt", globalKey, nil, "v2"), 200)
	expect(t, do(t, gw, http.MethodGet, "/media/a.txt", "", nil, ""), 403)
}

func TestVirtualHostStyle(t *testing.T) {
	_, gw := setup(t)
	put := do(t, gw, http.MethodPut, "/media/a.txt", globalKey, acl("public-read"), "v1")
	expect(t, put, 200)
	req, _ := http.NewRequest(http.MethodGet, gw.URL+"/a.txt", nil)
	req.Host = "media.s3.frankenmanager.test"
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if body := expect(t, res, 200); body != "v1" {
		t.Errorf("body %q", body)
	}
}

func TestObjectACLRoundTrip(t *testing.T) {
	_, gw := setup(t)
	expect(t, do(t, gw, http.MethodPut, "/media/a.txt", globalKey, nil, "v1"), 200)

	expect(t, do(t, gw, http.MethodPut, "/media/a.txt?acl", globalKey, acl("public-read"), ""), 200)
	expect(t, do(t, gw, http.MethodGet, "/media/a.txt", "", nil, ""), 200)
	body := expect(t, do(t, gw, http.MethodGet, "/media/a.txt?acl", globalKey, nil, ""), 200)
	if !strings.Contains(body, allUsers) || !strings.Contains(body, `xsi:type="Group"`) {
		t.Errorf("GetObjectAcl of a public object:\n%s", body)
	}

	// The XML body form, as sent by setVisibility() with grants
	policy := `<AccessControlPolicy><AccessControlList><Grant><Grantee><ID>x</ID></Grantee>` +
		`<Permission>FULL_CONTROL</Permission></Grant></AccessControlList></AccessControlPolicy>`
	expect(t, do(t, gw, http.MethodPut, "/media/a.txt?acl", globalKey, nil, policy), 200)
	expect(t, do(t, gw, http.MethodGet, "/media/a.txt", "", nil, ""), 403)
	body = expect(t, do(t, gw, http.MethodGet, "/media/a.txt?acl", globalKey, nil, ""), 200)
	if strings.Contains(body, allUsers) {
		t.Errorf("GetObjectAcl of a private object:\n%s", body)
	}

	expect(t, do(t, gw, http.MethodPut, "/media/missing.txt?acl", globalKey, acl("public-read"), ""), 404)
}

func TestObjectACLChecksTheSignatureAndThePermission(t *testing.T) {
	_, gw := setup(t)
	expect(t, do(t, gw, http.MethodPut, "/media/a.txt", globalKey, nil, "v1"), 200)

	// A read-only key reads the ACL, but cannot change it
	expect(t, do(t, gw, http.MethodGet, "/media/a.txt?acl", readerKey, nil, ""), 200)
	expect(t, do(t, gw, http.MethodPut, "/media/a.txt?acl", readerKey, acl("public-read"), ""), 403)

	// A forged signature
	res := do(t, gw, http.MethodPut, "/media/a.txt?acl", globalKey, acl("private"), "")
	expect(t, res, 200)
	req, _ := http.NewRequest(http.MethodPut, gw.URL+"/media/a.txt?acl", nil)
	req.Header = res.Request.Header.Clone()
	req.Header.Set("X-Amz-Acl", "public-read") // signed header changed
	forged, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = forged.Body.Close() }()
	expect(t, forged, 403)
	expect(t, do(t, gw, http.MethodGet, "/media/a.txt", "", nil, ""), 403)
}

func TestDeletesForgetTheACL(t *testing.T) {
	f, gw := setup(t)
	for _, k := range []string{"a", "b", "c"} {
		expect(t, do(t, gw, http.MethodPut, "/media/"+k, globalKey, acl("public-read"), k), 200)
	}
	expect(t, do(t, gw, http.MethodDelete, "/media/a", globalKey, nil, ""), 204)
	del := `<Delete><Quiet>true</Quiet><Object><Key>b</Key></Object></Delete>`
	expect(t, do(t, gw, http.MethodPost, "/media?delete", globalKey, nil, del), 200)

	// Recreated behind the gateway's back: no ACL left over
	f.mu.Lock()
	f.objects["media/a"], f.objects["media/b"] = "a", "b"
	f.mu.Unlock()
	expect(t, do(t, gw, http.MethodGet, "/media/a", "", nil, ""), 403)
	expect(t, do(t, gw, http.MethodGet, "/media/b", "", nil, ""), 403)
	expect(t, do(t, gw, http.MethodGet, "/media/c", "", nil, ""), 200)
}

func TestMultipartUploadKeepsItsACL(t *testing.T) {
	_, gw := setup(t)
	expect(t, do(t, gw, http.MethodPost, "/media/big.bin?uploads", globalKey, acl("public-read"), ""), 200)
	expect(t, do(t, gw, http.MethodPut, "/media/big.bin?partNumber=1&uploadId=up1", globalKey, nil, "part"), 200)
	complete := `<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>x</ETag></Part></CompleteMultipartUpload>`
	expect(t, do(t, gw, http.MethodPost, "/media/big.bin?uploadId=up1", globalKey, nil, complete), 200)
	if body := expect(t, do(t, gw, http.MethodGet, "/media/big.bin", "", nil, ""), 200); body != "assembled" {
		t.Errorf("body %q", body)
	}
}

func TestPublicBucketGoesToTheWebsiteEndpoint(t *testing.T) {
	f, gw := setup(t)
	if body := expect(t, do(t, gw, http.MethodGet, "/site/docs/", "", nil, ""), 200); body != "website" {
		t.Errorf("body %q", body)
	}
	if f.webHost != "site.s3.frankenmanager.test" || f.webPath != "/docs/" {
		t.Errorf("website request: host %s, path %s", f.webHost, f.webPath)
	}
}

func TestAnonymousWritesReachGarage(t *testing.T) {
	_, gw := setup(t)
	// Garage denies them
	expect(t, do(t, gw, http.MethodPut, "/media/a.txt", "", acl("public-read"), "x"), 403)
}

func TestGetObjectACLXML(t *testing.T) {
	_, gw := setup(t)
	expect(t, do(t, gw, http.MethodPut, "/media/a.txt", globalKey, acl("public-read"), "x"), 200)
	body := expect(t, do(t, gw, http.MethodGet, "/media/a.txt?acl", globalKey, nil, ""), 200)
	var p struct {
		Grants []struct {
			URI        string `xml:"Grantee>URI"`
			Permission string `xml:"Permission"`
		} `xml:"AccessControlList>Grant"`
	}
	if err := xml.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprint(p.Grants)
	if !bytes.Contains([]byte(body), []byte(`xmlns="http://s3.amazonaws.com/doc/2006-03-01/"`)) ||
		got != "[{ FULL_CONTROL} {"+allUsers+" READ}]" {
		t.Errorf("grants %s in\n%s", got, body)
	}
}

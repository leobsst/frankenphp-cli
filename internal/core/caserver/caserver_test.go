package caserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestHandler(t *testing.T) {
	cert := []byte("-----BEGIN CERTIFICATE-----\n...\n")
	h := Handler(cert)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/any/path?x=1", nil))
		res := rec.Result()
		if res.StatusCode != http.StatusOK ||
			res.Header.Get("Content-Type") != "application/x-x509-ca-cert" ||
			res.Header.Get("Content-Disposition") != `attachment; filename="mkcert-rootCA.pem"` ||
			res.Header.Get("Content-Length") != strconv.Itoa(len(cert)) {
			t.Errorf("%s: %d %v", method, res.StatusCode, res.Header)
		}
		want := string(cert)
		if method == http.MethodHead {
			want = ""
		}
		if rec.Body.String() != want {
			t.Errorf("%s body = %q", method, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("POST = %d", rec.Code)
	}
}

func TestRunningState(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, ".ca_server.state")
	if _, _, ok := RunningState(dir); ok {
		t.Error("running without state")
	}

	for _, content := range []string{"garbage", "999999999 9080"} { // unreadable, dead
		if err := os.WriteFile(state, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, ok := RunningState(dir); ok {
			t.Errorf("%q reported running", content)
		}
		if _, err := os.Stat(state); err == nil {
			t.Errorf("%q state file kept", content)
		}
	}
	if StopSharing(dir) {
		t.Error("StopSharing stopped something")
	}

	// A live process (this test) is running; StartSharing refuses
	if err := os.WriteFile(state, []byte(strconv.Itoa(os.Getpid())+" 9081\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pid, port, ok := RunningState(dir)
	if !ok || pid != os.Getpid() || port != 9081 {
		t.Errorf("state = %d %d %v", pid, port, ok)
	}
	if _, err := StartSharing(dir, "/nonexistent", "/nonexistent.pem", 9081); err == nil ||
		!strings.HasPrefix(err.Error(), "Root CA sharing is already running.") {
		t.Errorf("err = %v", err)
	}
}

func TestStartSharingWithoutCert(t *testing.T) {
	dir := t.TempDir()
	_, err := StartSharing(dir, "/nonexistent", filepath.Join(dir, "rootCA.pem"), DefaultPort)
	if err == nil || !strings.HasPrefix(err.Error(), "Root CA not found at ") {
		t.Errorf("err = %v", err)
	}
}

package daemon

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/core/access"
)

// The health tells the API version and the features, and nothing else,
// remotely without a session too.
func TestHealthFeatures(t *testing.T) {
	allow, _ := access.ParseAllowList("192.168.1.0/24")
	s := authServer(t, AccessConfig{Mode: access.LAN, Allow: allow})
	h := s.remoteGuard(s.routes())
	w := remoteRequest(h, http.MethodGet, "/api/health", "192.168.1.5", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("health: %d", w.Code)
	}
	var res map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if len(res) != 3 || res["version"] != "1.2.3" || res["api"] != "v1" {
		t.Errorf("health = %s", w.Body)
	}
	features, _ := res["features"].([]any)
	has := map[any]bool{}
	for _, f := range features {
		has[f] = true
	}
	// No pages nor update lookup in this server
	if !has["pairing"] || !has["docker"] || has["dashboard"] || has["update"] {
		t.Errorf("features = %v", features)
	}
}

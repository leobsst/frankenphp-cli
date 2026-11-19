package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// The PATH of launchd lacks Homebrew: it is added after the given entries,
// each directory once.
func TestToolPath(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS directories")
	}
	sep := string(os.PathListSeparator)
	got := filepath.SplitList(ToolPath("/custom/bin" + sep + "/usr/bin" + sep + "/bin"))
	if got[0] != "/custom/bin" || !slices.Contains(got, "/opt/homebrew/bin") || !slices.Contains(got, "/usr/local/bin") {
		t.Errorf("ToolPath = %v", got)
	}
	seen := map[string]bool{}
	for _, d := range got {
		if seen[d] {
			t.Errorf("%s twice in %v", d, got)
		}
		seen[d] = true
	}
}

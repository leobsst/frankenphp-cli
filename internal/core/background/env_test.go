package background

import (
	"strings"
	"testing"
)

// The service manager starts the process with a minimal PATH: the spec's
// environment goes into the LaunchAgent and the systemd unit.
func TestServiceEnvironment(t *testing.T) {
	spec := Spec{Label: "dev.test", Unit: "test.service", LogFile: "test.log",
		Env: map[string]string{"PATH": "/opt/homebrew/bin:/usr/bin"}}
	plist := string(LaunchdPlist(spec, "/data", []string{"/bin/fm", "daemon", "run"}, ""))
	want := "\t<key>EnvironmentVariables</key>\n\t<dict>\n\t\t<key>PATH</key>\n\t\t<string>/opt/homebrew/bin:/usr/bin</string>\n\t</dict>\n"
	if !strings.Contains(plist, want) {
		t.Errorf("plist:\n%s", plist)
	}
	unit := SystemdUnitContent(spec, "/data", []string{"/bin/fm", "daemon", "run"})
	if !strings.Contains(unit, "Environment=\"PATH=/opt/homebrew/bin:/usr/bin\"\nExecStart=") {
		t.Errorf("unit:\n%s", unit)
	}
	// Without Env, nothing changes (the DNS watcher's files stay as released)
	spec.Env = nil
	if strings.Contains(string(LaunchdPlist(spec, "/data", []string{"/bin/fm"}, "")), "EnvironmentVariables") ||
		strings.Contains(SystemdUnitContent(spec, "/data", []string{"/bin/fm"}), "Environment=") {
		t.Error("environment written without Env")
	}
}

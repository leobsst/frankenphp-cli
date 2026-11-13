package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestConfigurationSetAndReset(t *testing.T) {
	e := newEnv(t)

	code, _, errOut := e.run("configuration", "set", "PHP_VERSION", "8.4")
	if code != ExitUsage || !strings.Contains(errOut, "Unknown setting 'PHP_VERSION'. Did you mean: DEFAULT_PHP_VERSION?") {
		t.Errorf("unknown: code %d, err %q", code, errOut)
	}
	code, out, _ := e.run("configuration", "set", "DEFAULT_PHP_VERSION", "7.4")
	if code != ExitError || !strings.Contains(out, "Invalid value '7.4' for DEFAULT_PHP_VERSION") {
		t.Errorf("invalid: code %d, out %q", code, out)
	}

	// Case-insensitive name, applied by the next command
	if code, out, _ := e.run("config", "set", "default_php_version", "8.4"); code != 0 || out != "-- DEFAULT_PHP_VERSION = 8.4\n" {
		t.Errorf("set: code %d, out %q", code, out)
	}
	if got := e.envValue("DEFAULT_PHP_VERSION"); got != "8.4" {
		t.Errorf("stored %q", got)
	}
	if _, out, _ := e.run("config", "set", "DEFAULT_PHP_VERSION", "8.4"); out != "-- DEFAULT_PHP_VERSION = 8.4 (unchanged)\n" {
		t.Errorf("unchanged: %q", out)
	}
	if _, out, _ := e.run("config", "reset", "DEFAULT_PHP_VERSION"); out != "-- DEFAULT_PHP_VERSION = 8.3\n" {
		t.Errorf("reset: %q", out)
	}
	if e.loadEnv().IsSet("DEFAULT_PHP_VERSION") {
		t.Error("still set after reset")
	}
}

// A setting the running server cannot pick up by itself tells how to apply
// it; nothing is restarted.
func TestConfigurationSetNeedsRestart(t *testing.T) {
	e := newEnv(t)
	if err := e.store().AddDomains([]string{"app.test"}, "8.4"); err != nil {
		t.Fatal(err)
	}
	e.docker.ps = running
	code, out, _ := e.run("config", "set", "WEB_HTTPS_PORT", "8443")
	if code != 0 || !strings.HasSuffix(out, "WARNING: The server is running: apply the change with "+
		"'frankenmanager stop && frankenmanager start'.\n") {
		t.Errorf("code %d, out %q", code, out)
	}
	if !slices.Equal(e.dockerLines(), []string{"ps --all --no-trunc --format json"}) {
		t.Errorf("docker calls: %v", e.dockerLines())
	}
}

// On a running server, a service starts and stops alone; stopped, it waits
// for the next start. configuration set does the same as service.
func TestServiceToggleLive(t *testing.T) {
	e := newEnv(t)
	e.initEnv("MARIADB_ROOT_PASSWORD=secret\n")

	code, out, _ := e.run("service", "enable", "redis")
	if code != 0 || out != "-- Service redis enabled. It applies on the next start.\n" || e.envValue("REDIS_ENABLED") != "true" {
		t.Errorf("stopped: code %d, out %q", code, out)
	}
	if _, out, _ := e.run("service", "enable", "redis"); out != "-- Service redis is already enabled.\n" {
		t.Errorf("again: %q", out)
	}

	if err := e.store().AddDomains([]string{"app.test"}, "8.4"); err != nil {
		t.Fatal(err)
	}
	e.docker.ps = running
	e.docker.calls = nil
	if code, out, _ := e.run("config", "set", "REDIS_ENABLED", "false"); code != 0 || !strings.Contains(out, "-- Service redis disabled.\n") {
		t.Errorf("disable: code %d, out %q", code, out)
	}
	if code, out, _ := e.run("service", "enable", "redis"); code != 0 || !strings.Contains(out, "-- Service redis enabled.\n") {
		t.Errorf("enable: code %d, out %q", code, out)
	}
	want := []string{
		"ps --all --no-trunc --format json",
		"stop franken_redis", "rm franken_redis",
		"ps --all --no-trunc --format json",
		"up -d --no-deps cache",
	}
	if !slices.Equal(e.dockerLines(), want) {
		t.Errorf("docker calls:\n%s", strings.Join(e.dockerLines(), "\n"))
	}

	if code, _, errOut := e.run("service", "enable", "mysql"); code != ExitUsage || !strings.Contains(errOut, "Unknown service 'mysql'") {
		t.Errorf("unknown service: code %d, err %q", code, errOut)
	}
}

// The completion scripts complete the alias too: the lines appended to
// cobra's scripts must keep matching its function names.
func TestCompletionScriptRegistersAlias(t *testing.T) {
	root := &cobra.Command{Use: "frankenmanager"}
	root.AddCommand(&cobra.Command{Use: "start", Run: func(*cobra.Command, []string) {}})
	for shell, wants := range map[string][]string{
		"bash":       {"__start_frankenmanager()", "complete -o default -F __start_frankenmanager fk\n"},
		"zsh":        {"#compdef frankenmanager fk\n", "_frankenmanager()", "compdef _frankenmanager fk\n"},
		"fish":       {"complete -c fk -w frankenmanager\n"},
		"powershell": {"${__frankenmanagerCompleterBlock} =", "-CommandName 'fk' -ScriptBlock ${__frankenmanagerCompleterBlock}\n"},
	} {
		script, err := completionScript(root, shell, []string{"fk"})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(script, want) {
				t.Errorf("%s: missing %q", shell, want)
			}
		}
	}
}

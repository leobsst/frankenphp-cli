package soketi

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// memConfig is an in-memory .env.
type memConfig map[string]string

func (m memConfig) Get(key string) string { return m[key] }

func (m memConfig) Set(key, value string) error { m[key] = value; return nil }

func (m memConfig) Require(key string) (string, error) {
	if m[key] == "" {
		return "", errors.New("missing " + key)
	}
	return m[key], nil
}

func TestDisabledByDefault(t *testing.T) {
	c := memConfig{}
	if Enabled(c) || Target(c) != "" || Domains(c) != nil {
		t.Error("Soketi enabled without SOKETI_ENABLED")
	}
	if generated, _ := EnsureCredentials(c); generated != nil || len(c) != 0 {
		t.Errorf("credentials generated while disabled: %v", c)
	}
	if vars, _ := ComposeEnvVars(c, ""); vars != nil {
		t.Errorf("compose vars while disabled: %v", vars)
	}
}

func TestEnabled(t *testing.T) {
	c := memConfig{"SOKETI_ENABLED": " TRUE ", "SOKETI_PORT": " "}
	if !Enabled(c) || Port(c) != DefaultPort || Target(c) != "127.0.0.1:6001" {
		t.Errorf("enabled=%v port=%q target=%q", Enabled(c), Port(c), Target(c))
	}
	if !slices.Equal(Domains(c), []string{Domain}) {
		t.Errorf("Domains = %v", Domains(c))
	}
}

func TestEnsureCredentialsFillsOnlyEmptyOnes(t *testing.T) {
	c := memConfig{"SOKETI_ENABLED": "true", AppKeyKey: "kept", AppSecretKey: "  "}
	generated, err := EnsureCredentials(c)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(generated, []string{AppIDKey, AppSecretKey}) {
		t.Errorf("generated = %v", generated)
	}
	if c[AppIDKey] != DefaultAppID || c[AppKeyKey] != "kept" {
		t.Errorf("credentials = %v", c)
	}
	secret := c[AppSecretKey]
	if len(secret) != 32 || strings.Trim(secret, "abcdefghijklmnopqrstuvwxyz0123456789") != "" {
		t.Errorf("secret %q is not 32 characters of [a-z0-9]", secret)
	}
}

func TestComposeEnvVars(t *testing.T) {
	c := memConfig{"SOKETI_ENABLED": "true", "SOKETI_PORT": "7000",
		AppIDKey: "a", AppKeyKey: "k", AppSecretKey: "s"}
	vars, err := ComposeEnvVars(c, "127.0.0.1:")
	if err != nil {
		t.Fatal(err)
	}
	if vars["SOKETI_PORT"] != "7000" || vars["SOKETI_PUBLISH"] != "127.0.0.1:7000:6001" ||
		vars[AppIDKey] != "a" || vars[AppKeyKey] != "k" || vars[AppSecretKey] != "s" || len(vars) != 5 {
		t.Errorf("vars = %v", vars)
	}

	delete(c, AppSecretKey)
	if _, err := ComposeEnvVars(c, ""); err == nil {
		t.Error("missing credential accepted")
	}
}

// Line for line, the soketi service earlier releases wrote.
func TestService(t *testing.T) {
	head := `  soketi:
    container_name: franken_soketi
    image: quay.io/soketi/soketi:1.6-16-alpine
    restart: always
    environment:
      SOKETI_DEFAULT_APP_ID: ${SOKETI_DEFAULT_APP_ID}
      SOKETI_DEFAULT_APP_KEY: ${SOKETI_DEFAULT_APP_KEY}
      SOKETI_DEFAULT_APP_SECRET: ${SOKETI_DEFAULT_APP_SECRET}
`
	tail := `    healthcheck:
      test: ["CMD-SHELL", "wget -q -O /dev/null http://127.0.0.1:$${SOKETI_PORT}/ready"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 10s
`
	prod := head + `      SOKETI_PORT: '${SOKETI_PORT:-6001}'
    network_mode: host
` + tail
	dev := head + `      SOKETI_PORT: '6001'
    ports:
      - ${SOKETI_PUBLISH:-127.0.0.1:6001:6001}
    networks:
      - franken_network
` + tail

	if got := strings.Join(Service(true), "\n"); got != prod {
		t.Errorf("production:\n%s\nwant\n%s", got, prod)
	}
	if got := strings.Join(Service(false), "\n"); got != dev {
		t.Errorf("development:\n%s\nwant\n%s", got, dev)
	}
}

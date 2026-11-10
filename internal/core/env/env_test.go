package env

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// Each case states what earlier releases read from the line: existing .env
// files must keep the same meaning.
func TestParseMatchesReference(t *testing.T) {
	content := "APP_ENV=dev\r\n" +
		"  # comment line\n" +
		"\n" +
		"no_equals_sign\n" +
		"UID=\n" +
		"  SPACED_KEY  =  value with spaces  \n" +
		"QUOTED=\"double\"\n" +
		"SINGLE='single'\n" +
		"MIXED=\"'both'\"\n" +
		"export EXPORTED=1\n" +
		"INLINE=value # not a comment\n" +
		"EQUALS=a=b=c\n" +
		"DUP=first\n" +
		"DUP=second\n" +
		"OLD_MAC=cr\r" +
		"EXPAND=${HOME}/x\n"

	want := map[string]string{
		"APP_ENV":         "dev",
		"UID":             "",
		"SPACED_KEY":      "value with spaces",
		"QUOTED":          "double",
		"SINGLE":          "single",
		"MIXED":           "both",
		"export EXPORTED": "1",                     // the "export" prefix stays in the key
		"INLINE":          "value # not a comment", // no inline comment stripping
		"EQUALS":          "a=b=c",                 // split on the first "=" only
		"DUP":             "second",
		"OLD_MAC":         "cr",
		"EXPAND":          "${HOME}/x", // no variable expansion
	}
	if got := Parse(content); !reflect.DeepEqual(got, want) {
		t.Errorf("Parse() =\n%v\nwant\n%v", got, want)
	}
}

func newEnv(t *testing.T, content string) *Env {
	t.Helper()
	dir := t.TempDir()
	e := New(filepath.Join(dir, ".env"), filepath.Join(dir, ".env.example"))
	if content != "" {
		if err := os.WriteFile(e.Path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	return e
}

func fileContent(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLoadMissingFile(t *testing.T) {
	e := newEnv(t, "")
	if len(e.Vars()) != 0 {
		t.Errorf("vars = %v, want none", e.Vars())
	}
	if !reflect.DeepEqual(e.DBEngines(), []string{"mariadb"}) {
		t.Errorf("DBEngines() = %v", e.DBEngines())
	}
}

func TestSet(t *testing.T) {
	cases := []struct {
		name, before, key, value, after string
	}{
		{"replaces the first match only", "# head\nA=1\nB=2\nA=3\n", "A", "x", "# head\nA=x\nB=2\nA=3\n"},
		{"matches after trimming", "  A=1\n", "A", "x", "A=x\n"},
		{"needs KEY= exactly", "A =1\nAB=2\n", "A", "x", "A =1\nAB=2\nA=x\n"},
		{"appends and adds the final newline", "B=2", "A", "x", "B=2\nA=x\n"},
		{"normalizes CRLF", "A=1\r\nB=2\r\n", "B", "x", "A=1\nB=x\n"},
		{"keeps blank lines", "A=1\n\n\nB=2\n", "A", "x", "A=x\n\n\nB=2\n"},
	}
	for _, c := range cases {
		e := newEnv(t, c.before)
		if err := e.Set(c.key, c.value); err != nil {
			t.Fatal(err)
		}
		if got := fileContent(t, e.Path); got != c.after {
			t.Errorf("%s: file = %q, want %q", c.name, got, c.after)
		}
		if e.Get(c.key) != c.value {
			t.Errorf("%s: Get(%s) = %q", c.name, c.key, e.Get(c.key))
		}
	}
}

func TestSetCreatesMissingFile(t *testing.T) {
	e := newEnv(t, "")
	if err := e.Set("A", "1"); err != nil {
		t.Fatal(err)
	}
	if got := fileContent(t, e.Path); got != "A=1\n" {
		t.Errorf("file = %q", got)
	}
}

func TestEnsureExists(t *testing.T) {
	e := newEnv(t, "")
	if _, err := e.EnsureExists(); err == nil || err.Error() != "Missing "+e.ExamplePath {
		t.Errorf("without .env.example: err = %v", err)
	}

	example := "# Set to your user\nUID=\nGID=\nOTHER_UID=\n"
	if err := os.WriteFile(e.ExamplePath, []byte(example), 0o644); err != nil {
		t.Fatal(err)
	}
	existed, err := e.EnsureExists()
	if err != nil || existed {
		t.Fatalf("existed=%v err=%v, want false, nil", existed, err)
	}

	uid, gid := "1000", "1000"
	if runtime.GOOS != "windows" {
		uid, gid = strconv.Itoa(os.Getuid()), strconv.Itoa(os.Getgid())
	}
	want := "# Set to your user\nUID=" + uid + "\nGID=" + gid + "\nOTHER_UID=\n"
	if got := fileContent(t, e.Path); got != want {
		t.Errorf(".env = %q, want %q", got, want)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(e.Path); info.Mode().Perm() != 0o660 {
			t.Errorf("mode = %v, want 0660", info.Mode().Perm())
		}
	}

	if existed, err := e.EnsureExists(); err != nil || !existed {
		t.Errorf("second call: existed=%v err=%v, want true, nil", existed, err)
	}
}

func TestRequire(t *testing.T) {
	e := newEnv(t, "SET=1\nEMPTY=\n")
	if v, err := e.Require("SET"); err != nil || v != "1" {
		t.Errorf("Require(SET) = %q, %v", v, err)
	}
	for _, key := range []string{"EMPTY", "MISSING"} {
		_, err := e.Require(key)
		if err == nil || err.Error() != "Required environment variable '"+key+"' is not set" {
			t.Errorf("Require(%s) err = %v", key, err)
		}
	}
}

func TestGeneratePassword(t *testing.T) {
	p := GeneratePassword()
	if len(p) != 32 || strings.Trim(p, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") != "" {
		t.Errorf("password %q is not 32 alphanumeric characters", p)
	}
	if GeneratePassword() == p {
		t.Error("two identical passwords")
	}
}

func TestExtraPackages(t *testing.T) {
	e := newEnv(t, "EXTRA_APK_PACKAGES=git  vim\nEXTRA_APK_PACKAGES_84=vim htop\n")
	if got := e.ExtraPackages("8.4", "APK"); got != "git vim htop" {
		t.Errorf("8.4 = %q", got)
	}
	if got := e.ExtraPackages("8.3", "APK"); got != "git vim" {
		t.Errorf("8.3 = %q", got)
	}
	if got := e.ExtraPackages("8.3", "APT"); got != "" {
		t.Errorf("APT = %q", got)
	}
}

func TestExtraSystemPackages(t *testing.T) {
	alpine := newEnv(t, "EXTRA_APK_PACKAGES=git\nEXTRA_APT_PACKAGES=curl\n")
	pkgs, warning, err := alpine.ExtraSystemPackages("8.3")
	if err != nil || pkgs != "git" || !strings.HasPrefix(warning, "EXTRA_APT_PACKAGES is ignored on Alpine") {
		t.Errorf("alpine: %q, %q, %v", pkgs, warning, err)
	}

	trixie := newEnv(t, "PHP_IMAGE_DISTRO= Trixie \nEXTRA_APK_PACKAGES=git\nEXTRA_APT_PACKAGES=curl\n")
	pkgs, warning, err = trixie.ExtraSystemPackages("8.3")
	if err != nil || pkgs != "curl" || warning != "" {
		t.Errorf("trixie: %q, %q, %v", pkgs, warning, err)
	}

	bad := newEnv(t, "PHP_IMAGE_DISTRO=debian\n")
	if _, _, err := bad.ExtraSystemPackages("8.3"); err == nil ||
		err.Error() != "Unsupported PHP_IMAGE_DISTRO 'debian'. Supported: alpine, trixie" {
		t.Errorf("debian: err = %v", err)
	}
}

func TestFlags(t *testing.T) {
	cases := []struct {
		content string
		check   func(*Env) bool
		want    bool
	}{
		{"APP_ENV=prod", (*Env).IsProduction, true},
		{"APP_ENV=production", (*Env).IsProduction, true},
		{"APP_ENV=Prod", (*Env).IsProduction, false},
		{"", (*Env).IsProduction, false},
		{"REDIS_ENABLED= TRUE ", (*Env).RedisEnabled, true},
		{"DNS_SERVER=True", (*Env).DNSServerEnabled, false},
		{"DNS_SERVER=true", (*Env).DNSServerEnabled, true},
		{"INSTALL_PDFTK=TRUE", (*Env).PdftkEnabled, false},
		{"SOKETI_ENABLED=True", (*Env).SoketiEnabled, true},
	}
	for _, c := range cases {
		if got := c.check(newEnv(t, c.content)); got != c.want {
			t.Errorf("%q: got %v, want %v", c.content, got, c.want)
		}
	}
}

func TestDBEngines(t *testing.T) {
	cases := map[string][]string{
		"":                                   {"mariadb"},
		"DB_ENGINES=":                        {"mariadb"},
		"DB_ENGINES=bogus":                   {"mariadb"},
		"DB_ENGINES= PostgreSQL , mysql,,x ": {"postgresql", "mysql"},
		"DB_ENGINES=mariadb,mariadb":         {"mariadb", "mariadb"},
	}
	for content, want := range cases {
		if got := newEnv(t, content).DBEngines(); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %v, want %v", content, got, want)
		}
	}
}

func TestDBAdminTarget(t *testing.T) {
	cases := map[string]string{
		"":                                       "127.0.0.1:8080",
		"PMA_PORT=9000\nDB_ENGINES=mysql":        "127.0.0.1:9000",
		"DB_ENGINES=postgresql,mariadb":          "127.0.0.1:8080",
		"DB_ENGINES=postgresql\nPGADMIN_PORT=82": "127.0.0.1:82",
		"DB_ENGINES=postgresql":                  "127.0.0.1:8081",
		"APP_ENV=prod":                           "",
	}
	for content, want := range cases {
		if got := newEnv(t, content).DBAdminTarget(); got != want {
			t.Errorf("%q: %q, want %q", content, got, want)
		}
	}
}

func TestResolveDirAndCertsDir(t *testing.T) {
	project := filepath.Join(t.TempDir(), "data")
	abs := filepath.Join(t.TempDir(), "caddy-elsewhere")

	e := newEnv(t, "DATABASE_DIR=db/files\nCERTS_DIR=/ignored\n")
	if got := e.ResolveDir("DATABASE_DIR", "./database", project); got != filepath.Join(project, "db", "files") {
		t.Errorf("relative: %q", got)
	}
	if got := e.ResolveDir("CADDY_DIR", "./caddy", project); got != filepath.Join(project, "caddy") {
		t.Errorf("default: %q", got)
	}
	// CERTS_DIR is ignored: certificates live in CADDY_DIR/certs
	if got := e.CertsDir(project); got != filepath.Join(project, "caddy", "certs") {
		t.Errorf("CertsDir: %q", got)
	}

	e = newEnv(t, "CADDY_DIR="+abs+"\n")
	if got := e.CertsDir(project); got != filepath.Join(abs, "certs") {
		t.Errorf("absolute CADDY_DIR: %q", got)
	}
}

func TestComposeEnvVars(t *testing.T) {
	e := newEnv(t, strings.Join([]string{
		"UID=501", "GID=20",
		"EXPOSE_SERVICES=true",
		"DB_PORT=3310",
		"MARIADB_ROOT_PASSWORD=maria",
		"POSTGRES_PASSWORD=pg", "POSTGRES_PORT=5433",
		"SOKETI_ENABLED=true", "SOKETI_PORT=6002",
		"SOKETI_DEFAULT_APP_ID=app", "SOKETI_DEFAULT_APP_KEY=key", "SOKETI_DEFAULT_APP_SECRET=secret",
	}, "\n"))
	project := filepath.Join(t.TempDir(), "data")

	got, err := e.ComposeEnvVars(project, "/projects", []string{"mariadb", "postgresql"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"CUSTOM_PATH":               "/projects",
		"UID":                       "501",
		"GID":                       "20",
		"DB_PORT":                   "3310:3306",
		"PMA_PORT":                  "8080:80",
		"REDIS_PORT":                "6379:6379",
		"WEB_HTTP_PORT":             "80",
		"WEB_HTTPS_PORT":            "443",
		"MYSQL_MAX_ALLOWED_PACKET":  "512M",
		"PWD":                       project,
		"CADDY_DIR":                 filepath.Join(project, "caddy"),
		"DATABASE_DIR":              filepath.Join(project, "database"),
		"MARIADB_ROOT_PASSWORD":     "maria",
		"MARIADB_VERSION":           "10.11.9",
		"POSTGRES_PASSWORD":         "pg",
		"POSTGRES_USER":             "postgres",
		"POSTGRES_VERSION":          "16",
		"POSTGRES_PORT":             "5433:5432",
		"PGADMIN_PORT":              "8081:80",
		"SOKETI_PORT":               "6002",
		"SOKETI_PUBLISH":            "6002:6001",
		"SOKETI_DEFAULT_APP_ID":     "app",
		"SOKETI_DEFAULT_APP_KEY":    "key",
		"SOKETI_DEFAULT_APP_SECRET": "secret",
	}
	if !reflect.DeepEqual(got, want) {
		for k := range mergeKeys(got, want) {
			if got[k] != want[k] {
				t.Errorf("%s = %q, want %q", k, got[k], want[k])
			}
		}
	}
}

func TestComposeEnvVarsLocalhostAndMissingValues(t *testing.T) {
	e := newEnv(t, "UID=1\nGID=1\nMYSQL_ROOT_PASSWORD=m\n")
	got, err := e.ComposeEnvVars("/data", "", []string{"mysql"})
	if err != nil {
		t.Fatal(err)
	}
	if got["DB_PORT"] != "127.0.0.1:3306:3306" || got["MYSQL_PORT"] != "127.0.0.1:3307:3306" ||
		got["MYSQL_VERSION"] != "8.0" {
		t.Errorf("ports and defaults: %v", got)
	}

	if _, err := e.ComposeEnvVars("/data", "", []string{"mariadb"}); err == nil ||
		err.Error() != "Required environment variable 'MARIADB_ROOT_PASSWORD' is not set" {
		t.Errorf("missing password: err = %v", err)
	}
	if _, err := newEnv(t, "GID=1").ComposeEnvVars("/data", "", nil); err == nil {
		t.Error("missing UID accepted")
	}
}

func mergeKeys(a, b map[string]string) map[string]bool {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	return keys
}

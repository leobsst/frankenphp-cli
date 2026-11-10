package mkcert

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/command"
	"github.com/leobsst/frankenphp-cli/internal/command/commandtest"
	"github.com/leobsst/frankenphp-cli/internal/platform"
)

func newInstaller(goos string, admin bool, paths map[string]string) (*Installer, *commandtest.Runner, *commandtest.Log) {
	r := &commandtest.Runner{Paths: paths}
	log := &commandtest.Log{}
	return &Installer{
		GOOS: goos, GOARCH: "amd64", Run: r, Log: log,
		IsAdmin:  func() bool { return admin },
		Download: func(context.Context, string, string) error { return errors.New("no network in tests") },
		Home:     "/home/me",
	}, r, log
}

func TestGenerateCA(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mkcert")
	if err := GenerateCA(dir); err != nil {
		t.Fatal(err)
	}
	certPEM, _ := os.ReadFile(filepath.Join(dir, "rootCA.pem"))
	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if got := cert.Subject.String(); got != "CN=FrankenManager Local Development CA,OU=Local Development CA,O=FrankenManager" {
		t.Errorf("subject = %s", got)
	}
	if cert.Issuer.String() != cert.Subject.String() || !cert.IsCA || cert.MaxPathLen != 0 || !cert.MaxPathLenZero {
		t.Errorf("not a pathlen=0 self-signed CA: %+v", cert)
	}
	if cert.KeyUsage != x509.KeyUsageCertSign || len(cert.SubjectKeyId) == 0 || len(cert.AuthorityKeyId) != 0 {
		t.Errorf("key usage %v, SKI %x, AKI %x", cert.KeyUsage, cert.SubjectKeyId, cert.AuthorityKeyId)
	}
	critical := map[string]bool{}
	for _, ext := range cert.Extensions {
		critical[ext.Id.String()] = ext.Critical
	}
	if !critical["2.5.29.19"] || !critical["2.5.29.15"] || critical["2.5.29.14"] {
		t.Errorf("criticality = %v", critical)
	}
	if cert.SignatureAlgorithm != x509.SHA256WithRSA || cert.PublicKey.(*rsa.PublicKey).N.BitLen() != 3072 {
		t.Errorf("algorithm %v", cert.SignatureAlgorithm)
	}
	if d := cert.NotAfter.Sub(cert.NotBefore); d != 3650*24*time.Hour {
		t.Errorf("validity %v", d)
	}
	if cert.SerialNumber.Sign() <= 0 || cert.SerialNumber.BitLen() > 159 {
		t.Errorf("serial %v", cert.SerialNumber)
	}

	keyPEM, _ := os.ReadFile(filepath.Join(dir, "rootCA-key.pem"))
	kb, _ := pem.Decode(keyPEM)
	if kb.Type != "PRIVATE KEY" {
		t.Errorf("key block %s", kb.Type)
	}
	if _, err := x509.ParsePKCS8PrivateKey(kb.Bytes); err != nil {
		t.Error(err)
	}
	for name, mode := range map[string]os.FileMode{"rootCA.pem": 0o644, "rootCA-key.pem": 0o600} {
		if info, _ := os.Stat(filepath.Join(dir, name)); runtime.GOOS != "windows" && info.Mode().Perm() != mode { // Windows keeps no Unix mode
			t.Errorf("%s mode %v", name, info.Mode())
		}
	}
	if !IsFrankenManagerCA(filepath.Join(dir, "rootCA.pem")) || IsFrankenManagerCA(filepath.Join(dir, "rootCA-key.pem")) {
		t.Error("IsFrankenManagerCA")
	}
}

// caRunner answers `mkcert -CAROOT` with dir.
func caRunner(r *commandtest.Runner, dir string, install command.Result) {
	r.Handle = func(c command.Cmd) (command.Result, error) {
		if slices.Contains(c.Args, "-CAROOT") {
			return command.Result{Stdout: []byte(dir + "\n")}, nil
		}
		if slices.Contains(c.Args, "-install") {
			return install, nil
		}
		return command.Result{}, nil
	}
}

func TestSetupCAReplacesForeignCA(t *testing.T) {
	in, r, log := newInstaller("linux", true, map[string]string{"mkcert": "/usr/bin/mkcert"})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rootCA.pem"), []byte("mkcert development CA"), 0o644); err != nil {
		t.Fatal(err)
	}
	caRunner(r, dir, command.Result{})

	if !in.SetupCA(context.Background()) {
		t.Fatalf("SetupCA failed: %v", log.Lines)
	}
	if !IsFrankenManagerCA(filepath.Join(dir, "rootCA.pem")) {
		t.Error("CA not replaced")
	}
	// Root: no sudo, the real user's HOME, not /root
	want := []string{"/usr/bin/mkcert -CAROOT", "/usr/bin/mkcert -uninstall", "/usr/bin/mkcert -install"}
	if !slices.Equal(r.Lines(), want) {
		t.Errorf("calls = %v", r.Lines())
	}
	for _, c := range r.Calls {
		if c.Env["HOME"] != "/home/me" {
			t.Errorf("%s without the user's HOME: %v", c, c.Env)
		}
	}

	// Already ours: kept as is
	before, _ := os.ReadFile(filepath.Join(dir, "rootCA.pem"))
	r.Calls = nil
	if !in.SetupCA(context.Background()) {
		t.Fatal("second SetupCA failed")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "rootCA.pem"))
	if string(before) != string(after) || len(r.Calls) != 2 {
		t.Errorf("our CA regenerated, calls %v", r.Lines())
	}
}

func TestSetupCAWithoutRootUsesSudo(t *testing.T) {
	in, r, log := newInstaller("darwin", false, map[string]string{"mkcert": "/opt/homebrew/bin/mkcert"})
	caRunner(r, t.TempDir(), command.Result{ExitCode: 1, Stderr: []byte("denied")})
	if in.SetupCA(context.Background()) {
		t.Fatal("SetupCA succeeded")
	}
	if got := r.Lines()[1]; got != "sudo /opt/homebrew/bin/mkcert -install" {
		t.Errorf("install = %s", got)
	}
	if last := log.Lines[len(log.Lines)-1]; last != "error: Failed to install CA: denied" {
		t.Errorf("log = %v", log.Lines)
	}
}

func TestSetupCAWithoutMkcert(t *testing.T) {
	in, r, log := newInstaller("linux", true, nil)
	if in.SetupCA(context.Background()) || len(r.Calls) != 0 || log.Lines[0] != "error: mkcert is not installed" {
		t.Errorf("calls %v log %v", r.Lines(), log.Lines)
	}
}

func TestInstallLinux(t *testing.T) {
	for _, tc := range []struct {
		arch, asset string
	}{{"amd64", "amd64"}, {"arm64", "arm64"}, {"arm", "arm"}} { // arm: armv7
		t.Run(tc.arch, func(t *testing.T) {
			in, r, log := newInstaller("linux", true, map[string]string{"apt-get": "/usr/bin/apt-get"})
			in.GOARCH = tc.arch
			in.BinDir = t.TempDir()
			var got string
			in.Download = func(_ context.Context, url, dest string) error {
				got = url
				return os.WriteFile(dest, []byte("bin"), 0o600)
			}
			if !in.Install(context.Background()) {
				t.Fatalf("install failed: %v", log.Lines)
			}
			if got != "https://github.com/FiloSottile/mkcert/releases/download/v1.4.4/mkcert-v1.4.4-linux-"+tc.asset {
				t.Errorf("url = %s", got)
			}
			if info, err := os.Stat(filepath.Join(in.BinDir, "mkcert")); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o755) {
				t.Errorf("mkcert not installed: %v", err)
			}
			want := []string{"sudo apt-get update", "sudo apt-get install -y libnss3-tools"}
			if !slices.Equal(r.Lines(), want) {
				t.Errorf("calls = %v", r.Lines())
			}
		})
	}
}

func TestInstallLinuxAsUserAndFailures(t *testing.T) {
	in, _, log := newInstaller("linux", false, nil)
	in.Home = t.TempDir()
	in.Download = func(_ context.Context, _, dest string) error { return os.WriteFile(dest, nil, 0o600) }
	if !in.Install(context.Background()) {
		t.Fatal("install failed")
	}
	if _, err := os.Stat(filepath.Join(in.Home, ".local", "bin", "mkcert")); err != nil {
		t.Error(err)
	}
	if !slices.Contains(log.Lines, "info: Make sure ~/.local/bin is in your PATH") ||
		log.Lines[0] != "warning: No supported package manager found, skipping NSS install for Firefox support." {
		t.Errorf("log = %v", log.Lines)
	}

	in, _, log = newInstaller("linux", true, nil)
	in.BinDir = t.TempDir()
	if in.Install(context.Background()) || !strings.HasPrefix(log.Lines[len(log.Lines)-1], "error: Download failed") {
		t.Errorf("log = %v", log.Lines)
	}

	in, _, log = newInstaller("linux", true, nil)
	in.GOARCH = "riscv64"
	if in.Install(context.Background()) || !slices.Contains(log.Lines, "warning: Unsupported architecture: riscv64") {
		t.Errorf("log = %v", log.Lines)
	}
}

func TestInstallMacOSAndWindows(t *testing.T) {
	in, r, _ := newInstaller("darwin", false, map[string]string{"brew": "/opt/homebrew/bin/brew"})
	if !in.Install(context.Background()) {
		t.Fatal("brew install failed")
	}
	if want := []string{"brew install nss", "brew install mkcert"}; !slices.Equal(r.Lines(), want) {
		t.Errorf("calls = %v", r.Lines())
	}

	in, r, _ = newInstaller("windows", true, map[string]string{"choco": "c", "scoop": "s"})
	r.Handle = func(c command.Cmd) (command.Result, error) {
		if c.Name == "choco" {
			return command.Result{ExitCode: 1}, nil
		}
		return command.Result{}, nil
	}
	if !in.Install(context.Background()) {
		t.Fatal("scoop install failed")
	}
	want := []string{"choco install mkcert -y", "scoop bucket add extras", "scoop install mkcert"}
	if !slices.Equal(r.Lines(), want) {
		t.Errorf("calls = %v", r.Lines())
	}

	in, r, _ = newInstaller("darwin", false, map[string]string{"mkcert": "m"})
	if !in.Install(context.Background()) || len(r.Calls) != 0 {
		t.Error("installed mkcert reinstalled")
	}
}

// Under `sudo frankenmanager setup`, CAROOT and the CA files go back to the
// real user, even when `mkcert -install` fails: otherwise `start` without
// sudo cannot read the key and sign the site certificates.
func TestSetupCAHandsTheCABackToTheUser(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no sudo")
	}
	var owned []string
	old := platform.Chown
	platform.Chown = func(path string, uid, gid int) error {
		if uid != 501 || gid != 20 {
			t.Errorf("chown %s to %d:%d", path, uid, gid)
		}
		owned = append(owned, path)
		return nil
	}
	t.Cleanup(func() { platform.Chown = old })
	t.Setenv("SUDO_UID", "501")
	t.Setenv("SUDO_GID", "20")

	for _, install := range []command.Result{{}, {ExitCode: 1, Stderr: []byte("trust store failure")}} {
		owned = nil
		in, r, log := newInstaller("linux", true, map[string]string{"mkcert": "/usr/bin/mkcert"})
		dir := t.TempDir()
		caRunner(r, dir, install)
		if got := in.SetupCA(context.Background()); got != install.OK() {
			t.Errorf("SetupCA = %v, log %v", got, log.Lines)
		}
		want := []string{dir, filepath.Join(dir, "rootCA.pem"), filepath.Join(dir, "rootCA-key.pem")}
		if !slices.Equal(owned, want) {
			t.Errorf("install ok=%v: chowned %v, want %v", install.OK(), owned, want)
		}
	}
}

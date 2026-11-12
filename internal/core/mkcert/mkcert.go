// Package mkcert installs mkcert and the FrankenManager root CA.
//
// Every machine trusts a CA with the same recognizable identity
// (O=FrankenManager) instead of mkcert's default one: it is written into
// mkcert's CAROOT before `mkcert -install`, which then registers it.
package mkcert

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/command"
	"github.com/leobsst/frankenphp-cli/internal/platform"
)

// Identity of every root CA FrankenManager generates.
const (
	CAOrganization       = "FrankenManager"
	CAOrganizationalUnit = "Local Development CA"
	CACommonName         = "FrankenManager Local Development CA"

	// Version is the mkcert release downloaded on Linux.
	Version = "v1.4.4"

	caCertName = "rootCA.pem"
	caKeyName  = "rootCA-key.pem"
	caValidity = 3650 * 24 * time.Hour
	caKeyBits  = 3072
)

// Logger receives the progress messages.
type Logger interface {
	Info(msg string)
	Success(msg string)
	Warning(msg string)
	Error(msg string)
}

// Installer installs mkcert and the CA.
type Installer struct {
	GOOS, GOARCH string
	Run          command.Runner
	Log          Logger
	IsAdmin      func() bool
	// Download saves url to dest (Linux install), replaced in tests.
	Download func(ctx context.Context, url, dest string) error
	// BinDir is where mkcert is installed on Linux as root.
	BinDir string
	// Home is the home directory of the real user (under sudo too).
	Home string
	// SudoUser is the user who ran sudo, "" outside sudo: Homebrew refuses
	// to run as root, so brew runs as that user.
	SudoUser string
}

// New returns the Installer of the running system.
func New(log Logger) *Installer {
	return &Installer{
		GOOS:     runtime.GOOS,
		GOARCH:   runtime.GOARCH,
		Run:      command.Exec{},
		Log:      log,
		IsAdmin:  platform.IsAdmin,
		Download: download,
		BinDir:   "/usr/local/bin",
		Home:     realHome(),
		SudoUser: sudoUser(),
	}
}

// sudoUser is the name of the user who ran sudo, "" outside sudo.
func sudoUser() string {
	if platform.UnderSudo() {
		return os.Getenv("SUDO_USER")
	}
	return ""
}

// realHome is the home of the user who ran sudo, the current one otherwise.
func realHome() string {
	if name := os.Getenv("SUDO_USER"); name != "" && platform.UnderSudo() {
		if u, err := user.Lookup(name); err == nil {
			return u.HomeDir
		}
	}
	home, _ := os.UserHomeDir()
	return home
}

// mkcertEnv runs mkcert with the real user's HOME: sudo on Linux often
// resets HOME to /root, which would put CAROOT in root's home.
func (in *Installer) mkcertEnv() map[string]string {
	if in.GOOS == "windows" || in.Home == "" {
		return nil
	}
	return map[string]string{"HOME": in.Home}
}

// IsInstalled reports whether mkcert is in the PATH.
func (in *Installer) IsInstalled() bool { return in.Run.LookPath("mkcert") != "" }

// Instructions explains how to install mkcert by hand.
func (in *Installer) Instructions() string {
	switch in.GOOS {
	case "darwin":
		return `To install mkcert on macOS:

    brew install mkcert
    brew install nss  # if using Firefox

Or download from: https://github.com/FiloSottile/mkcert/releases`
	case "windows":
		return `To install mkcert on Windows:

    # Using Chocolatey
    choco install mkcert

    # Using Scoop
    scoop install mkcert

    # Or download from: https://github.com/FiloSottile/mkcert/releases`
	}
	return `To install mkcert on Linux:

    # Ubuntu/Debian
    sudo apt install libnss3-tools
    # Download from GitHub releases and install to /usr/local/bin
    # See: https://github.com/FiloSottile/mkcert/releases

    # Arch Linux
    sudo pacman -S mkcert

    # Fedora
    sudo dnf install mkcert`
}

// run runs a command, reporting a failure to start as a failed result.
func (in *Installer) run(ctx context.Context, name string, args ...string) command.Result {
	return in.runCmd(ctx, command.Cmd{Name: name, Args: args})
}

func (in *Installer) runCmd(ctx context.Context, c command.Cmd) command.Result {
	res, err := in.Run.Run(ctx, c)
	if err != nil {
		return command.Result{ExitCode: -1, Stderr: []byte(err.Error())}
	}
	return res
}

// brew runs Homebrew with env added. As root under sudo, it runs as the
// user who ran sudo (with their HOME): brew refuses to run as root, and
// sudo drops the environment, hence env.
func (in *Installer) brew(ctx context.Context, env map[string]string, args ...string) command.Result {
	if in.SudoUser == "" || !in.IsAdmin() {
		return in.runCmd(ctx, command.Cmd{Name: "brew", Args: args, Env: env})
	}
	sudoArgs := []string{"-H", "-u", in.SudoUser, "env"}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		sudoArgs = append(sudoArgs, k+"="+env[k])
	}
	// sudo's secure_path may not include Homebrew's bin directory
	sudoArgs = append(append(sudoArgs, in.Run.LookPath("brew")), args...)
	return in.runCmd(ctx, command.Cmd{Name: "sudo", Args: sudoArgs})
}

// Install installs mkcert, and NSS first so that `mkcert -install` also
// trusts the CA in Firefox and Java. It reports whether mkcert is there.
func (in *Installer) Install(ctx context.Context) bool {
	if in.IsInstalled() {
		in.Log.Info("mkcert is already installed")
		return true
	}
	in.installNSS(ctx)
	in.Log.Info("Attempting to install mkcert...")

	var err error
	ok := false
	switch in.GOOS {
	case "darwin":
		ok = in.installMacOS(ctx)
	case "windows":
		ok = in.installWindows(ctx)
	default:
		ok, err = in.installLinux(ctx)
	}
	if err != nil {
		in.Log.Error(fmt.Sprintf("Failed to install mkcert: %v", err))
		in.Log.Info("")
		in.Log.Info(in.Instructions())
		return false
	}
	return ok
}

// linuxNSS maps each package manager to its NSS package and install
// arguments, in lookup order.
var linuxNSS = []struct {
	manager, pkg string
	args         []string
}{
	{"apt-get", "libnss3-tools", []string{"install", "-y"}},
	{"dnf", "nss-tools", []string{"install", "-y"}},
	{"yum", "nss-tools", []string{"install", "-y"}},
	{"pacman", "nss", []string{"-S", "--noconfirm"}},
	{"zypper", "mozilla-nss-tools", []string{"install", "-y"}},
}

// installNSS installs certutil (NSS). Not fatal: the system trust store
// works without it.
func (in *Installer) installNSS(ctx context.Context) bool {
	switch in.GOOS {
	case "windows":
		return true // Firefox on Windows needs no NSS
	case "darwin":
		if in.Run.LookPath("brew") == "" {
			in.Log.Warning("Homebrew not found, skipping NSS install for Firefox support.")
			return false
		}
		in.Log.Info("Installing nss via Homebrew...")
		if res := in.brew(ctx, nil, "install", "nss"); !res.OK() {
			in.Log.Warning("Failed to install nss: " + string(res.Stderr))
			return false
		}
		in.Log.Success("nss installed successfully!")
		return true
	}
	for _, n := range linuxNSS {
		if in.Run.LookPath(n.manager) == "" {
			continue
		}
		in.Log.Info(fmt.Sprintf("Installing %s via %s...", n.pkg, n.manager))
		if n.manager == "apt-get" {
			in.run(ctx, "sudo", "apt-get", "update")
		}
		args := append(append([]string{n.manager}, n.args...), n.pkg)
		if res := in.run(ctx, "sudo", args...); !res.OK() {
			in.Log.Warning(fmt.Sprintf("Failed to install %s: %s", n.pkg, res.Stderr))
			return false
		}
		in.Log.Success(n.pkg + " installed successfully!")
		return true
	}
	in.Log.Warning("No supported package manager found, skipping NSS install for Firefox support.")
	return false
}

func (in *Installer) installMacOS(ctx context.Context) bool {
	if in.Run.LookPath("brew") == "" {
		in.Log.Warning("Homebrew not found. Please install mkcert manually:")
		in.Log.Info(in.Instructions())
		return false
	}
	in.Log.Info("Installing mkcert via Homebrew...")
	if res := in.brew(ctx, nil, "install", "mkcert"); !res.OK() {
		in.Log.Error("Homebrew installation failed: " + string(res.Stderr))
		return false
	}
	in.Log.Success("mkcert installed successfully!")
	return true
}

// linuxArch maps GOARCH to the mkcert release asset: amd64, arm64 and
// armv7.
var linuxArch = map[string]string{"amd64": "amd64", "arm64": "arm64", "arm": "arm"}

// DownloadURL returns the mkcert release asset of a Linux architecture.
func DownloadURL(arch string) string {
	return "https://github.com/FiloSottile/mkcert/releases/download/" + Version + "/mkcert-" + Version + "-linux-" + arch
}

func (in *Installer) installLinux(ctx context.Context) (bool, error) {
	arch, ok := linuxArch[in.GOARCH]
	if !ok {
		in.Log.Warning("Unsupported architecture: " + in.GOARCH)
		in.Log.Info(in.Instructions())
		return false, nil
	}
	url := DownloadURL(arch)
	in.Log.Info(fmt.Sprintf("Downloading mkcert from %s...", url))

	destDir := in.BinDir
	if !in.IsAdmin() {
		destDir = filepath.Join(in.Home, ".local", "bin")
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return false, err
		}
	}
	dest := filepath.Join(destDir, "mkcert")
	tmp := dest + ".download"
	if err := in.Download(ctx, url, tmp); err != nil {
		_ = os.Remove(tmp)
		in.Log.Error(fmt.Sprintf("Download failed: %v", err))
		return false, nil
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return false, err
	}
	if !in.IsAdmin() {
		in.Log.Warning("Installed to " + dest)
		in.Log.Info("Make sure ~/.local/bin is in your PATH")
	}
	in.Log.Success("mkcert installed successfully!")
	return true, nil
}

func (in *Installer) installWindows(ctx context.Context) bool {
	if in.Run.LookPath("choco") != "" {
		in.Log.Info("Installing mkcert via Chocolatey...")
		if in.run(ctx, "choco", "install", "mkcert", "-y").OK() {
			in.Log.Success("mkcert installed successfully!")
			return true
		}
	}
	if in.Run.LookPath("scoop") != "" {
		in.Log.Info("Installing mkcert via Scoop...")
		in.run(ctx, "scoop", "bucket", "add", "extras") // mkcert is in the extras bucket
		if in.run(ctx, "scoop", "install", "mkcert").OK() {
			in.Log.Success("mkcert installed successfully!")
			return true
		}
	}
	in.Log.Warning("Neither Chocolatey nor Scoop found.")
	in.Log.Info(in.Instructions())
	return false
}

// download saves url to dest.
func download(ctx context.Context, url, dest string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// CARoot returns mkcert's CA directory (`mkcert -CAROOT`), "" when mkcert
// cannot tell.
func (in *Installer) CARoot(ctx context.Context, mkcert string) string {
	res, err := in.Run.Run(ctx, command.Cmd{Name: mkcert, Args: []string{"-CAROOT"}, Env: in.mkcertEnv()})
	if err != nil || !res.OK() {
		return ""
	}
	return strings.TrimSpace(string(res.Stdout))
}

// IsFrankenManagerCA reports whether the certificate at certPath has the
// FrankenManager organization.
func IsFrankenManagerCA(certPath string) bool {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return false
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	org := cert.Subject.Organization
	return len(org) > 0 && org[0] == CAOrganization
}

// GenerateCA writes a FrankenManager root CA into caRoot: RSA 3072, SHA-256,
// valid 3650 days, BasicConstraints ca pathlen=0 and KeyUsage keyCertSign
// (both critical), and a Subject Key Identifier. rootCA-key.pem is PKCS8,
// 0600; rootCA.pem is 0644.
func GenerateCA(caRoot string) error {
	if err := platform.MakeUserDir(caRoot); err != nil {
		return err
	}
	key, err := rsa.GenerateKey(rand.Reader, caKeyBits)
	if err != nil {
		return err
	}
	// Positive, up to 159 bits, like cryptography's random_serial_number()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 159))
	if err != nil {
		return err
	}
	name := pkix.Name{
		Organization:       []string{CAOrganization},
		OrganizationalUnit: []string{CAOrganizationalUnit},
		CommonName:         CACommonName,
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               name,
		Issuer:                name,
		NotBefore:             now,
		NotAfter:              now.Add(caValidity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign,
		// SubjectKeyId left empty: Go derives it from the public key for a
		// CA, like SubjectKeyIdentifier.from_public_key
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err := writePEM(filepath.Join(caRoot, caKeyName), "PRIVATE KEY", pkcs8, 0o600); err != nil {
		return err
	}
	return writePEM(filepath.Join(caRoot, caCertName), "CERTIFICATE", der, 0o644)
}

func writePEM(path, blockType string, der []byte, mode os.FileMode) error {
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), mode); err != nil {
		return err
	}
	return os.Chmod(path, mode) // WriteFile keeps the mode of an existing file
}

// SetupCA makes mkcert's CAROOT hold the FrankenManager CA, replacing any
// other one, then registers it in the trust stores (`mkcert -install`).
// CAROOT and the CA files are handed back to the real user afterwards:
// later runs without sudo sign site certificates with that key.
func (in *Installer) SetupCA(ctx context.Context) bool {
	mkcert := in.Run.LookPath("mkcert")
	if mkcert == "" {
		in.Log.Error("mkcert is not installed")
		return false
	}
	caRoot := in.CARoot(ctx, mkcert)
	if caRoot != "" {
		defer giveToUser(caRoot)
		cert := filepath.Join(caRoot, caCertName)
		if !IsFrankenManagerCA(cert) {
			if _, err := os.Stat(cert); err == nil {
				in.uninstallCA(ctx, mkcert)
			}
			if err := GenerateCA(caRoot); err != nil {
				in.Log.Error(fmt.Sprintf("Failed to generate the CA: %v", err))
				return false
			}
		}
	}
	if !in.runInstall(ctx, mkcert) {
		return false
	}
	in.updateHomebrewCABundle(ctx)
	return true
}

// updateHomebrewCABundle rebuilds Homebrew's CA bundle on macOS, which
// OpenSSL-based clients (Homebrew PHP and curl...) read instead of the
// keychain: its post-install copies the trusted System keychain roots, so
// the CA just installed joins it. An outdated keg is upgraded instead, its
// post-install script failing against the newer formula. Best effort.
func (in *Installer) updateHomebrewCABundle(ctx context.Context) {
	if in.GOOS != "darwin" || in.Run.LookPath("brew") == "" ||
		!in.brew(ctx, nil, "list", "--versions", "ca-certificates").OK() {
		return
	}
	// The upgrade must not upgrade PHP or OpenSSL along with it
	env := map[string]string{"HOMEBREW_NO_INSTALLED_DEPENDENTS_CHECK": "1"}
	action := "postinstall"
	in.Log.Info("Adding the CA to Homebrew's CA bundle (PHP, curl)...")
	if res := in.brew(ctx, nil, "outdated", "--quiet", "ca-certificates"); strings.TrimSpace(string(res.Stdout)) != "" {
		action = "upgrade"
		in.Log.Info("Upgrading Homebrew's ca-certificates first...")
	}
	if res := in.brew(ctx, env, action, "ca-certificates"); !res.OK() {
		in.Log.Warning(fmt.Sprintf("Could not update Homebrew's CA bundle: %s", strings.TrimSpace(string(res.Stderr))))
		in.Log.Info("Host PHP may reject local certificates (cURL error 60). Run it by hand:")
		in.Log.Info("  brew " + action + " ca-certificates")
		return
	}
	in.Log.Success("Homebrew's CA bundle now trusts the FrankenManager CA!")
}

// uninstallCA removes the trusted CA before it is replaced, so that the
// old one does not stay trusted next to ours. Best effort.
func (in *Installer) uninstallCA(ctx context.Context, mkcert string) {
	in.Log.Info("Removing the existing CA from the trust store before replacing it...")
	res := in.runMkcert(ctx, mkcert, in.GOOS != "windows", "-uninstall")
	if !res.OK() {
		in.Log.Info("Could not remove the previous CA (continuing anyway): " + strings.TrimSpace(string(res.Stderr)))
	}
}

func (in *Installer) runInstall(ctx context.Context, mkcert string) bool {
	in.Log.Info("Installing FrankenManager CA (may require admin password)...")
	// The system trust store needs root on macOS and Linux
	res := in.runMkcert(ctx, mkcert, in.GOOS != "windows", "-install")
	if !res.OK() {
		in.Log.Error("Failed to install CA: " + string(res.Stderr))
		return false
	}
	in.Log.Success("mkcert CA installed successfully!")
	return true
}

// runMkcert runs mkcert with the real user's HOME, through sudo when asked
// and not already root (`setup` runs as root: no sudo, HOME applies).
func (in *Installer) runMkcert(ctx context.Context, mkcert string, sudo bool, args ...string) command.Result {
	c := command.Cmd{Name: mkcert, Args: args, Env: in.mkcertEnv()}
	if sudo && !in.IsAdmin() {
		c = command.Cmd{Name: "sudo", Args: append([]string{mkcert}, args...)}
	}
	res, err := in.Run.Run(ctx, c)
	if err != nil {
		return command.Result{ExitCode: -1, Stderr: []byte(err.Error())}
	}
	return res
}

// giveToUser hands CAROOT and the CA files back to the user who ran sudo.
func giveToUser(caRoot string) {
	for _, p := range []string{caRoot, filepath.Join(caRoot, caCertName), filepath.Join(caRoot, caKeyName)} {
		if _, err := os.Stat(p); err == nil {
			platform.ChownToSudoUser(p)
		}
	}
}

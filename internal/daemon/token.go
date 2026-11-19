package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/core/background"
)

// TokenFile is the file of the access token, in the data directory.
const TokenFile = "daemon.token"

// SocketFile is the Unix socket of the CLI, in the data directory.
const SocketFile = "daemon.sock"

// ServiceSpec is the background service running the daemon: started at
// login and relaunched when it dies (see background).
var ServiceSpec = background.Spec{
	Label: "dev.frankenmanager.daemon", Unit: "frankenmanager-daemon.service", Description: "FrankenManager daemon",
	Args: []string{"--no-update-check", "daemon", "run"}, PIDFile: ".daemon.pid", LogFile: "daemon.log",
}

// LoadToken returns the access token of the HTTP API stored in dir,
// creating it (readable by the user only) on first use.
func LoadToken(dir string) (string, error) {
	path := filepath.Join(dir, TokenFile)
	data, err := os.ReadFile(path)
	if err == nil {
		if token := strings.TrimSpace(string(data)); token != "" {
			return token, nil
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}
	return token, nil
}

package platform

import (
	"net"
	"os"
	"path/filepath"
)

// Executable returns the real path of the running binary, symlinks
// resolved: /usr/local/lib/frankenmanager/frankenmanager for a standard
// install reached through /usr/local/bin/frankenmanager. The watcher
// service and the background processes are started with it.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// LANIP returns the address this host uses to reach the network, through
// a UDP "connection" to 8.8.8.8:80 that sends no packet. 127.0.0.1 without
// network.
func LANIP() string {
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer func() { _ = conn.Close() }()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return "127.0.0.1"
}

package daemon

import "testing"

// The database admin link must be one the requester can open: https://localhost
// only on this machine, the published port at the address of this machine on
// another device, nothing when the admin UI is off or its port is not
// published beyond 127.0.0.1.
func TestDBAdminURL(t *testing.T) {
	cases := map[string]struct {
		port    string
		exposed bool
		remote  bool
		host    string
		want    string
	}{
		"this machine":                      {"8080", false, false, "127.0.0.1", "https://localhost"},
		"this machine, ports exposed":       {"8080", true, false, "frankenmanager.test", "https://localhost"},
		"no admin UI":                       {"", true, false, "127.0.0.1", ""},
		"no admin UI, remote":               {"", true, true, "192.168.1.20", ""},
		"remote, port on 127.0.0.1 only":    {"8080", false, true, "192.168.1.20", ""},
		"remote, LAN address":               {"8080", true, true, "192.168.1.20", "http://192.168.1.20:8080"},
		"remote, pgAdmin":                   {"8081", true, true, "192.168.1.20", "http://192.168.1.20:8081"},
		"remote, name of the remote access": {"8080", true, true, "dev.example.com", "http://dev.example.com:8080"},
		"remote, IPv6":                      {"8080", true, true, "fd00::20", "http://[fd00::20]:8080"},
		"remote, address unknown":           {"8080", true, true, "", ""},
	}
	for name, c := range cases {
		if got := dbAdminURL(c.port, c.exposed, c.remote, c.host); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

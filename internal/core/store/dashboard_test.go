package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// The proxies of a database of an earlier release keep their WebSocket
// connections; the option lives apart, its table left as it was, and goes
// with the proxy.
func TestProxyWebsocketMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	db, err := sql.Open("sqlite", path)
	check(t, err)
	_, err = db.Exec(`CREATE TABLE proxies (id INTEGER PRIMARY KEY AUTOINCREMENT, domain TEXT NOT NULL UNIQUE,
		target TEXT NOT NULL, added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`)
	check(t, err)
	_, err = db.Exec(`INSERT INTO proxies (domain, target) VALUES ('old.test', '127.0.0.1:1')`)
	check(t, err)
	check(t, db.Close())

	for range 2 {
		s, err := Open(path)
		check(t, err)
		check(t, s.Close())
	}
	s, err := Open(path)
	check(t, err)
	defer func() { _ = s.Close() }()
	check(t, s.AddProxies([]string{"new.test"}, "127.0.0.1:2"))
	ok, err := s.SetProxyWebsocket("new.test", false)
	check(t, err)
	if missing, _ := s.SetProxyWebsocket("ghost.test", false); !ok || missing {
		t.Errorf("SetProxyWebsocket = %v, ghost %v", ok, missing)
	}
	proxies, err := s.Proxies()
	check(t, err)
	if len(proxies) != 2 || proxies[0].NoWebsocket || !proxies[1].NoWebsocket {
		t.Errorf("proxies = %+v", proxies)
	}
	check(t, s.RemoveProxies([]string{"new.test"}))
	check(t, s.AddProxies([]string{"new.test"}, "127.0.0.1:3"))
	if proxies, _ := s.Proxies(); proxies[1].NoWebsocket {
		t.Error("the option outlived its proxy")
	}
}

func TestSessions(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db.sqlite"))
	check(t, err)
	defer func() { _ = s.Close() }()
	now := time.Now().UTC().Truncate(time.Second)
	check(t, s.AddSession(Session{ID: "a", Device: "phone", IP: "192.168.1.5", Remote: true, Created: now, LastSeen: now}, "hash-a"))
	check(t, s.AddSession(Session{ID: "b", Device: "old", Created: now.Add(-48 * time.Hour), LastSeen: now.Add(-48 * time.Hour)}, "hash-b"))
	got, ok, err := s.SessionByToken("hash-a")
	check(t, err)
	if !ok || got.ID != "a" || !got.Remote || !got.LastSeen.Equal(now) {
		t.Errorf("session = %+v, %v", got, ok)
	}
	if _, ok, _ := s.SessionByToken("a"); ok {
		t.Error("found by id instead of token hash")
	}
	check(t, s.RemoveSessions(now.Add(-time.Hour))) // expired
	if list, _ := s.Sessions(); len(list) != 1 || list[0].ID != "a" {
		t.Errorf("sessions = %+v", list)
	}
	check(t, s.RemoveSessions(time.Time{})) // all
	if list, _ := s.Sessions(); len(list) != 0 {
		t.Errorf("sessions = %+v", list)
	}

	for i := range accessLogSize + 5 {
		check(t, s.LogAccess(AccessEntry{At: now, IP: "10.0.0.1", Event: "login", Device: string(rune('a' + i%26))}))
	}
	log, err := s.AccessLog(accessLogSize + 10)
	check(t, err)
	if len(log) != accessLogSize {
		t.Errorf("%d entries kept", len(log))
	}
}

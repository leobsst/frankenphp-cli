package redis

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeRedis answers RESP commands on a local port with reply.
func fakeRedis(t *testing.T, reply func(args []string) string) (string, func() [][]string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var mu sync.Mutex
	var got [][]string
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			n, _ := strconv.Atoi(strings.TrimSpace(line[1:]))
			args := make([]string, n)
			for i := range args {
				_, _ = r.ReadString('\n') // $len
				v, _ := r.ReadString('\n')
				args[i] = strings.TrimSuffix(v, "\r\n")
			}
			mu.Lock()
			got = append(got, args)
			mu.Unlock()
			_, _ = fmt.Fprint(conn, reply(args))
		}
	}()
	return ln.Addr().String(), func() [][]string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(got)
	}
}

func bulk(s string) string { return fmt.Sprintf("$%d\r\n%s\r\n", len(s), s) }

func TestClient(t *testing.T) {
	addr, got := fakeRedis(t, func(args []string) string {
		switch args[0] {
		case "INFO":
			return bulk("# Server\r\nredis_version:7.4.1\r\nuptime_in_seconds:120\r\n# Memory\r\nused_memory_human:1.05M\r\n" +
				"# Stats\r\nkeyspace_hits:10\r\nkeyspace_misses:3\r\nconnected_clients:2\r\n# Keyspace\r\ndb0:keys=4,expires=1,avg_ttl=0\r\ndb1:keys=2,expires=0\r\n")
		case "SCAN":
			return "*2\r\n" + bulk("17") + "*2\r\n" + bulk("session:1") + bulk("cache:a")
		case "TYPE":
			return "+string\r\n"
		case "TTL":
			if args[1] == "session:1" {
				return ":300\r\n"
			}
			return ":-1\r\n"
		case "DEL":
			return ":1\r\n"
		case "FLUSHALL":
			return "-ERR flush refused\r\n"
		}
		return "-ERR unknown\r\n"
	})
	c, err := Dial(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	info, err := c.Info()
	if err != nil {
		t.Fatal(err)
	}
	if info != (Info{Version: "7.4.1", UptimeSeconds: 120, UsedMemory: "1.05M", ConnectedClients: 2, Keys: 6, Hits: 10, Misses: 3}) {
		t.Errorf("info = %+v", info)
	}
	page, err := c.Scan("0", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	want := Page{Cursor: "17", Keys: []Key{{"session:1", "string", 300}, {"cache:a", "string", -1}}}
	if page.Cursor != want.Cursor || !slices.Equal(page.Keys, want.Keys) {
		t.Errorf("page = %+v", page)
	}
	if n, err := c.Delete([]string{"cache:a", "gone"}); n != 1 || err != nil {
		t.Errorf("Delete = %d, %v", n, err)
	}
	var rerr Error
	if err := c.Flush(); !errors.As(err, &rerr) || !strings.Contains(err.Error(), "flush refused") {
		t.Errorf("Flush = %v", err)
	}
	if cmds := got(); strings.Join(cmds[1], " ") != "SCAN 0 MATCH * COUNT 50" {
		t.Errorf("commands = %v", cmds)
	}
}

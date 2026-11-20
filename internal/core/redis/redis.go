// Package redis talks to the Redis cache of the stack with the RESP
// protocol, straight on its port: no redis-cli process per call. Only what
// the dashboard needs: server stats, browsing and deleting keys, flushing.
package redis

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// Client is a connection to Redis.
type Client struct {
	conn net.Conn
	r    *bufio.Reader
}

// Dial connects to the Redis server at addr (host:port).
func Dial(ctx context.Context, addr string) (*Client, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	}
	return &Client{conn: conn, r: bufio.NewReader(conn)}, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Error is an error answered by Redis.
type Error string

func (e Error) Error() string { return "redis: " + string(e) }

// Do sends a command and returns its reply: string, int64, nil or []any.
func (c *Client) Do(args ...string) (any, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a)
	}
	if _, err := c.conn.Write([]byte(b.String())); err != nil {
		return nil, err
	}
	return c.read()
}

// pipeline sends several commands at once and returns their replies.
func (c *Client) pipeline(cmds [][]string) ([]any, error) {
	var b strings.Builder
	for _, args := range cmds {
		fmt.Fprintf(&b, "*%d\r\n", len(args))
		for _, a := range args {
			fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a)
		}
	}
	if _, err := c.conn.Write([]byte(b.String())); err != nil {
		return nil, err
	}
	out := make([]any, len(cmds))
	for i := range cmds {
		v, err := c.read()
		var rerr Error
		if err != nil && !errors.As(err, &rerr) {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// maxBulk bounds a bulk reply: the dashboard never needs a huge value.
const maxBulk = 64 << 20

func (c *Client) read() (any, error) {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSuffix(line, "\r\n")
	if line == "" {
		return nil, errors.New("redis: empty reply")
	}
	switch line[0] {
	case '+':
		return line[1:], nil
	case '-':
		return nil, Error(line[1:])
	case ':':
		return strconv.ParseInt(line[1:], 10, 64)
	case '$':
		n, err := strconv.Atoi(line[1:])
		if err != nil || n > maxBulk {
			return nil, fmt.Errorf("redis: bad bulk length %q", line)
		}
		if n < 0 {
			return nil, nil
		}
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(c.r, buf); err != nil {
			return nil, err
		}
		return string(buf[:n]), nil
	case '*':
		n, err := strconv.Atoi(line[1:])
		if err != nil {
			return nil, fmt.Errorf("redis: bad array length %q", line)
		}
		if n < 0 {
			return nil, nil
		}
		out := make([]any, n)
		for i := range out {
			if out[i], err = c.read(); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("redis: unexpected reply %q", line)
}

// --- What the dashboard uses -------------------------------------------------

// Info is the state of the server.
type Info struct {
	Version          string `json:"version"`
	UptimeSeconds    int64  `json:"uptime_seconds"`
	UsedMemory       string `json:"used_memory"` // e.g. "1.05M"
	ConnectedClients int64  `json:"connected_clients"`
	Keys             int64  `json:"keys"`
	Hits             int64  `json:"hits"`
	Misses           int64  `json:"misses"`
}

// Info returns the state of the server (INFO, every database).
func (c *Client) Info() (Info, error) {
	v, err := c.Do("INFO")
	if err != nil {
		return Info{}, err
	}
	text, _ := v.(string)
	return parseInfo(text), nil
}

func parseInfo(text string) Info {
	var info Info
	num := func(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		switch {
		case k == "redis_version":
			info.Version = v
		case k == "uptime_in_seconds":
			info.UptimeSeconds = num(v)
		case k == "used_memory_human":
			info.UsedMemory = v
		case k == "connected_clients":
			info.ConnectedClients = num(v)
		case k == "keyspace_hits":
			info.Hits = num(v)
		case k == "keyspace_misses":
			info.Misses = num(v)
		case strings.HasPrefix(k, "db"):
			// db0:keys=12,expires=0,avg_ttl=0
			for _, f := range strings.Split(v, ",") {
				if n, ok := strings.CutPrefix(f, "keys="); ok {
					info.Keys += num(n)
				}
			}
		}
	}
	return info
}

// Key is a key of the cache.
type Key struct {
	Name string `json:"name"`
	Type string `json:"type"`
	// TTL is the time to live in seconds, -1 without expiration
	TTL int64 `json:"ttl"`
}

// Page is a page of keys; Cursor is "0" after the last one.
type Page struct {
	Keys   []Key  `json:"keys"`
	Cursor string `json:"cursor"`
}

// Scan returns the keys of database 0 matching pattern from cursor ("0"
// to start), about count of them, with their type and time to live.
func (c *Client) Scan(cursor, pattern string, count int) (Page, error) {
	if pattern == "" {
		pattern = "*"
	}
	v, err := c.Do("SCAN", cursor, "MATCH", pattern, "COUNT", strconv.Itoa(count))
	if err != nil {
		return Page{}, err
	}
	arr, ok := v.([]any)
	if !ok || len(arr) != 2 {
		return Page{}, errors.New("redis: bad SCAN reply")
	}
	page := Page{Keys: []Key{}}
	page.Cursor, _ = arr[0].(string)
	names, _ := arr[1].([]any)
	var cmds [][]string
	for _, n := range names {
		name, _ := n.(string)
		page.Keys = append(page.Keys, Key{Name: name})
		cmds = append(cmds, []string{"TYPE", name}, []string{"TTL", name})
	}
	if len(cmds) == 0 {
		return page, nil
	}
	replies, err := c.pipeline(cmds)
	if err != nil {
		return Page{}, err
	}
	for i := range page.Keys {
		page.Keys[i].Type, _ = replies[2*i].(string)
		page.Keys[i].TTL, _ = replies[2*i+1].(int64)
	}
	return page, nil
}

// Delete deletes keys and returns how many existed.
func (c *Client) Delete(keys []string) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	v, err := c.Do(append([]string{"DEL"}, keys...)...)
	n, _ := v.(int64)
	return n, err
}

// Flush deletes every key of every database.
func (c *Client) Flush() error {
	_, err := c.Do("FLUSHALL")
	return err
}

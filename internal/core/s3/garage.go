package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// Garage talks to Garage's admin API (v2).
type Garage struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewGarage returns the admin client of the configured server.
func NewGarage(c Getter) *Garage {
	return &Garage{
		BaseURL: AdminURL(c),
		Token:   strings.TrimSpace(c.Get(AdminTokenKey)),
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// APIError is an error answered by the admin API.
type APIError struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("Garage: %s (HTTP %d)", e.Message, e.Status)
	}
	return fmt.Sprintf("Garage: HTTP %d", e.Status)
}

// IsNotFound reports whether err is a 404 of the admin API.
func IsNotFound(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

// call sends a request to /v2/<endpoint> and decodes the answer into out
// (nil to ignore it). A nil body sends a GET.
func (g *Garage) call(ctx context.Context, endpoint string, query url.Values, body, out any) error {
	u := g.BaseURL + "/v2/" + endpoint
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	method, reader := http.MethodGet, io.Reader(nil)
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		method, reader = http.MethodPost, bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := g.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		apiErr := &APIError{Status: res.StatusCode}
		_ = json.Unmarshal(data, apiErr)
		return apiErr
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// Ready reports whether the admin API answers.
func (g *Garage) Ready(ctx context.Context) bool {
	var status struct{}
	return g.call(ctx, "GetClusterStatus", nil, nil, &status) == nil
}

// WaitReady waits until the admin API answers, at most timeout.
func (g *Garage) WaitReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		if g.Ready(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return i18n.Errorf("The S3 server did not answer on %s.", g.BaseURL)
		case <-time.After(time.Second):
		}
	}
}

// EnsureLayout gives the single node a role on first start: Garage stores
// nothing until the cluster layout assigns it a zone and a capacity.
func (g *Garage) EnsureLayout(ctx context.Context) error {
	var layout struct {
		Version int64 `json:"version"`
		Roles   []struct {
			ID string `json:"id"`
		} `json:"roles"`
	}
	if err := g.call(ctx, "GetClusterLayout", nil, nil, &layout); err != nil {
		return err
	}
	if len(layout.Roles) > 0 {
		return nil
	}
	var status struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	if err := g.call(ctx, "GetClusterStatus", nil, nil, &status); err != nil {
		return err
	}
	if len(status.Nodes) == 0 {
		return i18n.NewError("The S3 server reports no node.")
	}
	roles := []map[string]any{{
		"id": status.Nodes[0].ID, "zone": "local", "tags": []string{"frankenmanager"},
		// 1 TB: the capacity only weighs nodes against each other
		"capacity": int64(1) << 40,
	}}
	if err := g.call(ctx, "UpdateClusterLayout", nil, map[string]any{"roles": roles}, nil); err != nil {
		return err
	}
	return g.call(ctx, "ApplyClusterLayout", nil, map[string]any{"version": layout.Version + 1}, nil)
}

// Perms are the permissions of a key on a bucket.
type Perms struct {
	Owner bool `json:"owner"`
	Read  bool `json:"read"`
	Write bool `json:"write"`
}

// String is "read", "read, write" or "read, write, owner".
func (p Perms) String() string {
	var s []string
	if p.Read {
		s = append(s, "read")
	}
	if p.Write {
		s = append(s, "write")
	}
	if p.Owner {
		s = append(s, "owner")
	}
	return strings.Join(s, ", ")
}

// Key is an access key.
type Key struct {
	ID      string `json:"accessKeyId"`
	Name    string `json:"name"`
	Secret  string `json:"secretAccessKey"`
	Buckets []struct {
		ID            string   `json:"id"`
		GlobalAliases []string `json:"globalAliases"`
		Permissions   Perms    `json:"permissions"`
	} `json:"buckets"`
}

// KeySummary is a key of ListKeys.
type KeySummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Keys lists the access keys.
func (g *Garage) Keys(ctx context.Context) ([]KeySummary, error) {
	var keys []KeySummary
	return keys, g.call(ctx, "ListKeys", nil, nil, &keys)
}

// KeyInfo returns a key, with its secret.
func (g *Garage) KeyInfo(ctx context.Context, id string) (Key, error) {
	var k Key
	err := g.call(ctx, "GetKeyInfo", url.Values{"id": {id}, "showSecretKey": {"true"}}, nil, &k)
	return k, err
}

// CreateKey creates a key named name.
func (g *Garage) CreateKey(ctx context.Context, name string) (Key, error) {
	var k Key
	err := g.call(ctx, "CreateKey", nil, map[string]any{"name": name}, &k)
	return k, err
}

// ImportKey imports a key with the given ID and secret.
func (g *Garage) ImportKey(ctx context.Context, id, secret, name string) error {
	return g.call(ctx, "ImportKey", nil,
		map[string]any{"accessKeyId": id, "secretAccessKey": secret, "name": name}, nil)
}

// AllowCreateBucket lets a key create buckets.
func (g *Garage) AllowCreateBucket(ctx context.Context, id string) error {
	return g.call(ctx, "UpdateKey", url.Values{"id": {id}},
		map[string]any{"allow": map[string]bool{"createBucket": true}}, nil)
}

// DeleteKey deletes a key.
func (g *Garage) DeleteKey(ctx context.Context, id string) error {
	return g.call(ctx, "DeleteKey", url.Values{"id": {id}}, map[string]any{}, nil)
}

// BucketKey is a key allowed on a bucket.
type BucketKey struct {
	ID          string `json:"accessKeyId"`
	Name        string `json:"name"`
	Permissions Perms  `json:"permissions"`
}

// Bucket is a bucket and its usage.
type Bucket struct {
	ID            string      `json:"id"`
	GlobalAliases []string    `json:"globalAliases"`
	WebsiteAccess bool        `json:"websiteAccess"`
	Keys          []BucketKey `json:"keys"`
	Objects       int64       `json:"objects"`
	Bytes         int64       `json:"bytes"`
}

// Name is the first global alias of b, its ID when it has none.
func (b Bucket) Name() string {
	if len(b.GlobalAliases) > 0 {
		return b.GlobalAliases[0]
	}
	return b.ID
}

// Buckets lists the buckets with their usage, by name.
func (g *Garage) Buckets(ctx context.Context) ([]Bucket, error) {
	var items []struct {
		ID string `json:"id"`
	}
	if err := g.call(ctx, "ListBuckets", nil, nil, &items); err != nil {
		return nil, err
	}
	buckets := make([]Bucket, 0, len(items))
	for _, it := range items {
		var b Bucket
		if err := g.call(ctx, "GetBucketInfo", url.Values{"id": {it.ID}}, nil, &b); err != nil {
			return nil, err
		}
		buckets = append(buckets, b)
	}
	slices.SortFunc(buckets, func(a, b Bucket) int { return strings.Compare(a.Name(), b.Name()) })
	return buckets, nil
}

// BucketNames returns the names of the buckets with a global alias, the
// ones reachable by name.
func (g *Garage) BucketNames(ctx context.Context) ([]string, error) {
	buckets, err := g.Buckets(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, b := range buckets {
		names = append(names, b.GlobalAliases...)
	}
	slices.Sort(names)
	return names, nil
}

// BucketInfo returns the bucket named name.
func (g *Garage) BucketInfo(ctx context.Context, name string) (Bucket, error) {
	var b Bucket
	err := g.call(ctx, "GetBucketInfo", url.Values{"globalAlias": {name}}, nil, &b)
	if IsNotFound(err) {
		return b, i18n.Errorf("Bucket '%s' not found.", name)
	}
	return b, err
}

// CreateBucket creates a bucket named name.
func (g *Garage) CreateBucket(ctx context.Context, name string) (Bucket, error) {
	var b Bucket
	err := g.call(ctx, "CreateBucket", nil, map[string]any{"globalAlias": name}, &b)
	return b, err
}

// DeleteBucket deletes an empty bucket.
func (g *Garage) DeleteBucket(ctx context.Context, id string) error {
	return g.call(ctx, "DeleteBucket", url.Values{"id": {id}}, map[string]any{}, nil)
}

// SetPublic turns the website access of a bucket on or off: on, every
// object is readable without credentials.
func (g *Garage) SetPublic(ctx context.Context, id string, public bool) error {
	access := map[string]any{"enabled": false}
	if public {
		access = map[string]any{"enabled": true, "indexDocument": "index.html"}
	}
	return g.call(ctx, "UpdateBucket", url.Values{"id": {id}}, map[string]any{"websiteAccess": access}, nil)
}

// Allow grants perms (the true ones) to a key on a bucket.
func (g *Garage) Allow(ctx context.Context, bucketID, keyID string, perms Perms) error {
	return g.call(ctx, "AllowBucketKey", nil,
		map[string]any{"bucketId": bucketID, "accessKeyId": keyID, "permissions": perms}, nil)
}

// EnsureGlobalKey imports the global key when Garage does not know it,
// lets it create buckets, and grants it every permission on every bucket.
func (g *Garage) EnsureGlobalKey(ctx context.Context, id, secret string) error {
	if _, err := g.KeyInfo(ctx, id); IsNotFound(err) {
		if err := g.ImportKey(ctx, id, secret, GlobalKeyName); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := g.AllowCreateBucket(ctx, id); err != nil {
		return err
	}
	buckets, err := g.Buckets(ctx)
	if err != nil {
		return err
	}
	all := Perms{Owner: true, Read: true, Write: true}
	for _, b := range buckets {
		if !slices.ContainsFunc(b.Keys, func(k BucketKey) bool { return k.ID == id && k.Permissions == all }) {
			if err := g.Allow(ctx, b.ID, id, all); err != nil {
				return err
			}
		}
	}
	return nil
}

// WriteConfig writes <dataDir>/garage/garage.toml and creates the metadata,
// data and ACL directories under dir, so that they belong to the user the
// containers run as.
func WriteConfig(dataDir, dir string, c Getter, production bool) error {
	for _, d := range []string{filepath.Join(dataDir, filepath.Dir(ConfigPath)),
		filepath.Join(dir, "meta"), filepath.Join(dir, "data"), filepath.Join(dir, aclDir)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dataDir, ConfigPath), []byte(GarageConfig(c, production)), 0o644)
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// cacheTTL is how long the bucket and key information is reused: a
// bucket made public or private, or a key allowed on a bucket, applies
// within that time.
const cacheTTL = 3 * time.Second

// errNotFound is a bucket or a key Garage does not know.
var errNotFound = errors.New("not found")

// Admin reads buckets and keys from Garage's admin API, with a short
// cache.
type Admin struct {
	BaseURL, Token string
	HTTP           *http.Client
	Now            func() time.Time

	mu      sync.Mutex
	buckets map[string]cached[BucketInfo]
	keys    map[string]cached[KeyInfo]
}

type cached[T any] struct {
	value T
	err   error
	at    time.Time
}

// BucketInfo is what the gateway needs of a bucket.
type BucketInfo struct {
	ID            string `json:"id"`
	WebsiteAccess bool   `json:"websiteAccess"`
}

// Perms are the permissions of a key on a bucket.
type Perms struct {
	Owner bool `json:"owner"`
	Read  bool `json:"read"`
	Write bool `json:"write"`
}

// KeyInfo is what the gateway needs of an access key.
type KeyInfo struct {
	ID      string `json:"accessKeyId"`
	Secret  string `json:"secretAccessKey"`
	Buckets []struct {
		ID          string `json:"id"`
		Permissions Perms  `json:"permissions"`
	} `json:"buckets"`
}

// On returns the permissions of the key on a bucket.
func (k KeyInfo) On(bucketID string) Perms {
	for _, b := range k.Buckets {
		if b.ID == bucketID {
			return b.Permissions
		}
	}
	return Perms{}
}

// Bucket returns a bucket by its global alias, errNotFound when none.
func (a *Admin) Bucket(ctx context.Context, name string) (BucketInfo, error) {
	return lookup(a, &a.buckets, name, func() (BucketInfo, error) {
		var b BucketInfo
		err := a.get(ctx, "GetBucketInfo", url.Values{"globalAlias": {name}}, &b)
		return b, err
	})
}

// Key returns an access key with its secret, errNotFound when unknown.
func (a *Admin) Key(ctx context.Context, id string) (KeyInfo, error) {
	return lookup(a, &a.keys, id, func() (KeyInfo, error) {
		var k KeyInfo
		err := a.get(ctx, "GetKeyInfo", url.Values{"id": {id}, "showSecretKey": {"true"}}, &k)
		return k, err
	})
}

// Forget drops a bucket from the cache (deleted).
func (a *Admin) Forget(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.buckets, name)
}

func lookup[T any](a *Admin, cache *map[string]cached[T], id string, load func() (T, error)) (T, error) {
	now := a.Now()
	a.mu.Lock()
	if c, ok := (*cache)[id]; ok && now.Sub(c.at) < cacheTTL {
		a.mu.Unlock()
		return c.value, c.err
	}
	a.mu.Unlock()

	v, err := load()
	if err != nil && !errors.Is(err, errNotFound) {
		// Not cached: Garage may be starting
		return v, err
	}
	a.mu.Lock()
	if *cache == nil {
		*cache = map[string]cached[T]{}
	}
	(*cache)[id] = cached[T]{value: v, err: err, at: now}
	a.mu.Unlock()
	return v, err
}

func (a *Admin) get(ctx context.Context, endpoint string, query url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.BaseURL+"/v2/"+endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	res, err := a.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	switch {
	case res.StatusCode == http.StatusNotFound:
		return errNotFound
	case res.StatusCode >= 300:
		return fmt.Errorf("garage admin API %s: HTTP %d: %s", endpoint, res.StatusCode, data)
	}
	return json.Unmarshal(data, out)
}

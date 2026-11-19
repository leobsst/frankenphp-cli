// Package buckets manages the buckets and access keys of the S3 server
// (Garage): what `s3 bucket` and `s3 key` do, reported as events and
// returned as data for the dashboard.
package buckets

import (
	"context"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/events"
	"github.com/leobsst/frankenphp-cli/internal/core/hosts"
	"github.com/leobsst/frankenphp-cli/internal/core/s3"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// Errors of Open.
var (
	ErrDisabled   = i18n.NewError("S3 is disabled: run 'frankenmanager service enable s3' to use it.")
	ErrNotRunning = i18n.NewError("The S3 server is not running: start it with 'frankenmanager start'.")
)

// Open returns the admin client of the S3 server, once checked that it is
// enabled and answers.
func Open(ctx context.Context, e *env.Env) (*s3.Garage, error) {
	if !s3.Enabled(e) {
		return nil, ErrDisabled
	}
	g := s3.NewGarage(e)
	if !g.Ready(ctx) {
		return nil, ErrNotRunning
	}
	return g, nil
}

// Service works on the buckets and keys of a running S3 server.
type Service struct {
	Dir    string // data directory
	Env    *env.Env
	Store  *store.Store
	Hosts  *hosts.Manager
	Garage *s3.Garage
	Log    events.Log
}

// syncHosts brings the hosts entries and DNS records of the bucket hosts in
// line with the buckets.
func (s *Service) syncHosts(ctx context.Context) error {
	return stack.SyncS3Hosts(ctx, s.Dir, s.Env, s.Store, s.Hosts, s.Garage)
}

func (s *Service) objectsDir() string { return s.Env.ResolveDir(s3.DirKey, s3.DefaultDir, s.Dir) }

// warnPublic explains what a public bucket exposes.
func (s *Service) warnPublic() {
	s.Log.Warning(i18n.T("Every object of a public bucket can be read and downloaded by anyone reaching " +
		"this server, without credentials, even the ones stored as private. To make only some objects " +
		"public, keep the bucket private and store them with the public-read ACL (visibility 'public')."))
}

// Bucket is a bucket and its usage.
type Bucket struct {
	Name    string
	Public  bool // every object readable without credentials
	Objects int64
	// PublicObjects counts the objects stored with the public-read ACL,
	// -1 for a public bucket (all of them)
	PublicObjects int
	Bytes         int64
	Keys          int // its own keys, without the global one
	URL           string
}

// List returns the buckets with their usage, by name.
func (s *Service) List(ctx context.Context) ([]Bucket, error) {
	buckets, err := s.Garage.Buckets(ctx)
	if err != nil {
		return nil, err
	}
	global := s.Env.Get(s3.AccessKeyKey)
	out := make([]Bucket, 0, len(buckets))
	for _, b := range buckets {
		keys := 0
		for _, k := range b.Keys {
			if k.ID != global {
				keys++
			}
		}
		public := -1
		if !b.WebsiteAccess {
			public = s3.PublicObjects(s.objectsDir(), b.ID)
		}
		out = append(out, Bucket{Name: b.Name(), Public: b.WebsiteAccess, Objects: b.Objects,
			PublicObjects: public, Bytes: b.Bytes, Keys: keys, URL: s3.PublicURL(b.Name())})
	}
	return out, nil
}

// Create creates buckets (names already validated with
// s3.ValidateBucket), allowed for the global key, public when asked. It
// returns the buckets created, without those that existed.
func (s *Service) Create(ctx context.Context, names []string, public bool) ([]string, error) {
	global := s.Env.Get(s3.AccessKeyKey)
	var created []string
	for _, name := range names {
		if _, err := s.Garage.BucketInfo(ctx, name); err == nil {
			s.Log.Info(i18n.Tf("Bucket %s already exists.", name))
			continue
		}
		b, err := s.Garage.CreateBucket(ctx, name)
		if err != nil {
			return created, err
		}
		if global != "" {
			if err := s.Garage.Allow(ctx, b.ID, global, s3.Perms{Owner: true, Read: true, Write: true}); err != nil {
				return created, err
			}
		}
		if public {
			if err := s.Garage.SetPublic(ctx, b.ID, true); err != nil {
				return created, err
			}
		}
		created = append(created, name)
		s.Log.Success(i18n.Tf("Bucket %s created: %s", name, s3.PublicURL(name)))
	}
	if public {
		s.warnPublic()
	}
	return created, s.syncHosts(ctx)
}

// Delete deletes buckets with their own keys. A bucket with objects is an
// error unless force, which empties it first.
func (s *Service) Delete(ctx context.Context, names []string, force bool) error {
	buckets := make([]s3.Bucket, 0, len(names))
	for _, name := range names {
		b, err := s.Garage.BucketInfo(ctx, name)
		if err != nil {
			return err
		}
		if b.Objects > 0 && !force {
			return i18n.Errorf("Bucket '%s' is not empty (%d objects): use --force to empty and delete it.",
				name, b.Objects)
		}
		buckets = append(buckets, b)
	}
	keys, err := s.Garage.Keys(ctx)
	if err != nil {
		return err
	}
	for _, b := range buckets {
		name := b.Name()
		if b.Objects > 0 {
			n, err := s3.NewObjects(s.Env).Empty(ctx, name)
			if err != nil {
				return err
			}
			s.Log.Info(i18n.Tf("Bucket %s emptied (%d objects deleted).", name, n))
		}
		if err := s.Garage.DeleteBucket(ctx, b.ID); err != nil {
			return err
		}
		if err := s3.DropBucketACL(s.objectsDir(), b.ID); err != nil {
			return err
		}
		// The keys made by `s3 key create` for this bucket
		for _, k := range keys {
			if strings.HasPrefix(k.Name, name+"/") {
				if err := s.Garage.DeleteKey(ctx, k.ID); err != nil {
					return err
				}
				s.Log.Info(i18n.Tf("Access key %s (%s) deleted.", k.ID, k.Name))
			}
		}
		s.Log.Success(i18n.Tf("Bucket %s deleted.", name))
	}
	return s.syncHosts(ctx)
}

// Check checks that the buckets exist.
func (s *Service) Check(ctx context.Context, names []string) error {
	for _, name := range names {
		if _, err := s.Garage.BucketInfo(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

// Empty deletes every object of the buckets. The caller confirms first.
func (s *Service) Empty(ctx context.Context, names []string) error {
	for _, name := range names {
		n, err := s3.NewObjects(s.Env).Empty(ctx, name)
		if err != nil {
			return err
		}
		s.Log.Success(i18n.Tf("Bucket %s emptied (%d objects deleted).", name, n))
	}
	return nil
}

// SetPublic makes buckets public (every object readable without
// credentials) or private.
func (s *Service) SetPublic(ctx context.Context, names []string, public bool) error {
	for _, name := range names {
		b, err := s.Garage.BucketInfo(ctx, name)
		if err != nil {
			return err
		}
		if err := s.Garage.SetPublic(ctx, b.ID, public); err != nil {
			return err
		}
		if public {
			s.Log.Success(i18n.Tf("Bucket %s is public: %s/<key>", name, s3.PublicURL(name)))
		} else {
			s.Log.Success(i18n.Tf("Bucket %s is private.", name))
		}
	}
	if public {
		s.warnPublic()
	}
	return nil
}

// --- Keys --------------------------------------------------------------------

// ErrKeyName is raised for an empty key name or one with a '/'.
var ErrKeyName = i18n.NewError("The key name must not be empty nor contain '/'.")

// Key is an access key with its secret.
type Key struct {
	ID, Name, Secret string
	Perms            s3.Perms // on its bucket, for a key just created
}

// CreateKey creates a key named <bucket>/<label>, allowed on bucket only,
// reading only when readOnly.
func (s *Service) CreateKey(ctx context.Context, bucket, label string, readOnly bool) (Key, error) {
	label = strings.TrimSpace(label)
	if label == "" || strings.Contains(label, "/") {
		return Key{}, ErrKeyName
	}
	b, err := s.Garage.BucketInfo(ctx, bucket)
	if err != nil {
		return Key{}, err
	}
	k, err := s.Garage.CreateKey(ctx, bucket+"/"+label)
	if err != nil {
		return Key{}, err
	}
	perms := s3.Perms{Read: true, Write: !readOnly}
	if err := s.Garage.Allow(ctx, b.ID, k.ID, perms); err != nil {
		return Key{}, err
	}
	return Key{ID: k.ID, Name: k.Name, Secret: k.Secret, Perms: perms}, nil
}

// Grant is a bucket a key is allowed on.
type Grant struct {
	Bucket string
	Perms  s3.Perms
}

// KeyEntry is an access key and its buckets.
type KeyEntry struct {
	ID, Name string
	Global   bool // the global key, allowed on every bucket
	Grants   []Grant
}

// Keys returns the access keys, or only those allowed on bucket when not
// "".
func (s *Service) Keys(ctx context.Context, bucket string) ([]KeyEntry, error) {
	var out []KeyEntry
	if bucket != "" {
		b, err := s.Garage.BucketInfo(ctx, bucket)
		if err != nil {
			return nil, err
		}
		for _, k := range b.Keys {
			out = append(out, KeyEntry{ID: k.ID, Name: k.Name, Grants: []Grant{{Bucket: bucket, Perms: k.Permissions}}})
		}
		return out, nil
	}
	keys, err := s.Garage.Keys(ctx)
	if err != nil {
		return nil, err
	}
	global := s.Env.Get(s3.AccessKeyKey)
	for _, summary := range keys {
		k, err := s.Garage.KeyInfo(ctx, summary.ID)
		if err != nil {
			return nil, err
		}
		entry := KeyEntry{ID: k.ID, Name: k.Name, Global: k.ID == global}
		for _, b := range k.Buckets {
			name := b.ID
			if len(b.GlobalAliases) > 0 {
				name = b.GlobalAliases[0]
			}
			entry.Grants = append(entry.Grants, Grant{Bucket: name, Perms: b.Permissions})
		}
		out = append(out, entry)
	}
	return out, nil
}

// ShowKey returns an access key with its secret.
func (s *Service) ShowKey(ctx context.Context, id string) (Key, error) {
	k, err := s.Garage.KeyInfo(ctx, id)
	if s3.IsNotFound(err) {
		return Key{}, i18n.Errorf("Access key '%s' not found.", id)
	} else if err != nil {
		return Key{}, err
	}
	return Key{ID: k.ID, Name: k.Name, Secret: k.Secret}, nil
}

// DeleteKeys deletes access keys. The global key cannot be deleted: it
// comes from the configuration.
func (s *Service) DeleteKeys(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if id == s.Env.Get(s3.AccessKeyKey) {
			return i18n.Errorf("%s is the global key: change S3_ACCESS_KEY_ID instead.", id)
		}
	}
	for _, id := range ids {
		if err := s.Garage.DeleteKey(ctx, id); s3.IsNotFound(err) {
			return i18n.Errorf("Access key '%s' not found.", id)
		} else if err != nil {
			return err
		}
		s.Log.Success(i18n.Tf("Access key %s deleted.", id))
	}
	return nil
}

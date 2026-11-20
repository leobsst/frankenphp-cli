package buckets

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/s3"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// ObjectInfo is an object of a bucket as the dashboard shows it.
type ObjectInfo struct {
	s3.Object
	// Public tells that everyone reads it: its ACL, or a public bucket
	Public bool `json:"public"`
	// URL is where everyone reads it, public objects only
	URL string `json:"url,omitempty"`
}

// ObjectPage is a page of a bucket, one folder deep.
type ObjectPage struct {
	Bucket    string       `json:"bucket"`
	Prefix    string       `json:"prefix"`
	Folders   []string     `json:"folders"`
	Objects   []ObjectInfo `json:"objects"`
	NextToken string       `json:"next_token,omitempty"`
}

// objects returns the S3 client of the global key.
func (s *Service) objects() *s3.Objects { return s3.NewObjects(s.Env) }

// bucket returns the bucket called name, an error when it does not exist.
func (s *Service) bucket(ctx context.Context, name string) (s3.Bucket, error) {
	b, err := s.Garage.BucketInfo(ctx, name)
	if s3.IsNotFound(err) {
		return b, i18n.Errorf("Bucket '%s' does not exist.", name)
	}
	return b, err
}

// Objects returns a page of the objects of bucket under prefix, with
// their visibility. token continues a previous page.
func (s *Service) Objects(ctx context.Context, bucket, prefix, token string) (ObjectPage, error) {
	b, err := s.bucket(ctx, bucket)
	if err != nil {
		return ObjectPage{}, err
	}
	l, err := s.objects().List(ctx, bucket, prefix, token, 1000)
	if err != nil {
		return ObjectPage{}, err
	}
	page := ObjectPage{Bucket: bucket, Prefix: l.Prefix, Folders: l.Folders, Objects: []ObjectInfo{}, NextToken: l.NextToken}
	for _, o := range l.Objects {
		info := ObjectInfo{Object: o, Public: b.WebsiteAccess || s3.ObjectPublic(s.objectsDir(), b.ID, o.Key)}
		if info.Public {
			info.URL = s3.PublicObjectURL(bucket, o.Key)
		}
		page.Objects = append(page.Objects, info)
	}
	return page, nil
}

// checkKey refuses an empty object key or one with a path trick.
func checkKey(key string) error {
	if key == "" || strings.HasPrefix(key, "/") || len(key) > 1024 {
		return i18n.Errorf("Invalid object key '%s'.", key)
	}
	return nil
}

// Upload stores an object, private unless public.
func (s *Service) Upload(ctx context.Context, bucket, key, contentType string, body io.Reader, size int64, public bool) (ObjectInfo, error) {
	if err := checkKey(key); err != nil {
		return ObjectInfo{}, err
	}
	b, err := s.bucket(ctx, bucket)
	if err != nil {
		return ObjectInfo{}, err
	}
	if err := s.objects().Upload(ctx, bucket, key, contentType, body, size, public); err != nil {
		return ObjectInfo{}, err
	}
	s.Log.Success(i18n.Tf("Uploaded %s to %s.", key, bucket))
	info := ObjectInfo{Object: s3.Object{Key: key, Size: size, LastModified: time.Now().UTC()},
		Public: public || b.WebsiteAccess}
	if info.Public {
		info.URL = s3.PublicObjectURL(bucket, key)
	}
	return info, nil
}

// DeleteObjects deletes objects of bucket.
func (s *Service) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	for _, k := range keys {
		if err := checkKey(k); err != nil {
			return err
		}
	}
	if _, err := s.bucket(ctx, bucket); err != nil {
		return err
	}
	if err := s.objects().Delete(ctx, bucket, keys); err != nil {
		return err
	}
	s.Log.Success(i18n.Tf("Deleted %d object(s) from %s.", len(keys), bucket))
	return nil
}

// SetObjectsPublic makes objects public-read or private.
func (s *Service) SetObjectsPublic(ctx context.Context, bucket string, keys []string, public bool) error {
	for _, k := range keys {
		if err := checkKey(k); err != nil {
			return err
		}
	}
	b, err := s.bucket(ctx, bucket)
	if err != nil {
		return err
	}
	o := s.objects()
	for _, k := range keys {
		if err := o.SetPublic(ctx, bucket, k, public); err != nil {
			return err
		}
	}
	if !public && b.WebsiteAccess {
		s.Log.Warning(i18n.Tf("Bucket '%s' is public: its objects stay readable by everyone.", bucket))
	}
	return nil
}

// Link is where an object can be read: its public URL, or a temporary
// presigned one for a private object.
type Link struct {
	URL       string     `json:"url"`
	Public    bool       `json:"public"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// MaxLinkDuration is the longest validity of a temporary link (the limit
// of SigV4).
const MaxLinkDuration = 7 * 24 * time.Hour

// ObjectLink returns the link of an object; expires is the validity of a
// temporary link.
func (s *Service) ObjectLink(ctx context.Context, bucket, key string, expires time.Duration) (Link, error) {
	if err := checkKey(key); err != nil {
		return Link{}, err
	}
	b, err := s.bucket(ctx, bucket)
	if err != nil {
		return Link{}, err
	}
	if b.WebsiteAccess || s3.ObjectPublic(s.objectsDir(), b.ID, key) {
		return Link{URL: s3.PublicObjectURL(bucket, key), Public: true}, nil
	}
	if expires <= 0 || expires > MaxLinkDuration {
		expires = time.Hour
	}
	o := s.objects()
	at := o.Now().Add(expires).UTC()
	return Link{URL: o.PresignedURL(bucket, key, expires), ExpiresAt: &at}, nil
}

// Download opens an object to send it; the caller closes the body.
func (s *Service) Download(ctx context.Context, bucket, key string) (*http.Response, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	return s.objects().Download(ctx, bucket, key)
}

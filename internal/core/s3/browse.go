package s3

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/sigv4"
)

// Object is an object of a bucket.
type Object struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
	ETag         string    `json:"etag"`
}

// Listing is a page of a bucket, one "folder" deep: the objects under
// Prefix and the sub-prefixes ("folders"), NextToken set when more follow.
type Listing struct {
	Prefix    string   `json:"prefix"`
	Folders   []string `json:"folders"`
	Objects   []Object `json:"objects"`
	NextToken string   `json:"next_token,omitempty"`
}

// List returns a page of the objects of bucket under prefix, grouped at
// the next "/" (ListObjectsV2). token continues a previous page.
func (o *Objects) List(ctx context.Context, bucket, prefix, token string, max int) (Listing, error) {
	if max <= 0 || max > 1000 {
		max = 1000
	}
	q := url.Values{"list-type": {"2"}, "max-keys": {strconv.Itoa(max)}, "delimiter": {"/"}}
	if prefix != "" {
		q.Set("prefix", prefix)
	}
	if token != "" {
		q.Set("continuation-token", token)
	}
	var out struct {
		Contents []struct {
			Key          string    `xml:"Key"`
			Size         int64     `xml:"Size"`
			LastModified time.Time `xml:"LastModified"`
			ETag         string    `xml:"ETag"`
		} `xml:"Contents"`
		CommonPrefixes []struct {
			Prefix string `xml:"Prefix"`
		} `xml:"CommonPrefixes"`
		IsTruncated           bool   `xml:"IsTruncated"`
		NextContinuationToken string `xml:"NextContinuationToken"`
	}
	if err := o.do(ctx, http.MethodGet, bucket, q, nil, &out); err != nil {
		return Listing{}, err
	}
	l := Listing{Prefix: prefix, Folders: []string{}, Objects: []Object{}}
	for _, p := range out.CommonPrefixes {
		l.Folders = append(l.Folders, p.Prefix)
	}
	for _, c := range out.Contents {
		l.Objects = append(l.Objects, Object{Key: c.Key, Size: c.Size, LastModified: c.LastModified,
			ETag: strings.Trim(c.ETag, `"`)})
	}
	if out.IsTruncated {
		l.NextToken = out.NextContinuationToken
	}
	return l, nil
}

// Delete deletes objects, 1000 per request.
func (o *Objects) Delete(ctx context.Context, bucket string, keys []string) error {
	for len(keys) > 0 {
		n := min(len(keys), 1000)
		if err := o.deleteBatch(ctx, bucket, keys[:n]); err != nil {
			return err
		}
		keys = keys[n:]
	}
	return nil
}

// Public reports whether an object is readable by everyone (its ACL
// grants READ to AllUsers).
func (o *Objects) Public(ctx context.Context, bucket, key string) (bool, error) {
	var out struct {
		Grants []struct {
			URI        string `xml:"Grantee>URI"`
			Permission string `xml:"Permission"`
		} `xml:"AccessControlList>Grant"`
	}
	if err := o.do(ctx, http.MethodGet, bucket+"/"+key, url.Values{"acl": {""}}, nil, &out); err != nil {
		return false, err
	}
	for _, g := range out.Grants {
		if strings.HasSuffix(g.URI, "/AllUsers") && (g.Permission == "READ" || g.Permission == "FULL_CONTROL") {
			return true, nil
		}
	}
	return false, nil
}

// SetPublic makes an object public-read or private (canned ACL).
func (o *Objects) SetPublic(ctx context.Context, bucket, key string, public bool) error {
	acl := "private"
	if public {
		acl = "public-read"
	}
	return o.send(ctx, http.MethodPut, bucket+"/"+key, url.Values{"acl": {""}}, nil, -1,
		map[string]string{"X-Amz-Acl": acl})
}

// Upload stores body (size bytes, -1 when unknown) as key, private or
// public-read. The body streams through, unsigned.
func (o *Objects) Upload(ctx context.Context, bucket, key, contentType string, body io.Reader, size int64, public bool) error {
	headers := map[string]string{}
	if contentType != "" {
		headers["Content-Type"] = contentType
	}
	if public {
		headers["X-Amz-Acl"] = "public-read"
	}
	return o.send(ctx, http.MethodPut, bucket+"/"+key, nil, body, size, headers)
}

// send sends a signed request with an unsigned (streamed) body and checks
// the status.
func (o *Objects) send(ctx context.Context, method, path string, query url.Values, body io.Reader,
	size int64, headers map[string]string) error {
	res, err := o.open(ctx, method, path, query, body, size, headers)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, res.Body)
	return res.Body.Close()
}

// open sends a signed request with an unsigned body and returns the
// answer, an error for a failure status; the caller closes its body.
func (o *Objects) open(ctx context.Context, method, path string, query url.Values, body io.Reader,
	size int64, headers map[string]string) (*http.Response, error) {
	uri := "/" + sigv4.EncodePath(path)
	rawQuery := sigv4.CanonicalQuery(query)
	u := o.Endpoint + uri
	if rawQuery != "" {
		u += "?" + rawQuery
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if size >= 0 && body != nil {
		req.ContentLength = size
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c := sigv4.Credentials{AccessKey: o.AccessKey, SecretKey: o.SecretKey, Region: o.Region}
	sigv4.Sign(req, c, uri, rawQuery, sigv4.UnsignedPayload, o.Now())
	client := o.HTTP
	if body != nil || method == http.MethodGet {
		// Uploads and downloads may take long: no overall timeout
		client = &http.Client{Transport: o.HTTP.Transport}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		defer func() { _ = res.Body.Close() }()
		var e struct {
			Code    string `xml:"Code"`
			Message string `xml:"Message"`
		}
		data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
		_ = xml.Unmarshal(data, &e)
		return nil, &ObjectError{Status: res.StatusCode, Code: e.Code, Message: e.Message}
	}
	return res, nil
}

// ObjectError is an error answered by the S3 server.
type ObjectError struct {
	Status        int
	Code, Message string
}

func (e *ObjectError) Error() string {
	return fmt.Sprintf("S3: %s %s (HTTP %d)", e.Code, e.Message, e.Status)
}

// Download opens an object: its body, type and size. The caller closes
// the body.
func (o *Objects) Download(ctx context.Context, bucket, key string) (*http.Response, error) {
	return o.open(ctx, http.MethodGet, bucket+"/"+key, nil, nil, -1, nil)
}

// PublicObjectURL is where everyone reads a public object (path style,
// through the reverse proxy).
func PublicObjectURL(bucket, key string) string {
	return "https://" + Domain + "/" + sigv4.EncodePath(bucket+"/"+key)
}

// PresignedURL is a temporary link to a private object, through the
// reverse proxy, valid for expires.
func (o *Objects) PresignedURL(bucket, key string, expires time.Duration) string {
	c := sigv4.Credentials{AccessKey: o.AccessKey, SecretKey: o.SecretKey, Region: o.Region}
	uri := "/" + sigv4.EncodePath(bucket+"/"+key)
	return "https://" + Domain + uri + "?" + sigv4.Presign(c, http.MethodGet, Domain, uri, expires, o.Now())
}

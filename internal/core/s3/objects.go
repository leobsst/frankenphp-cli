package s3

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // Content-MD5 is required by DeleteObjects
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/sigv4"
)

// Objects talks to the S3 API with SigV4-signed requests, path style. Only
// what emptying a bucket needs: list the objects, delete them.
type Objects struct {
	Endpoint  string // http://127.0.0.1:3900
	Region    string
	AccessKey string
	SecretKey string
	HTTP      *http.Client
	Now       func() time.Time
}

// NewObjects returns the S3 client of the configured server, with the
// global key.
func NewObjects(c Getter) *Objects {
	return &Objects{
		Endpoint:  Endpoint(c),
		Region:    Region(c),
		AccessKey: strings.TrimSpace(c.Get(AccessKeyKey)),
		SecretKey: strings.TrimSpace(c.Get(SecretKeyKey)),
		HTTP:      &http.Client{Timeout: 60 * time.Second},
		Now:       time.Now,
	}
}

// Empty deletes every object of bucket and the unfinished multipart
// uploads. It returns the number of deleted objects.
func (o *Objects) Empty(ctx context.Context, bucket string) (int, error) {
	deleted := 0
	for {
		keys, err := o.list(ctx, bucket)
		if err != nil {
			return deleted, err
		}
		if len(keys) == 0 {
			break
		}
		if err := o.deleteBatch(ctx, bucket, keys); err != nil {
			return deleted, err
		}
		deleted += len(keys)
	}
	return deleted, o.abortUploads(ctx, bucket)
}

// list returns up to 1000 object keys of bucket.
func (o *Objects) list(ctx context.Context, bucket string) ([]string, error) {
	var out struct {
		Contents []struct {
			Key string `xml:"Key"`
		} `xml:"Contents"`
	}
	q := url.Values{"list-type": {"2"}, "max-keys": {"1000"}}
	if err := o.do(ctx, http.MethodGet, bucket, q, nil, &out); err != nil {
		return nil, err
	}
	keys := make([]string, len(out.Contents))
	for i, c := range out.Contents {
		keys[i] = c.Key
	}
	return keys, nil
}

type deleteRequest struct {
	XMLName xml.Name       `xml:"Delete"`
	Quiet   bool           `xml:"Quiet"`
	Objects []deleteObject `xml:"Object"`
}

type deleteObject struct {
	Key string `xml:"Key"`
}

// deleteBatch deletes up to 1000 objects in one request.
func (o *Objects) deleteBatch(ctx context.Context, bucket string, keys []string) error {
	req := deleteRequest{Quiet: true}
	for _, k := range keys {
		req.Objects = append(req.Objects, deleteObject{Key: k})
	}
	body, err := xml.Marshal(req)
	if err != nil {
		return err
	}
	var out struct {
		Errors []struct {
			Key     string `xml:"Key"`
			Message string `xml:"Message"`
		} `xml:"Error"`
	}
	if err := o.do(ctx, http.MethodPost, bucket, url.Values{"delete": {""}}, body, &out); err != nil {
		return err
	}
	if len(out.Errors) > 0 {
		return fmt.Errorf("S3: %s: %s", out.Errors[0].Key, out.Errors[0].Message)
	}
	return nil
}

// abortUploads aborts the unfinished multipart uploads, which keep a
// bucket from being deleted.
func (o *Objects) abortUploads(ctx context.Context, bucket string) error {
	var out struct {
		Uploads []struct {
			Key      string `xml:"Key"`
			UploadID string `xml:"UploadId"`
		} `xml:"Upload"`
	}
	if err := o.do(ctx, http.MethodGet, bucket, url.Values{"uploads": {""}}, nil, &out); err != nil {
		return err
	}
	for _, u := range out.Uploads {
		if err := o.do(ctx, http.MethodDelete, bucket+"/"+u.Key, url.Values{"uploadId": {u.UploadID}}, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// do sends a signed request to /<path> and decodes the XML answer into
// out (nil to ignore it).
func (o *Objects) do(ctx context.Context, method, path string, query url.Values, body []byte, out any) error {
	uri := "/" + sigv4.EncodePath(path)
	rawQuery := sigv4.CanonicalQuery(query)
	u := o.Endpoint + uri
	if rawQuery != "" {
		u += "?" + rawQuery
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		sum := md5.Sum(body) //nolint:gosec // see the import
		req.Header.Set("Content-MD5", base64.StdEncoding.EncodeToString(sum[:]))
		req.Header.Set("Content-Type", "application/xml")
	}
	o.sign(req, uri, rawQuery, body)
	res, err := o.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		var e struct {
			Code    string `xml:"Code"`
			Message string `xml:"Message"`
		}
		_ = xml.Unmarshal(data, &e)
		return fmt.Errorf("S3: %s %s (HTTP %d)", e.Code, e.Message, res.StatusCode)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return xml.Unmarshal(data, out)
}

// sign adds the AWS Signature Version 4 headers to req.
func (o *Objects) sign(req *http.Request, uri, rawQuery string, body []byte) {
	c := sigv4.Credentials{AccessKey: o.AccessKey, SecretKey: o.SecretKey, Region: o.Region}
	sigv4.Sign(req, c, uri, rawQuery, sigv4.SHA256Hex(body), o.Now())
}

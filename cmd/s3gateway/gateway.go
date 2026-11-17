package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/sigv4"
)

// Config is the configuration of the gateway.
type Config struct {
	Listen     string // address the gateway listens on
	S3URL      string // Garage's S3 API
	WebURL     string // Garage's website endpoint (public buckets)
	AdminURL   string // Garage's admin API
	AdminToken string
	AccessKey  string // the global key, which reads the public objects
	SecretKey  string
	Region     string
	// RootDomain makes <bucket>.RootDomain a virtual-host style host, as
	// in Garage's root_domain. Empty: path style only.
	RootDomain string
	ACLDir     string
}

// Gateway is the S3 ACL gateway.
type Gateway struct {
	cfg    Config
	s3     *url.URL
	web    *url.URL
	admin  *Admin
	store  *Store
	client *http.Client
	now    func() time.Time
	// transport forwards to Garage
	transport http.RoundTripper
}

// New returns the gateway of cfg.
func New(cfg Config) (*Gateway, error) {
	s3, err := url.Parse(cfg.S3URL)
	if err != nil || s3.Host == "" {
		return nil, fmt.Errorf("invalid GARAGE_S3_URL %q", cfg.S3URL)
	}
	web, err := url.Parse(cfg.WebURL)
	if err != nil || web.Host == "" {
		return nil, fmt.Errorf("invalid GARAGE_WEB_URL %q", cfg.WebURL)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 64
	// Garage answers the body itself: no gzip negotiation in between
	transport.DisableCompression = true
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	return &Gateway{
		cfg:       cfg,
		s3:        s3,
		web:       web,
		admin:     &Admin{BaseURL: strings.TrimRight(cfg.AdminURL, "/"), Token: cfg.AdminToken, HTTP: client, Now: time.Now},
		store:     &Store{dir: cfg.ACLDir},
		client:    client,
		now:       time.Now,
		transport: transport,
	}, nil
}

// target is the bucket and the object key of a request.
type target struct {
	bucket, key string
}

// parseTarget finds the bucket in the host (virtual-host style) or in the
// first path segment (path style).
func (g *Gateway) parseTarget(r *http.Request) target {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if root := g.cfg.RootDomain; root != "" {
		if bucket, ok := strings.CutSuffix(strings.ToLower(host), "."+root); ok && bucket != "" {
			return target{bucket: bucket, key: path}
		}
	}
	bucket, key, _ := strings.Cut(path, "/")
	return target{bucket: bucket, key: key}
}

// resource is the S3 resource of a target, for the error answers.
func (t target) resource() string {
	if t.key == "" {
		return "/" + t.bucket
	}
	return "/" + t.bucket + "/" + t.key
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == HealthPath {
		w.WriteHeader(http.StatusOK)
		return
	}
	t := g.parseTarget(r)
	q := r.URL.Query()
	switch {
	case sigv4.Anonymous(r):
		if t.bucket != "" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			g.serveAnonymous(w, r, t)
			return
		}
		// Garage denies it
		g.forward(w, r, nil)
	case t.key != "" && q.Has("acl") && r.Method == http.MethodGet:
		g.getObjectACL(w, r, t)
	case t.key != "" && q.Has("acl") && r.Method == http.MethodPut:
		g.putObjectACL(w, r, t)
	default:
		g.forwardTracked(w, r, t, q)
	}
}

// forward sends r to Garage's S3 API untouched (same Host, so that its
// signature holds) and calls after with the response, before it is sent.
func (g *Gateway) forward(w http.ResponseWriter, r *http.Request, after func(*http.Response) error) {
	p := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(g.s3)
			pr.Out.Host = pr.In.Host
		},
		Transport:      g.transport,
		FlushInterval:  -1,
		ModifyResponse: after,
		ErrorHandler:   g.proxyError,
	}
	p.ServeHTTP(w, r)
}

func (g *Gateway) proxyError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
	writeError(w, r, http.StatusBadGateway, "ServiceUnavailable", "The S3 server is not reachable.", "")
}

// subresources are the object subresources whose PUT does not write the
// object itself.
var subresources = []string{"tagging", "retention", "legal-hold", "acl", "torrent", "partNumber", "uploadId"}

// forwardTracked forwards a signed request and, once Garage accepted it,
// records the ACL of the objects it writes or forgets the ones it deletes.
func (g *Gateway) forwardTracked(w http.ResponseWriter, r *http.Request, t target, q url.Values) {
	var after func(bucketID string) func(*http.Response) error
	switch {
	case t.bucket == "":
	case t.key == "" && r.Method == http.MethodPost && q.Has("delete"):
		after = g.trackDeleteObjects(w, r, t)
		if after == nil {
			return
		}
	case t.key == "" && r.Method == http.MethodDelete && len(q) == 0:
		after = func(id string) func(*http.Response) error {
			return onSuccess(func(*http.Response) error {
				g.admin.Forget(t.bucket)
				return g.store.DropBucket(id)
			})
		}
	case t.key == "":
	case r.Method == http.MethodPut && !hasAny(q, subresources):
		// PutObject or CopyObject: the new object gets the ACL of the
		// request, private when none, as on AWS
		public := publicFromHeaders(r.Header)
		after = func(id string) func(*http.Response) error {
			return onSuccess(func(*http.Response) error { return g.store.Set(id, t.key, public) })
		}
	case r.Method == http.MethodPost && q.Has("uploads"):
		public := publicFromHeaders(r.Header)
		after = func(string) func(*http.Response) error {
			return onSuccess(func(res *http.Response) error {
				var out struct {
					UploadID string `xml:"UploadId"`
				}
				if err := readXML(res, &out); err != nil || out.UploadID == "" {
					return errors.Join(errors.New("no UploadId in the answer"), err)
				}
				return g.store.SetPending(out.UploadID, public)
			})
		}
	case r.Method == http.MethodPost && q.Has("uploadId"):
		uploadID := q.Get("uploadId")
		after = func(id string) func(*http.Response) error {
			return onSuccess(func(res *http.Response) error {
				// The answer can be an error despite the 200
				var out struct {
					XMLName xml.Name
				}
				if err := readXML(res, &out); err != nil || out.XMLName.Local == "Error" {
					return err
				}
				public, err := g.store.TakePending(uploadID)
				if err != nil {
					return err
				}
				return g.store.Set(id, t.key, public)
			})
		}
	case r.Method == http.MethodDelete && q.Has("uploadId"):
		uploadID := q.Get("uploadId")
		after = func(string) func(*http.Response) error {
			return onSuccess(func(*http.Response) error {
				_, err := g.store.TakePending(uploadID)
				return err
			})
		}
	case r.Method == http.MethodDelete && !hasAny(q, subresources):
		after = func(id string) func(*http.Response) error {
			return onSuccess(func(*http.Response) error { return g.store.Set(id, t.key, false) })
		}
	}
	if after == nil {
		g.forward(w, r, nil)
		return
	}
	bucket, err := g.admin.Bucket(r.Context(), t.bucket)
	if err != nil {
		// Unknown bucket: Garage answers the error, nothing to record
		if !errors.Is(err, errNotFound) {
			log.Printf("bucket %s: %v", t.bucket, err)
		}
		g.forward(w, r, nil)
		return
	}
	track := after(bucket.ID)
	g.forward(w, r, func(res *http.Response) error {
		if err := track(res); err != nil {
			// The write happened: answer it, the ACL may be stale
			log.Printf("ACL of %s %s not recorded: %v", r.Method, t.resource(), err)
		}
		return nil
	})
}

// trackDeleteObjects reads the keys of a DeleteObjects request, then
// returns what forgets the deleted ones. It answers the request itself and
// returns nil when the body cannot be read.
func (g *Gateway) trackDeleteObjects(w http.ResponseWriter, r *http.Request, t target) func(string) func(*http.Response) error {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "IncompleteBody", "The request body could not be read.", t.resource())
		return nil
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var req struct {
		Objects []struct {
			Key string `xml:"Key"`
		} `xml:"Object"`
	}
	if body, err := decodedBody(r, raw); err == nil {
		_ = xml.Unmarshal(body, &req)
	}
	return func(id string) func(*http.Response) error {
		return onSuccess(func(res *http.Response) error {
			var out struct {
				Errors []struct {
					Key string `xml:"Key"`
				} `xml:"Error"`
			}
			if err := readXML(res, &out); err != nil {
				return err
			}
			failed := map[string]bool{}
			for _, e := range out.Errors {
				failed[e.Key] = true
			}
			var errs []error
			for _, o := range req.Objects {
				if !failed[o.Key] {
					errs = append(errs, g.store.Set(id, o.Key, false))
				}
			}
			return errors.Join(errs...)
		})
	}
}

// onSuccess runs f on the 2xx answers only.
func onSuccess(f func(*http.Response) error) func(*http.Response) error {
	return func(res *http.Response) error {
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return nil
		}
		return f(res)
	}
}

// readXML reads the body of res into out and puts it back for the client.
func readXML(res *http.Response, out any) error {
	data, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	_ = res.Body.Close()
	res.Body = io.NopCloser(bytes.NewReader(data))
	if err != nil {
		return err
	}
	return xml.Unmarshal(data, out)
}

func hasAny(q url.Values, keys []string) bool {
	for _, k := range keys {
		if q.Has(k) {
			return true
		}
	}
	return false
}

// serveAnonymous answers a GET or HEAD without credentials: the website
// endpoint for a public bucket, the object read with the global key when
// it is public, AccessDenied otherwise (as AWS, even for a missing key).
func (g *Gateway) serveAnonymous(w http.ResponseWriter, r *http.Request, t target) {
	bucket, err := g.admin.Bucket(r.Context(), t.bucket)
	switch {
	case errors.Is(err, errNotFound):
		writeError(w, r, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", t.resource())
		return
	case err != nil:
		log.Printf("bucket %s: %v", t.bucket, err)
		writeError(w, r, http.StatusServiceUnavailable, "ServiceUnavailable", "The S3 server is not ready.", t.resource())
		return
	case bucket.WebsiteAccess:
		g.serveWebsite(w, r, t)
		return
	}
	public := false
	if t.key != "" {
		if public, err = g.store.Public(bucket.ID, t.key); err != nil {
			log.Printf("ACL of %s: %v", t.resource(), err)
		}
	}
	if !public {
		writeError(w, r, http.StatusForbidden, "AccessDenied", "Access Denied", t.resource())
		return
	}
	g.serveSigned(w, r, t)
}

// serveWebsite forwards to the website endpoint, which finds the bucket in
// the host and serves index.html for the directories.
func (g *Gateway) serveWebsite(w http.ResponseWriter, r *http.Request, t target) {
	p := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(g.web)
			pr.Out.URL.Path, pr.Out.URL.RawPath, pr.Out.URL.RawQuery = "/"+t.key, "", ""
			pr.Out.Host = t.bucket
			if g.cfg.RootDomain != "" {
				pr.Out.Host = t.bucket + "." + g.cfg.RootDomain
			}
		},
		Transport:     g.transport,
		FlushInterval: -1,
		ErrorHandler:  g.proxyError,
	}
	p.ServeHTTP(w, r)
}

// forwardedHeaders are the anonymous request headers kept when the object
// is read with the global key.
var forwardedHeaders = []string{
	"Range", "If-Match", "If-None-Match", "If-Modified-Since", "If-Unmodified-Since", "Accept-Encoding",
}

// serveSigned reads a public object with the global key, path style.
func (g *Gateway) serveSigned(w http.ResponseWriter, r *http.Request, t target) {
	p := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			uri := "/" + sigv4.EncodePath(t.bucket+"/"+t.key)
			out := pr.Out
			out.URL = &url.URL{Scheme: g.s3.Scheme, Host: g.s3.Host, Path: "/" + t.bucket + "/" + t.key, RawPath: uri}
			out.Host = g.s3.Host
			header := http.Header{}
			for _, name := range forwardedHeaders {
				if v := pr.In.Header.Values(name); len(v) > 0 {
					header[name] = v
				}
			}
			out.Header = header
			creds := sigv4.Credentials{AccessKey: g.cfg.AccessKey, SecretKey: g.cfg.SecretKey, Region: g.cfg.Region}
			sigv4.Sign(out, creds, uri, "", sigv4.SHA256Hex(nil), g.now())
		},
		Transport:     g.transport,
		FlushInterval: -1,
		ErrorHandler:  g.proxyError,
	}
	p.ServeHTTP(w, r)
}

// objectExists asks Garage, with the global key, whether an object exists.
func (g *Gateway) objectExists(ctx context.Context, t target) (bool, error) {
	uri := "/" + sigv4.EncodePath(t.bucket+"/"+t.key)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, g.s3.Scheme+"://"+g.s3.Host+uri, nil)
	if err != nil {
		return false, err
	}
	creds := sigv4.Credentials{AccessKey: g.cfg.AccessKey, SecretKey: g.cfg.SecretKey, Region: g.cfg.Region}
	sigv4.Sign(req, creds, uri, "", sigv4.SHA256Hex(nil), g.now())
	res, err := g.client.Do(req)
	if err != nil {
		return false, err
	}
	_ = res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return false, nil
	case res.StatusCode >= 300:
		return false, fmt.Errorf("HEAD %s: HTTP %d", uri, res.StatusCode)
	}
	return true, nil
}

// writeError answers an S3 error (no body for HEAD).
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message, resource string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	var b strings.Builder
	b.WriteString(xml.Header + "<Error><Code>")
	_ = xml.EscapeText(&b, []byte(code))
	b.WriteString("</Code><Message>")
	_ = xml.EscapeText(&b, []byte(message))
	b.WriteString("</Message><Resource>")
	_ = xml.EscapeText(&b, []byte(resource))
	b.WriteString("</Resource></Error>")
	_, _ = io.WriteString(w, b.String())
}

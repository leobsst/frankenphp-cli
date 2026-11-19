package daemon

import (
	"context"
	"net/http"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/buckets"
	"github.com/leobsst/frankenphp-cli/internal/core/caddyfile"
	"github.com/leobsst/frankenphp-cli/internal/core/caserver"
	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/dnsclients"
	"github.com/leobsst/frankenphp-cli/internal/core/events"
	"github.com/leobsst/frankenphp-cli/internal/core/s3"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// --- Archived hosts ----------------------------------------------------------

// ArchivedHost is a host whose site files are archived, ready to restore.
type ArchivedHost struct {
	Domain string `json:"domain"`
}

func (s *Server) archivedHosts(w http.ResponseWriter, _ *http.Request) {
	s.read(w, func(ws *Workspace) (any, error) {
		g := caddyfile.New(ws.Env.ResolveDir("CADDY_DIR", "./caddy", ws.Dir), ws.Dir, events.NewLog(nil))
		archived, err := g.ListArchived()
		if err != nil {
			return nil, err
		}
		out := []ArchivedHost{}
		for _, a := range archived {
			out = append(out, ArchivedHost{Domain: a.FullDomain})
		}
		return out, nil
	})
}

// --- LAN DNS devices ---------------------------------------------------------

type dnsClientsResponse struct {
	Enabled bool `json:"enabled"`
	// State is the state of the LAN DNS server process
	State     string              `json:"state"`
	Connected int                 `json:"connected"`
	Devices   []dnsclients.Device `json:"devices"`
}

// dnsClients lists the devices of the LAN DNS server: the connected ones,
// all of them with ?all=true.
func (s *Server) dnsClients(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "true"
	s.read(w, func(ws *Workspace) (any, error) {
		res := dnsClientsResponse{Enabled: dns.Enabled(ws.Env), Devices: []dnsclients.Device{}}
		if !res.Enabled {
			return res, nil
		}
		res.State = ws.Stack.DNSProcessState(r.Context())
		stored, err := ws.Store.DNSClients()
		if err != nil {
			return nil, err
		}
		res.Devices, res.Connected = dnsclients.Devices(stored, time.Now(), all)
		return res, nil
	})
}

// --- LAN DNS server ---

// DNSState is whether the LAN DNS server is on and serves.
type DNSState struct {
	Enabled bool `json:"enabled"`
	Running bool `json:"running"`
}

func (s *Server) dnsState(w http.ResponseWriter, r *http.Request) {
	s.read(w, func(ws *Workspace) (any, error) {
		return DNSState{Enabled: dns.Enabled(ws.Env), Running: ws.Stack.DNS.Running(r.Context())}, nil
	})
}

// applyDNS serves when DNS_SERVER=true, stops otherwise, reading the DNS_*
// settings again.
func (s *Server) applyDNS(w http.ResponseWriter, _ *http.Request) {
	s.submit(w, "dns.apply", func(ctx context.Context, ws *Workspace) (any, error) {
		running, err := ws.Stack.DNS.Apply(ctx)
		return DNSState{Enabled: dns.Enabled(ws.Env), Running: running}, err
	})
}

// stopDNS stops serving until the next start or apply.
func (s *Server) stopDNS(w http.ResponseWriter, _ *http.Request) {
	s.submit(w, "dns.stop", func(ctx context.Context, ws *Workspace) (any, error) {
		ws.Stack.DNS.Stop(ctx)
		return DNSState{Enabled: dns.Enabled(ws.Env)}, nil
	})
}

// --- Root CA sharing ---------------------------------------------------------

type caResponse struct {
	Sharing bool   `json:"sharing"`
	Port    int    `json:"port,omitempty"`
	URL     string `json:"url,omitempty"` // where the other devices download it
}

func (s *Server) caStatus(w http.ResponseWriter, _ *http.Request) {
	s.read(w, func(ws *Workspace) (any, error) {
		url, ok := ws.CA.Status()
		_, port, _ := ws.CA.State(ws.Dir)
		return caResponse{Sharing: ok, Port: port, URL: url}, nil
	})
}

type caRequest struct {
	Enabled bool `json:"enabled"`
	// Port is the port to share on, 0 for the default
	Port int `json:"port"`
}

func (s *Server) setCA(w http.ResponseWriter, r *http.Request) {
	var req caRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.Port == 0 {
		req.Port = caserver.DefaultPort
	}
	s.submit(w, "ca.set", func(ctx context.Context, ws *Workspace) (any, error) {
		if !req.Enabled {
			return caResponse{}, ws.CA.Off()
		}
		url, err := ws.CA.On(ctx, req.Port)
		if err != nil {
			return nil, err
		}
		return caResponse{Sharing: true, Port: req.Port, URL: url}, nil
	})
}

// --- S3 ----------------------------------------------------------------------

type s3Response struct {
	Enabled bool `json:"enabled"`
	// Domain is the HTTPS host of the S3 API, AppEndpoint the endpoint of
	// the PHP apps
	Domain      string `json:"domain"`
	AppEndpoint string `json:"app_endpoint"`
	Region      string `json:"region"`
	AccessKeyID string `json:"access_key_id"` // the global key
}

func (s *Server) s3Info(w http.ResponseWriter, _ *http.Request) {
	s.read(w, func(ws *Workspace) (any, error) {
		e := ws.Env
		return s3Response{Enabled: s3.Enabled(e), Domain: s3.Domain, AppEndpoint: s3.AppEndpoint(e, e.IsProduction()),
			Region: s3.Region(e), AccessKeyID: e.Get(s3.AccessKeyKey)}, nil
	})
}

// readS3 runs fn on the S3 service, for the requests that only read.
func (s *Server) readS3(w http.ResponseWriter, r *http.Request, fn func(svc *buckets.Service) (any, error)) {
	s.read(w, func(ws *Workspace) (any, error) {
		svc, err := ws.Buckets(r.Context())
		if err != nil {
			return nil, err
		}
		return fn(svc)
	})
}

// submitS3 queues an operation on the S3 service.
func (s *Server) submitS3(w http.ResponseWriter, kind string, fn func(ctx context.Context, svc *buckets.Service) (any, error)) {
	s.submit(w, kind, func(ctx context.Context, ws *Workspace) (any, error) {
		svc, err := ws.Buckets(ctx)
		if err != nil {
			return nil, err
		}
		return fn(ctx, svc)
	})
}

// Bucket is a bucket and its usage.
type Bucket struct {
	Name    string `json:"name"`
	Public  bool   `json:"public"`
	Objects int64  `json:"objects"`
	// PublicObjects counts the objects stored public-read, -1 for a public
	// bucket (all of them)
	PublicObjects int    `json:"public_objects"`
	Bytes         int64  `json:"bytes"`
	Keys          int    `json:"keys"`
	URL           string `json:"url"`
}

func (s *Server) listBuckets(w http.ResponseWriter, r *http.Request) {
	s.readS3(w, r, func(svc *buckets.Service) (any, error) {
		list, err := svc.List(r.Context())
		if err != nil {
			return nil, err
		}
		out := []Bucket{}
		for _, b := range list {
			out = append(out, Bucket{Name: b.Name, Public: b.Public, Objects: b.Objects, PublicObjects: b.PublicObjects,
				Bytes: b.Bytes, Keys: b.Keys, URL: b.URL})
		}
		return out, nil
	})
}

type createBucketsRequest struct {
	Names  []string `json:"names"`
	Public bool     `json:"public"`
}

type createdBuckets struct {
	Created []string `json:"created"`
}

func (s *Server) createBuckets(w http.ResponseWriter, r *http.Request) {
	var req createBucketsRequest
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Names) == 0 {
		writeError(w, http.StatusBadRequest, i18n.Tf("Missing field '%s'.", "names"))
		return
	}
	for _, name := range req.Names {
		if err := s3.ValidateBucket(name); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	s.submitS3(w, "s3.buckets.create", func(ctx context.Context, svc *buckets.Service) (any, error) {
		created, err := svc.Create(ctx, req.Names, req.Public)
		if created == nil {
			created = []string{}
		}
		return createdBuckets{Created: created}, err
	})
}

// deleteBucket deletes a bucket and its keys; ?force=true empties it first.
func (s *Server) deleteBucket(w http.ResponseWriter, r *http.Request) {
	name, force := r.PathValue("name"), r.URL.Query().Get("force") == "true"
	s.submitS3(w, "s3.buckets.delete", func(ctx context.Context, svc *buckets.Service) (any, error) {
		return nil, svc.Delete(ctx, []string{name}, force)
	})
}

func (s *Server) emptyBucket(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.submitS3(w, "s3.buckets.empty", func(ctx context.Context, svc *buckets.Service) (any, error) {
		if err := svc.Check(ctx, []string{name}); err != nil {
			return nil, err
		}
		return nil, svc.Empty(ctx, []string{name})
	})
}

type publicRequest struct {
	Public bool `json:"public"`
}

func (s *Server) setBucketPublic(w http.ResponseWriter, r *http.Request) {
	var req publicRequest
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	s.submitS3(w, "s3.buckets.public", func(ctx context.Context, svc *buckets.Service) (any, error) {
		return nil, svc.SetPublic(ctx, []string{name}, req.Public)
	})
}

// Grant is a bucket an access key is allowed on.
type Grant struct {
	Bucket string `json:"bucket"`
	Read   bool   `json:"read"`
	Write  bool   `json:"write"`
	Owner  bool   `json:"owner"`
}

// AccessKey is an access key, with its secret only when created or shown.
type AccessKey struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Secret string  `json:"secret,omitempty"`
	Global bool    `json:"global"`
	Grants []Grant `json:"grants"`
}

func grantOf(bucket string, p s3.Perms) Grant {
	return Grant{Bucket: bucket, Read: p.Read, Write: p.Write, Owner: p.Owner}
}

// listKeys lists the access keys, those of a bucket with ?bucket=<name>.
func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	bucket := r.URL.Query().Get("bucket")
	s.readS3(w, r, func(svc *buckets.Service) (any, error) {
		keys, err := svc.Keys(r.Context(), bucket)
		if err != nil {
			return nil, err
		}
		out := []AccessKey{}
		for _, k := range keys {
			key := AccessKey{ID: k.ID, Name: k.Name, Global: k.Global, Grants: []Grant{}}
			for _, g := range k.Grants {
				key.Grants = append(key.Grants, grantOf(g.Bucket, g.Perms))
			}
			out = append(out, key)
		}
		return out, nil
	})
}

type createKeyRequest struct {
	Bucket   string `json:"bucket"`
	Name     string `json:"name"` // stored as <bucket>/<name>, "app" when empty
	ReadOnly bool   `json:"read_only"`
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	req := createKeyRequest{}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Bucket == "" {
		writeError(w, http.StatusBadRequest, i18n.Tf("Missing field '%s'.", "bucket"))
		return
	}
	if req.Name == "" {
		req.Name = "app"
	}
	s.submitS3(w, "s3.keys.create", func(ctx context.Context, svc *buckets.Service) (any, error) {
		k, err := svc.CreateKey(ctx, req.Bucket, req.Name, req.ReadOnly)
		if err != nil {
			return nil, err
		}
		return AccessKey{ID: k.ID, Name: k.Name, Secret: k.Secret, Grants: []Grant{grantOf(req.Bucket, k.Perms)}}, nil
	})
}

func (s *Server) showKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.readS3(w, r, func(svc *buckets.Service) (any, error) {
		k, err := svc.ShowKey(r.Context(), id)
		if err != nil {
			return nil, err
		}
		return AccessKey{ID: k.ID, Name: k.Name, Secret: k.Secret, Grants: []Grant{}}, nil
	})
}

func (s *Server) deleteKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.submitS3(w, "s3.keys.delete", func(ctx context.Context, svc *buckets.Service) (any, error) {
		return nil, svc.DeleteKeys(ctx, []string{id})
	})
}

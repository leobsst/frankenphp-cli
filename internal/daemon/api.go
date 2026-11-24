package daemon

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/buckets"
	"github.com/leobsst/frankenphp-cli/internal/core/caserver"
	"github.com/leobsst/frankenphp-cli/internal/core/domains"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/events"
	"github.com/leobsst/frankenphp-cli/internal/core/settings"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// Workspace is the configuration, the database and the services of the
// data directory, reporting to the log it was opened with.
type Workspace struct {
	Dir   string
	Env   *env.Env
	Store *store.Store
	Stack *stack.Service
	// Domains returns the hosts service; Running is checked first
	Domains func() *domains.Service
	// Buckets returns the S3 service, an error when S3 is disabled or does
	// not answer
	Buckets func(ctx context.Context) (*buckets.Service, error)
	// CA shares the root CA on the LAN
	CA    caserver.Sharing
	Close func() error
}

// Running reports whether the stack runs.
func (ws *Workspace) Running(ctx context.Context) (bool, error) {
	return stack.IsRunning(ctx, ws.Stack.Docker, ws.Store)
}

// Backend opens a workspace reporting to log. The CLI provides it, with
// the same dependencies as its commands.
type Backend func(log events.Log) (*Workspace, error)

// errRestoreKind is the error of a restore that is neither PHP nor static.
var errRestoreKind = i18n.NewError("Restore the host(s) as PHP domains (--php VERSION) or as static hosts (--static).")

// The errors of the actions that need the stack running: those that add
// hosts point to start.
var (
	errNotRunning      = i18n.NewError("The server is not running.")
	errNotRunningStart = i18n.NewError("The server is not running. Use 'start' command instead.")
)

// route is an endpoint of the API: "METHOD /path" and its handler.
type route struct {
	pattern string
	handler http.HandlerFunc
}

// routeTable lists the endpoints, as api/openapi.yaml describes them.
func (s *Server) routeTable() []route {
	return []route{
		{"GET /api/health", s.health},
		{"POST /api/daemon/stop", s.stopDaemon},
		{"GET /api/v1/host", s.hostInfo},
		{"GET /api/v1/update", s.updateInfo},
		{"GET /api/v1/auth", s.authState},
		{"POST /api/v1/auth/login", s.login},
		{"POST /api/v1/auth/local", s.localLogin},
		{"POST /api/v1/auth/pairing", s.createPairing},
		{"POST /api/v1/auth/pair", s.pair},
		{"POST /api/v1/auth/logout", s.logout},
		{"PUT /api/v1/auth/password", s.setPassword},
		{"GET /api/v1/auth/sessions", s.sessions},
		{"DELETE /api/v1/auth/sessions/{id}", s.revokeSession},
		{"GET /api/v1/auth/log", s.accessLog},
		{"GET /api/v1/status", s.status},
		{"GET /api/v1/events", s.events},
		{"GET /api/v1/operations", s.operations},
		{"GET /api/v1/operations/{id}", s.operation},
		{"POST /api/v1/stack/start", s.startStack},
		{"POST /api/v1/stack/stop", s.stopStack},
		{"POST /api/v1/stack/restart", s.restartStack},
		{"GET /api/v1/containers", s.containers},
		{"POST /api/v1/containers/{name}/{action}", s.containerAction},
		{"GET /api/v1/stats", s.stats},
		{"GET /api/v1/logs", s.logs},
		{"POST /api/v1/hosts", s.addHosts},
		{"POST /api/v1/hosts/remove", s.removeHosts},
		{"POST /api/v1/hosts/{name}/certificate", s.renewCertificate},
		{"POST /api/v1/hosts/{name}/aliases", s.addAliases},
		{"PUT /api/v1/hosts/{name}/php", s.switchPHP},
		{"PUT /api/v1/hosts/{name}/websocket", s.setWebsocket},
		{"GET /api/v1/hosts/{name}/caddyfile", s.siteFile},
		{"PUT /api/v1/hosts/{name}/caddyfile", s.saveSite},
		{"POST /api/v1/hosts/{name}/caddyfile/validate", s.validateSite},
		{"POST /api/v1/hosts/{name}/caddyfile/reset", s.resetSite},
		{"POST /api/v1/hosts/restore", s.restoreHosts},
		{"GET /api/v1/services", s.services},
		{"PUT /api/v1/services/{name}", s.setService},
		{"GET /api/v1/settings", s.settings},
		{"PUT /api/v1/settings/{key}", s.setSetting},
		{"GET /api/v1/hosts/archived", s.archivedHosts},
		{"GET /api/v1/dns", s.dnsState},
		{"POST /api/v1/dns/apply", s.applyDNS},
		{"POST /api/v1/dns/stop", s.stopDNS},
		{"GET /api/v1/dns/clients", s.dnsClients},
		{"GET /api/v1/ca", s.caStatus},
		{"PUT /api/v1/ca", s.setCA},
		{"GET /api/v1/redis", s.redisState},
		{"GET /api/v1/redis/keys", s.redisKeys},
		{"POST /api/v1/redis/keys/delete", s.deleteRedisKeys},
		{"POST /api/v1/redis/flush", s.flushRedis},
		{"GET /api/v1/databases", s.databases},
		{"POST /api/v1/databases/{engine}", s.createDatabase},
		{"POST /api/v1/databases/{engine}/delete", s.dropDatabases},
		{"POST /api/v1/databases/{engine}/users", s.createDatabaseUser},
		{"POST /api/v1/databases/{engine}/users/delete", s.dropDatabaseUsers},
		{"GET /api/v1/s3", s.s3Info},
		{"GET /api/v1/s3/buckets", s.listBuckets},
		{"POST /api/v1/s3/buckets", s.createBuckets},
		{"POST /api/v1/s3/buckets/delete", s.deleteBuckets},
		{"POST /api/v1/s3/buckets/empty", s.emptyBuckets},
		{"POST /api/v1/s3/buckets/public", s.setBucketsPublic},
		{"GET /api/v1/s3/buckets/{bucket}/objects", s.listObjects},
		{"PUT /api/v1/s3/buckets/{bucket}/object", s.uploadObject},
		{"GET /api/v1/s3/buckets/{bucket}/object", s.downloadObject},
		{"GET /api/v1/s3/buckets/{bucket}/object/link", s.objectLink},
		{"POST /api/v1/s3/buckets/{bucket}/objects/delete", s.deleteObjects},
		{"POST /api/v1/s3/buckets/{bucket}/objects/public", s.setObjectsPublic},
		{"GET /api/v1/s3/keys", s.listKeys},
		{"POST /api/v1/s3/keys", s.createKey},
		{"GET /api/v1/s3/keys/{id}", s.showKey},
		{"POST /api/v1/s3/keys/delete", s.deleteKeys},
	}
}

// routes returns the API.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	for _, r := range s.routeTable() {
		h := r.handler
		if needs, ok := destructive[r.pattern]; ok {
			h = func(w http.ResponseWriter, req *http.Request) {
				if needs(req) && !s.confirmed(w, req) {
					return
				}
				r.handler(w, req)
			}
		}
		mux.HandleFunc(r.pattern, h)
	}
	return mux
}

func always(*http.Request) bool { return true }

// destructive are the routes that lose data or cut the service: requested
// remotely, they need the password again (see confirmed).
var destructive = map[string]func(*http.Request) bool{
	"POST /api/daemon/stop":                           always,
	"POST /api/v1/stack/stop":                         always,
	"POST /api/v1/hosts/remove":                       always,
	"POST /api/v1/hosts/{name}/caddyfile/reset":       always,
	"POST /api/v1/dns/stop":                           always,
	"POST /api/v1/containers/{name}/{action}":         func(r *http.Request) bool { return r.PathValue("action") == "stop" },
	"POST /api/v1/s3/buckets/delete":                  always,
	"POST /api/v1/s3/buckets/empty":                   always,
	"POST /api/v1/s3/keys/delete":                     always,
	"POST /api/v1/s3/buckets/{bucket}/objects/delete": always,
	"POST /api/v1/redis/keys/delete":                  always,
	"POST /api/v1/redis/flush":                        always,
	"POST /api/v1/databases/{engine}/delete":          always,
	"POST /api/v1/databases/{engine}/users/delete":    always,
	// A pairing code opens a session without the password
	"POST /api/v1/auth/pairing": always,
	// The access settings could open the dashboard wider
	"PUT /api/v1/settings/{key}": func(r *http.Request) bool {
		return strings.HasPrefix(strings.ToUpper(r.PathValue("key")), "DASHBOARD_")
	},
}

// read runs fn on a workspace that reports nothing, for the requests that
// only read.
func (s *Server) read(w http.ResponseWriter, fn func(ws *Workspace) (any, error)) {
	ws, err := s.Backend(events.NewLog(nil))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = ws.Close() }()
	v, err := fn(ws)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// submit queues an operation working on a workspace opened for it, and
// answers 202 with the operation.
//
// With ?verbose=true, the output of the image builds streams as events.
func (s *Server) submit(w http.ResponseWriter, r *http.Request, kind string,
	fn func(ctx context.Context, ws *Workspace) (any, error)) {
	verbose := r.URL.Query().Get("verbose") == "true"
	op := s.Queue.Submit(kind, func(ctx context.Context, log events.Log) (any, error) {
		ws, err := s.Backend(log)
		if err != nil {
			return nil, err
		}
		defer func() { _ = ws.Close() }()
		if verbose {
			ws.Stack.Docker.BuildOutput = log.Writer()
		}
		return fn(ctx, ws)
	})
	writeJSON(w, http.StatusAccepted, op)
}

// running returns the hosts service once checked that the stack runs,
// notRunning otherwise.
func running(ctx context.Context, ws *Workspace, notRunning error) (*domains.Service, error) {
	ok, err := ws.Running(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, notRunning
	}
	return ws.Domains(), nil
}

// --- Read --------------------------------------------------------------------

// APIVersion is the major version of the API, the prefix of its routes.
const APIVersion = "v1"

// healthResponse tells the version of the daemon and what it can do, so
// that a client (the mobile app) adapts to daemons of other versions. It
// answers without a session on the remote access: nothing about the
// settings or the stack.
type healthResponse struct {
	Version string `json:"version"`
	// API is the major version of the API (APIVersion)
	API string `json:"api"`
	// Features lists the capabilities of this daemon (see features)
	Features []string `json:"features"`
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Version: s.Version, API: APIVersion, Features: s.features()})
}

// features are the capabilities a client may test in GET /api/health:
// "dashboard" (this build serves the pages), "update" (it looks up the
// releases, GET /api/v1/update), "docker" (it drives the stack and its
// containers) and "pairing" (POST /api/v1/auth/pairing and
// POST /api/v1/auth/pair).
func (s *Server) features() []string {
	f := []string{}
	if s.UI != nil {
		f = append(f, "dashboard")
	}
	if s.Updater != nil {
		f = append(f, "update")
	}
	return append(f, "docker", "pairing")
}

// stopDaemon stops the daemon once the response is sent: the CLI replaces
// a daemon of another version.
func (s *Server) stopDaemon(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusAccepted, struct{}{})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go s.Stop()
}

// Host is a registered host.
type Host struct {
	Name string `json:"name"`
	// Kind is php, proxy, static or alias
	Kind   string `json:"kind"`
	PHP    string `json:"php,omitempty"`
	Target string `json:"target,omitempty"` // upstream of a proxy, target of an alias
	Root   string `json:"root,omitempty"`   // static host
	SPA    bool   `json:"spa,omitempty"`
	// Websocket tells whether a proxy host forwards WebSocket connections
	Websocket *bool `json:"websocket,omitempty"`
}

// Service is the state of an optional service.
type Service struct {
	Name    string `json:"name"`
	Setting string `json:"setting"`
	Enabled bool   `json:"enabled"`
	State   string `json:"state"`
}

type statusResponse struct {
	Running bool `json:"running"`
	// Docker tells whether the Docker engine answers: when it does not, the
	// stack is reported stopped and its containers "not found"
	Docker   bool      `json:"docker"`
	Hosts    []Host    `json:"hosts"`
	Services []Service `json:"services"`
}

func hostsOf(st *store.Store) ([]Host, error) {
	hosts := []Host{}
	domainList, err := st.Domains()
	if err != nil {
		return nil, err
	}
	for _, d := range domainList {
		hosts = append(hosts, Host{Name: d.Name, Kind: "php", PHP: d.PHPVersion})
	}
	proxies, err := st.Proxies()
	if err != nil {
		return nil, err
	}
	for _, p := range proxies {
		hosts = append(hosts, Host{Name: p.Domain, Kind: "proxy", Target: p.Target, Websocket: new(!p.NoWebsocket)})
	}
	statics, err := st.StaticHosts()
	if err != nil {
		return nil, err
	}
	for _, h := range statics {
		hosts = append(hosts, Host{Name: h.Domain, Kind: "static", Root: h.Root, SPA: h.SPA})
	}
	aliases, err := st.Aliases()
	if err != nil {
		return nil, err
	}
	for _, a := range aliases {
		hosts = append(hosts, Host{Name: a.Alias, Kind: "alias", Target: a.Target})
	}
	return hosts, nil
}

func servicesOf(ctx context.Context, ws *Workspace) []Service {
	out := []Service{}
	for _, st := range ws.Stack.OptionalStatuses(ctx, "") {
		out = append(out, Service{Name: st.Name, Setting: st.Key, Enabled: st.Enabled, State: st.State})
	}
	return out
}

// status answers quickly even when Docker is stopped or stuck: the
// dashboard tells "Docker unreachable" from "daemon gone" by it.
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	s.read(w, func(ws *Workspace) (any, error) {
		ctx := r.Context()
		up := ws.Stack.Docker.Reachable(ctx)
		running := false
		if up {
			var err error
			if running, err = ws.Running(ctx); err != nil {
				return nil, err
			}
		}
		hosts, err := hostsOf(ws.Store)
		if err != nil {
			return nil, err
		}
		return statusResponse{Running: running, Docker: up, Hosts: hosts, Services: servicesOf(ctx, ws)}, nil
	})
}

func (s *Server) services(w http.ResponseWriter, r *http.Request) {
	s.read(w, func(ws *Workspace) (any, error) { return servicesOf(r.Context(), ws), nil })
}

func (s *Server) operations(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Queue.List())
}

func (s *Server) operation(w http.ResponseWriter, r *http.Request) {
	op, ok := s.Queue.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, i18n.T("Operation not found."))
		return
	}
	writeJSON(w, http.StatusOK, op)
}

// Setting is a setting as the API shows it.
type Setting struct {
	Key         string   `json:"key"`
	Value       string   `json:"value"`
	Default     string   `json:"default"`
	Description string   `json:"description"`
	Values      []string `json:"values,omitempty"`
	Custom      bool     `json:"custom"`
	Secret      bool     `json:"secret"`
	// Apply tells how a change applies (see applyNames), Applies is its
	// label in the language of the daemon, Hint the command that applies it
	Apply   string `json:"apply"`
	Applies string `json:"applies"`
	Hint    string `json:"hint,omitempty"`
}

// applyNames are the stable names of how a setting change applies.
var applyNames = map[env.Apply]string{
	env.ApplyNow: "now", env.ApplyLive: "live", env.ApplyAlias: "alias", env.ApplyRecreate: "recreate",
	env.ApplyRebuild: "rebuild", env.ApplyRestartDNS: "restart_dns", env.ApplyRestartDB: "restart_db",
	env.ApplyDaemon: "daemon",
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	showSecrets := r.URL.Query().Get("show_secrets") == "true"
	s.read(w, func(ws *Workspace) (any, error) {
		out := []Setting{}
		for _, e := range settings.List(ws.Env, showSecrets) {
			out = append(out, Setting{Key: e.Key, Value: e.Value, Default: e.Default, Description: e.Description,
				Values: e.Values, Custom: e.Custom, Secret: e.Secret, Apply: applyNames[e.Apply], Applies: e.Apply.Label(),
				Hint: e.Apply.Hint()})
		}
		return out, nil
	})
}

// --- Operations --------------------------------------------------------------

type startRequest struct {
	Domains  []string `json:"domains"`
	Path     string   `json:"path"`
	PHP      string   `json:"php"`
	ForceSSL bool     `json:"force_ssl"`
	Build    bool     `json:"build"`
}

func (s *Server) startStack(w http.ResponseWriter, r *http.Request) {
	var req startRequest
	if !readJSON(w, r, &req) {
		return
	}
	s.submit(w, r, "stack.start", func(ctx context.Context, ws *Workspace) (any, error) {
		return ws.Stack.Start(ctx, stack.StartOptions{Domains: req.Domains, Path: req.Path, PHP: req.PHP,
			ForceSSL: req.ForceSSL, Build: req.Build})
	})
}

func (s *Server) stopStack(w http.ResponseWriter, r *http.Request) {
	s.submit(w, r, "stack.stop", func(ctx context.Context, ws *Workspace) (any, error) {
		return nil, ws.Stack.Stop(ctx)
	})
}

type restartRequest struct {
	Targets  []string `json:"targets"`
	ForceSSL bool     `json:"force_ssl"`
	Build    bool     `json:"build"`
}

func (s *Server) restartStack(w http.ResponseWriter, r *http.Request) {
	var req restartRequest
	if !readJSON(w, r, &req) {
		return
	}
	for _, t := range req.Targets {
		if !slices.Contains(stack.RestartTargets, t) {
			writeError(w, http.StatusBadRequest, i18n.Tf("Unknown target '%s'. Use %s.", t, strings.Join(stack.RestartTargets, ", ")))
			return
		}
	}
	// Processed in the order of RestartTargets, like the CLI options
	var targets []string
	for _, t := range stack.RestartTargets {
		if slices.Contains(req.Targets, t) {
			targets = append(targets, t)
		}
	}
	s.submit(w, r, "stack.restart", func(ctx context.Context, ws *Workspace) (any, error) {
		return nil, ws.Stack.Restart(ctx, targets, req.ForceSSL, req.Build)
	})
}

type addHostsRequest struct {
	Domains []string `json:"domains"`
	// PHP is the version of PHP domains, "" for DEFAULT_PHP_VERSION
	PHP string `json:"php"`
	// Root is the directory served, relative to the projects root
	Root string `json:"root"`
	// Target makes proxy hosts forwarding to it
	Target string `json:"target"`
	// Websocket allows the WebSocket connections of proxy hosts (default)
	Websocket *bool `json:"websocket"`
	Static    bool  `json:"static"`
	SPA       bool  `json:"spa"`
	ForceSSL  bool  `json:"force_ssl"`
}

func (s *Server) addHosts(w http.ResponseWriter, r *http.Request) {
	var req addHostsRequest
	if !readJSON(w, r, &req) {
		return
	}
	switch {
	case len(req.Domains) == 0:
		writeError(w, http.StatusBadRequest, i18n.Tf("Missing field '%s'.", "domains"))
		return
	case req.Target != "" && (req.Static || req.Root != ""):
		writeError(w, http.StatusBadRequest, i18n.T("A proxy host has no root and is not static."))
		return
	case req.Websocket != nil && req.Target == "":
		writeError(w, http.StatusBadRequest, i18n.Tf("Option '%s' requires '%s'.", "websocket", "target"))
		return
	case req.SPA && !req.Static:
		writeError(w, http.StatusBadRequest, i18n.Tf("Option '%s' requires '%s'.", "spa", "static"))
		return
	}
	s.submit(w, r, "hosts.add", func(ctx context.Context, ws *Workspace) (any, error) {
		svc, err := running(ctx, ws, errNotRunningStart)
		if err != nil {
			return nil, err
		}
		switch {
		case req.Target != "":
			return svc.AddProxyWebsocket(ctx, req.Domains, req.Target, req.Websocket == nil || *req.Websocket, req.ForceSSL)
		case req.Static:
			return svc.AddStatic(ctx, req.Domains, req.Root, req.SPA, req.ForceSSL)
		}
		return svc.AddPHP(ctx, req.Domains, req.PHP, req.Root, req.ForceSSL)
	})
}

// removeHosts removes hosts: domains, aliases, proxy and static hosts.
func (s *Server) removeHosts(w http.ResponseWriter, r *http.Request) {
	var req namesRequest
	if !readJSON(w, r, &req) || !req.check(w, "domains") {
		return
	}
	s.submit(w, r, "hosts.remove", func(ctx context.Context, ws *Workspace) (any, error) {
		svc, err := running(ctx, ws, errNotRunning)
		if err != nil {
			return nil, err
		}
		return svc.Remove(ctx, req.Domains)
	})
}

// namesRequest is a request on several hosts.
type namesRequest struct {
	Domains []string `json:"domains"`
}

// check answers 400 when no name is given.
func (req namesRequest) check(w http.ResponseWriter, field string) bool {
	if len(req.Domains) == 0 {
		writeError(w, http.StatusBadRequest, i18n.Tf("Missing field '%s'.", field))
		return false
	}
	return true
}

type aliasesRequest struct {
	Aliases  []string `json:"aliases"`
	ForceSSL bool     `json:"force_ssl"`
}

func (s *Server) addAliases(w http.ResponseWriter, r *http.Request) {
	var req aliasesRequest
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Aliases) == 0 {
		writeError(w, http.StatusBadRequest, i18n.Tf("Missing field '%s'.", "aliases"))
		return
	}
	target := r.PathValue("name")
	s.submit(w, r, "hosts.alias", func(ctx context.Context, ws *Workspace) (any, error) {
		svc, err := running(ctx, ws, errNotRunningStart)
		if err != nil {
			return nil, err
		}
		return svc.AddAlias(ctx, target, req.Aliases, req.ForceSSL)
	})
}

type phpRequest struct {
	PHP string `json:"php"`
}

func (s *Server) switchPHP(w http.ResponseWriter, r *http.Request) {
	var req phpRequest
	if !readJSON(w, r, &req) {
		return
	}
	domain := r.PathValue("name")
	s.submit(w, r, "hosts.php", func(ctx context.Context, ws *Workspace) (any, error) {
		php, err := stack.ResolvePHP(req.PHP, ws.Env, ws.Stack.Log)
		if err != nil {
			return nil, err
		}
		svc, err := running(ctx, ws, errNotRunning)
		if err != nil {
			return nil, err
		}
		return svc.SwitchPHP(ctx, domain, php)
	})
}

type restoreRequest struct {
	Domains []string `json:"domains"`
	// PHP restores PHP domains with that version; Static restores static
	// hosts. One of them is required
	PHP      string `json:"php"`
	Static   bool   `json:"static"`
	ForceSSL bool   `json:"force_ssl"`
}

func (s *Server) restoreHosts(w http.ResponseWriter, r *http.Request) {
	var req restoreRequest
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Domains) == 0 {
		writeError(w, http.StatusBadRequest, i18n.Tf("Missing field '%s'.", "domains"))
		return
	}
	if req.Static == (req.PHP != "") {
		writeError(w, http.StatusBadRequest, errRestoreKind.Error())
		return
	}
	s.submit(w, r, "hosts.restore", func(ctx context.Context, ws *Workspace) (any, error) {
		if req.Static {
			svc, err := running(ctx, ws, errNotRunningStart)
			if err != nil {
				return nil, err
			}
			return svc.RestoreStatic(ctx, req.Domains, req.ForceSSL)
		}
		php, err := stack.ResolvePHP(req.PHP, ws.Env, ws.Stack.Log)
		if err != nil {
			return nil, err
		}
		svc, err := running(ctx, ws, errNotRunningStart)
		if err != nil {
			return nil, err
		}
		return svc.Restore(ctx, req.Domains, php, req.ForceSSL)
	})
}

type serviceRequest struct {
	Enabled bool `json:"enabled"`
}

func (s *Server) setService(w http.ResponseWriter, r *http.Request) {
	var req serviceRequest
	if !readJSON(w, r, &req) {
		return
	}
	name := strings.ToLower(r.PathValue("name"))
	if !slices.Contains(stack.OptionalServices, name) {
		writeError(w, http.StatusNotFound, i18n.Tf("Unknown service '%s'. Use %s.", name, strings.Join(stack.OptionalServices, ", ")))
		return
	}
	s.submit(w, r, "services.set", func(ctx context.Context, ws *Workspace) (any, error) {
		return ws.Stack.SetOptional(ctx, name, req.Enabled)
	})
}

type settingRequest struct {
	// Value is the new value, null to reset the default
	Value *string `json:"value"`
}

// settingResult is what a setting change did.
type settingResult struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Changed bool   `json:"changed"`
	// Service is what happened to the optional service of the setting,
	// applied at once; otherwise Hint tells how to apply the change
	Service *stack.OptionalResult `json:"service,omitempty"`
	Hint    string                `json:"hint,omitempty"`
}

func (s *Server) setSetting(w http.ResponseWriter, r *http.Request) {
	var req settingRequest
	if !readJSON(w, r, &req) {
		return
	}
	d, err := settings.Check(r.PathValue("key"), req.Value)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.submit(w, r, "settings.set", func(ctx context.Context, ws *Workspace) (any, error) {
		c, err := settings.Set(ws.Env, d, req.Value)
		if err != nil {
			return nil, err
		}
		res := settingResult{Key: d.Key, Value: c.Shown, Changed: c.Changed(), Hint: d.Apply.Hint()}
		if !c.Changed() {
			ws.Stack.Log.Info(i18n.Tf("%s = %s (unchanged)", d.Key, c.Shown))
			return res, nil
		}
		ws.Stack.Log.Success(d.Key + " = " + c.Shown)
		if d.Apply == env.ApplyDaemon {
			if d.Key == env.DashboardPasswordKey {
				// A new password signs every device out
				if err := ws.Store.RemoveSessions(time.Time{}); err != nil {
					return nil, err
				}
			}
			s.ReloadAccess()
			res.Hint = ""
		}
		if d.Apply == env.ApplyLive {
			o, err := ws.Stack.ApplyOptional(ctx, d.Service)
			if err != nil {
				return nil, err
			}
			res.Service, res.Hint = &o, ""
		}
		return res, nil
	})
}

// setWebsocket allows or refuses the WebSocket connections of a proxy host.
func (s *Server) setWebsocket(w http.ResponseWriter, r *http.Request) {
	host := r.PathValue("name")
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	s.submit(w, r, "hosts.websocket", func(ctx context.Context, ws *Workspace) (any, error) {
		svc, err := running(ctx, ws, errNotRunning)
		if err != nil {
			return nil, err
		}
		return map[string]bool{"websocket": req.Enabled}, svc.SetWebsocket(ctx, host, req.Enabled)
	})
}

package daemon

import (
	"context"
	"net/http"
	"slices"
	"strings"

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
	Close   func() error
}

// Running reports whether the stack runs.
func (ws *Workspace) Running(ctx context.Context) (bool, error) {
	return stack.IsRunning(ctx, ws.Stack.Docker, ws.Store)
}

// Backend opens a workspace reporting to log. The CLI provides it, with
// the same dependencies as its commands.
type Backend func(log events.Log) (*Workspace, error)

// errNotRunning is the error of the actions that need the stack running.
var errNotRunning = i18n.NewError("The server is not running.")

// routes returns the API.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("GET /api/operations", s.operations)
	mux.HandleFunc("GET /api/operations/{id}", s.operation)

	mux.HandleFunc("POST /api/stack/start", s.startStack)
	mux.HandleFunc("POST /api/stack/stop", s.stopStack)
	mux.HandleFunc("POST /api/stack/restart", s.restartStack)

	mux.HandleFunc("POST /api/hosts", s.addHosts)
	mux.HandleFunc("DELETE /api/hosts/{name}", s.removeHost)
	mux.HandleFunc("POST /api/hosts/{name}/aliases", s.addAliases)
	mux.HandleFunc("PUT /api/hosts/{name}/php", s.switchPHP)
	mux.HandleFunc("POST /api/hosts/restore", s.restoreHosts)

	mux.HandleFunc("GET /api/services", s.services)
	mux.HandleFunc("PUT /api/services/{name}", s.setService)

	mux.HandleFunc("GET /api/settings", s.settings)
	mux.HandleFunc("PUT /api/settings/{key}", s.setSetting)
	return mux
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
func (s *Server) submit(w http.ResponseWriter, kind string, fn func(ctx context.Context, ws *Workspace) (any, error)) {
	op := s.Queue.Submit(kind, func(ctx context.Context, log events.Log) (any, error) {
		ws, err := s.Backend(log)
		if err != nil {
			return nil, err
		}
		defer func() { _ = ws.Close() }()
		return fn(ctx, ws)
	})
	writeJSON(w, http.StatusAccepted, op)
}

// running returns the hosts service once checked that the stack runs.
func running(ctx context.Context, ws *Workspace) (*domains.Service, error) {
	ok, err := ws.Running(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errNotRunning
	}
	return ws.Domains(), nil
}

// --- Read --------------------------------------------------------------------

type healthResponse struct {
	Version string `json:"version"`
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Version: s.Version})
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
}

// Service is the state of an optional service.
type Service struct {
	Name    string `json:"name"`
	Setting string `json:"setting"`
	Enabled bool   `json:"enabled"`
	State   string `json:"state"`
}

type statusResponse struct {
	Running  bool      `json:"running"`
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
		hosts = append(hosts, Host{Name: p.Domain, Kind: "proxy", Target: p.Target})
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

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	s.read(w, func(ws *Workspace) (any, error) {
		ok, err := ws.Running(r.Context())
		if err != nil {
			return nil, err
		}
		hosts, err := hostsOf(ws.Store)
		if err != nil {
			return nil, err
		}
		return statusResponse{Running: ok, Hosts: hosts, Services: servicesOf(r.Context(), ws)}, nil
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
	// Applies tells how a change applies, Hint the command that applies it
	Applies string `json:"applies"`
	Hint    string `json:"hint,omitempty"`
}

func (s *Server) settings(w http.ResponseWriter, _ *http.Request) {
	s.read(w, func(ws *Workspace) (any, error) {
		out := []Setting{}
		for _, e := range settings.List(ws.Env, false) {
			out = append(out, Setting{Key: e.Key, Value: e.Value, Default: e.Default, Description: e.Description,
				Values: e.Values, Custom: e.Custom, Secret: e.Secret, Applies: e.Apply.Label(), Hint: e.Apply.Hint()})
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
	s.submit(w, "stack.start", func(ctx context.Context, ws *Workspace) (any, error) {
		return ws.Stack.Start(ctx, stack.StartOptions{Domains: req.Domains, Path: req.Path, PHP: req.PHP,
			ForceSSL: req.ForceSSL, Build: req.Build})
	})
}

func (s *Server) stopStack(w http.ResponseWriter, _ *http.Request) {
	s.submit(w, "stack.stop", func(ctx context.Context, ws *Workspace) (any, error) {
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
	s.submit(w, "stack.restart", func(ctx context.Context, ws *Workspace) (any, error) {
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
	Target   string `json:"target"`
	Static   bool   `json:"static"`
	SPA      bool   `json:"spa"`
	ForceSSL bool   `json:"force_ssl"`
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
	case req.SPA && !req.Static:
		writeError(w, http.StatusBadRequest, i18n.Tf("Option '%s' requires '%s'.", "spa", "static"))
		return
	}
	s.submit(w, "hosts.add", func(ctx context.Context, ws *Workspace) (any, error) {
		svc, err := running(ctx, ws)
		if err != nil {
			return nil, err
		}
		switch {
		case req.Target != "":
			return svc.AddProxy(ctx, req.Domains, req.Target, req.ForceSSL)
		case req.Static:
			return svc.AddStatic(ctx, req.Domains, req.Root, req.SPA, req.ForceSSL)
		}
		return svc.AddPHP(ctx, req.Domains, req.PHP, req.Root, req.ForceSSL)
	})
}

func (s *Server) removeHost(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.submit(w, "hosts.remove", func(ctx context.Context, ws *Workspace) (any, error) {
		svc, err := running(ctx, ws)
		if err != nil {
			return nil, err
		}
		return svc.Remove(ctx, []string{name})
	})
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
	s.submit(w, "hosts.alias", func(ctx context.Context, ws *Workspace) (any, error) {
		svc, err := running(ctx, ws)
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
	s.submit(w, "hosts.php", func(ctx context.Context, ws *Workspace) (any, error) {
		php, err := stack.ResolvePHP(req.PHP, ws.Env, ws.Stack.Log)
		if err != nil {
			return nil, err
		}
		svc, err := running(ctx, ws)
		if err != nil {
			return nil, err
		}
		return svc.SwitchPHP(ctx, domain, php)
	})
}

type restoreRequest struct {
	Domains  []string `json:"domains"`
	PHP      string   `json:"php"`
	ForceSSL bool     `json:"force_ssl"`
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
	s.submit(w, "hosts.restore", func(ctx context.Context, ws *Workspace) (any, error) {
		php, err := stack.ResolvePHP(req.PHP, ws.Env, ws.Stack.Log)
		if err != nil {
			return nil, err
		}
		svc, err := running(ctx, ws)
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
	s.submit(w, "services.set", func(ctx context.Context, ws *Workspace) (any, error) {
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
	// Applied reports a change already applied (optional services);
	// otherwise Hint tells how to apply it
	Applied bool   `json:"applied"`
	Hint    string `json:"hint,omitempty"`
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
	s.submit(w, "settings.set", func(ctx context.Context, ws *Workspace) (any, error) {
		c, err := settings.Set(ws.Env, d, req.Value)
		if err != nil {
			return nil, err
		}
		res := settingResult{Key: d.Key, Value: c.Shown, Changed: c.Changed(), Hint: d.Apply.Hint()}
		if c.Changed() && d.Apply == env.ApplyLive {
			if _, err := ws.Stack.ApplyOptional(ctx, d.Service); err != nil {
				return nil, err
			}
			res.Applied, res.Hint = true, ""
		}
		return res, nil
	})
}

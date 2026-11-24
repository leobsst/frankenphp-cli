package daemon

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/compose"
	"github.com/leobsst/frankenphp-cli/internal/core/databases"
	"github.com/leobsst/frankenphp-cli/internal/core/events"
	"github.com/leobsst/frankenphp-cli/internal/core/redis"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// --- Redis -------------------------------------------------------------------

// errRedisOff is the error of the Redis routes when it is disabled.
var errRedisOff = i18n.NewError("Redis is disabled: run 'frankenmanager service enable redis' to use it.")

// redisClient connects to the Redis cache of the stack.
func redisClient(ctx context.Context, ws *Workspace) (*redis.Client, error) {
	if !ws.Env.RedisEnabled() {
		return nil, errRedisOff
	}
	port := ws.Env.Get("REDIS_PORT")
	if port == "" {
		port = "6379"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c, err := redis.Dial(ctx, net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return nil, i18n.Errorf("Redis does not answer: %v", err)
	}
	return c, nil
}

// RedisState is the state of the Redis cache.
type RedisState struct {
	Enabled bool        `json:"enabled"`
	Running bool        `json:"running"`
	Info    *redis.Info `json:"info,omitempty"`
}

func (s *Server) redisState(w http.ResponseWriter, r *http.Request) {
	s.read(w, func(ws *Workspace) (any, error) {
		st := RedisState{Enabled: ws.Env.RedisEnabled()}
		if !st.Enabled {
			return st, nil
		}
		st.Running = ws.Stack.Docker.ContainerStatuses(r.Context()).Running(compose.RedisContainer)
		if !st.Running {
			return st, nil
		}
		c, err := redisClient(r.Context(), ws)
		if err != nil {
			return st, nil
		}
		defer func() { _ = c.Close() }()
		info, err := c.Info()
		if err != nil {
			return nil, err
		}
		st.Info = &info
		return st, nil
	})
}

// redisKeys returns a page of keys (?pattern=, ?cursor=, ?count=).
func (s *Server) redisKeys(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	count := 100
	if v := q.Get("count"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 1000 {
			writeError(w, http.StatusBadRequest, i18n.Tf("Invalid value '%s' for %s.", v, "count"))
			return
		}
		count = n
	}
	cursor := q.Get("cursor")
	if cursor == "" {
		cursor = "0"
	}
	s.read(w, func(ws *Workspace) (any, error) {
		c, err := redisClient(r.Context(), ws)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		return c.Scan(cursor, q.Get("pattern"), count)
	})
}

// deleteRedisKeys deletes keys.
func (s *Server) deleteRedisKeys(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Keys []string `json:"keys"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Keys) == 0 {
		writeError(w, http.StatusBadRequest, i18n.Tf("Missing field '%s'.", "keys"))
		return
	}
	s.submit(w, r, "redis.keys.delete", func(ctx context.Context, ws *Workspace) (any, error) {
		c, err := redisClient(ctx, ws)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		n, err := c.Delete(req.Keys)
		if err == nil {
			ws.Stack.Log.Success(i18n.Tf("Deleted %d key(s).", n))
		}
		return map[string]int64{"deleted": n}, err
	})
}

// flushRedis deletes every key.
func (s *Server) flushRedis(w http.ResponseWriter, r *http.Request) {
	s.submit(w, r, "redis.flush", func(ctx context.Context, ws *Workspace) (any, error) {
		c, err := redisClient(ctx, ws)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		if err := c.Flush(); err != nil {
			return nil, err
		}
		ws.Stack.Log.Success(i18n.T("Redis cache flushed."))
		return nil, nil
	})
}

// --- Databases ---------------------------------------------------------------

func databaseService(ws *Workspace, log events.Log) *databases.Service {
	return &databases.Service{Env: ws.Env, Engines: ws.Env.DBEngines(), Docker: ws.Stack.Docker, Log: log}
}

// DatabasesState is the database servers and where their admin interface
// answers.
type DatabasesState struct {
	Servers []databases.Server `json:"servers"`
	// AdminURL is phpMyAdmin or pgAdmin (development) as the requester
	// reaches it, "" without or when out of its reach (see dbAdminURL)
	AdminURL string `json:"admin_url,omitempty"`
}

func (s *Server) databases(w http.ResponseWriter, r *http.Request) {
	s.read(w, func(ws *Workspace) (any, error) {
		servers, err := databaseService(ws, events.NewLog(nil)).List(r.Context())
		if err != nil {
			return nil, err
		}
		host := (&url.URL{Host: r.Host}).Hostname()
		if host == "" && s.LANIP != nil {
			host = s.LANIP()
		}
		return DatabasesState{
			Servers:  servers,
			AdminURL: dbAdminURL(ws.Env.DBAdminPort(), ws.Env.ServicesExposed(), requesterOf(r).remote, host),
		}, nil
	})
}

// dbAdminURL is the address of the database admin UI listening on port
// (see env.DBAdminPort, "" for none) for a requester of this machine or,
// when remote, of another device that reached it at host.
//
// On this machine, the reverse proxy serves it at https://localhost. That
// name is the device itself anywhere else, and phpMyAdmin and pgAdmin have
// no name of their own behind the reverse proxy (nor in the LAN DNS
// server): another device reaches them on their port only, at the address
// it reaches this machine at, and only when EXPOSE_SERVICES publishes that
// port beyond 127.0.0.1. Otherwise there is no address to give: "".
func dbAdminURL(port string, exposed, remote bool, host string) string {
	switch {
	case port == "":
		return ""
	case !remote:
		return "https://localhost"
	case !exposed || host == "":
		return ""
	}
	return "http://" + net.JoinHostPort(host, port)
}

func (s *Server) createDatabase(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := databases.ValidateName(req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	engine := r.PathValue("engine")
	s.submit(w, r, "databases.create", func(ctx context.Context, ws *Workspace) (any, error) {
		return nil, databaseService(ws, ws.Stack.Log).CreateDatabase(ctx, engine, req.Name)
	})
}

// namesBody is a list of names to work on.
type namesBody struct {
	Names []string `json:"names"`
}

func readNames(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	var req namesBody
	if !readJSON(w, r, &req) {
		return nil, false
	}
	if len(req.Names) == 0 {
		writeError(w, http.StatusBadRequest, i18n.Tf("Missing field '%s'.", "names"))
		return nil, false
	}
	return req.Names, true
}

func (s *Server) dropDatabases(w http.ResponseWriter, r *http.Request) {
	names, ok := readNames(w, r)
	if !ok {
		return
	}
	engine := r.PathValue("engine")
	s.submit(w, r, "databases.delete", func(ctx context.Context, ws *Workspace) (any, error) {
		return nil, databaseService(ws, ws.Stack.Log).DropDatabases(ctx, engine, names)
	})
}

func (s *Server) createDatabaseUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Password string `json:"password"`
		Database string `json:"database"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := databases.ValidateName(req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	engine := r.PathValue("engine")
	s.submit(w, r, "databases.users.create", func(ctx context.Context, ws *Workspace) (any, error) {
		return nil, databaseService(ws, ws.Stack.Log).CreateUser(ctx, engine, req.Name, req.Password, req.Database)
	})
}

func (s *Server) dropDatabaseUsers(w http.ResponseWriter, r *http.Request) {
	names, ok := readNames(w, r)
	if !ok {
		return
	}
	engine := r.PathValue("engine")
	s.submit(w, r, "databases.users.delete", func(ctx context.Context, ws *Workspace) (any, error) {
		return nil, databaseService(ws, ws.Stack.Log).DropUsers(ctx, engine, names)
	})
}

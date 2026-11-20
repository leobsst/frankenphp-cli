// Package databases creates and deletes the databases and users of the
// database servers of the stack (MariaDB, MySQL, PostgreSQL), through their
// own client in the container. The passwords and the queries go through
// environment variables then the client's standard input: they never show
// on a command line.
package databases

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/core/compose"
	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/events"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// Config reads the configuration.
type Config interface {
	Get(key string) string
}

// Service works on the database servers of the stack.
type Service struct {
	Env     Config
	Engines []string // the enabled engines
	Docker  *docker.Manager
	Log     events.Log
}

// Server is a database server, its databases and users.
type Server struct {
	Engine    string   `json:"engine"`
	Container string   `json:"container"`
	Running   bool     `json:"running"`
	Databases []string `json:"databases"`
	Users     []User   `json:"users"`
}

// User is an account of a database server.
type User struct {
	Name string `json:"name"`
	Host string `json:"host,omitempty"` // MariaDB and MySQL: '%' or localhost...
}

// identifier is a database or user name: plain, so that it never needs
// quoting tricks.
var identifier = regexp.MustCompile(`^[A-Za-z0-9_]{1,63}$`)

// ValidateName checks a database or user name.
func ValidateName(name string) error {
	if !identifier.MatchString(name) {
		return i18n.Errorf("Invalid name '%s': letters, digits and _ only, 63 at most.", name)
	}
	return nil
}

// The accounts and databases of the servers themselves: never listed, never
// deleted.
var (
	systemDatabases = []string{"information_schema", "mysql", "performance_schema", "sys", "postgres", "template0", "template1"}
	systemUsers     = []string{"root", "mariadb.sys", "healthcheck", "mysql.sys", "mysql.session", "mysql.infoschema", "postgres"}
)

func (s *Service) container(engine string) (string, error) {
	if !slices.Contains(s.Engines, engine) {
		return "", i18n.Errorf("The %s server is not enabled (DB_ENGINES).", engine)
	}
	return compose.DBContainers[engine], nil
}

// query runs sql on the server of engine and returns its rows (tab
// separated columns).
func (s *Service) query(ctx context.Context, engine, sql string) ([][]string, error) {
	container, err := s.container(engine)
	if err != nil {
		return nil, err
	}
	env := map[string]string{"FM_SQL": sql}
	var script string
	switch engine {
	case "postgresql":
		user := s.Env.Get("POSTGRES_USER")
		if user == "" {
			user = "postgres"
		}
		env["FM_USER"] = user
		script = `printf '%s' "$FM_SQL" | psql -v ON_ERROR_STOP=1 -U "$FM_USER" -d postgres -At -F "$(printf '\t')"`
	case "mysql":
		env["MYSQL_PWD"] = s.Env.Get("MYSQL_ROOT_PASSWORD")
		script = `printf '%s' "$FM_SQL" | mysql -uroot -N -B`
	default:
		env["MYSQL_PWD"] = s.Env.Get("MARIADB_ROOT_PASSWORD")
		script = `printf '%s' "$FM_SQL" | mariadb -uroot -N -B`
	}
	res, err := s.Docker.ExecEnv(ctx, container, env, "sh", "-c", script)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(string(res.Stderr))
		if msg == "" {
			msg = strings.TrimSpace(string(res.Stdout))
		}
		return nil, i18n.Errorf("The %s server refused the request: %s", engine, msg)
	}
	var rows [][]string
	for _, line := range strings.Split(strings.TrimRight(string(res.Stdout), "\n"), "\n") {
		if line != "" {
			rows = append(rows, strings.Split(line, "\t"))
		}
	}
	return rows, nil
}

// List returns the enabled servers with their databases and users (those
// of running servers only).
func (s *Service) List(ctx context.Context) ([]Server, error) {
	statuses := s.Docker.ContainerStatuses(ctx)
	out := []Server{}
	for _, engine := range s.Engines {
		srv := Server{Engine: engine, Container: compose.DBContainers[engine], Databases: []string{}, Users: []User{}}
		srv.Running = statuses.Running(srv.Container)
		if srv.Running {
			dbs, users, err := s.inventory(ctx, engine)
			if err != nil {
				return nil, err
			}
			srv.Databases, srv.Users = dbs, users
		}
		out = append(out, srv)
	}
	return out, nil
}

func (s *Service) inventory(ctx context.Context, engine string) ([]string, []User, error) {
	dbSQL, userSQL := "SHOW DATABASES", "SELECT User, Host FROM mysql.user ORDER BY User, Host"
	if engine == "postgresql" {
		dbSQL = "SELECT datname FROM pg_database WHERE NOT datistemplate ORDER BY datname"
		userSQL = "SELECT rolname FROM pg_roles WHERE rolcanlogin AND NOT rolsuper ORDER BY rolname"
	}
	rows, err := s.query(ctx, engine, dbSQL)
	if err != nil {
		return nil, nil, err
	}
	dbs := []string{}
	for _, r := range rows {
		if !slices.Contains(systemDatabases, r[0]) {
			dbs = append(dbs, r[0])
		}
	}
	if rows, err = s.query(ctx, engine, userSQL); err != nil {
		return nil, nil, err
	}
	users := []User{}
	for _, r := range rows {
		u := User{Name: r[0]}
		if len(r) > 1 {
			u.Host = r[1]
		}
		if !slices.Contains(systemUsers, u.Name) && u.Name != s.Env.Get("POSTGRES_USER") {
			users = append(users, u)
		}
	}
	return dbs, users, nil
}

// quote quotes a string literal: ” for a quote, and the backslashes
// doubled for MariaDB and MySQL.
func quote(engine, v string) string {
	if engine != "postgresql" {
		v = strings.ReplaceAll(v, `\`, `\\`)
	}
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

// ident quotes a checked identifier.
func ident(engine, name string) string {
	if engine == "postgresql" {
		return `"` + name + `"`
	}
	return "`" + name + "`"
}

// CreateDatabase creates a database (utf8mb4 on MariaDB and MySQL, UTF8 on
// PostgreSQL).
func (s *Service) CreateDatabase(ctx context.Context, engine, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	sql := "CREATE DATABASE " + ident(engine, name) + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"
	if engine == "postgresql" {
		sql = "CREATE DATABASE " + ident(engine, name) + " ENCODING 'UTF8'"
	}
	if _, err := s.query(ctx, engine, sql); err != nil {
		return err
	}
	s.Log.Success(i18n.Tf("Database '%s' created on %s.", name, engine))
	return nil
}

// DropDatabases deletes databases, the system ones excepted.
func (s *Service) DropDatabases(ctx context.Context, engine string, names []string) error {
	for _, name := range names {
		if err := ValidateName(name); err != nil {
			return err
		}
		if slices.Contains(systemDatabases, name) {
			return i18n.Errorf("'%s' is a system database: it cannot be deleted.", name)
		}
	}
	for _, name := range names {
		sql := "DROP DATABASE " + ident(engine, name)
		if engine == "postgresql" {
			sql += " WITH (FORCE)"
		}
		if _, err := s.query(ctx, engine, sql); err != nil {
			return err
		}
		s.Log.Success(i18n.Tf("Database '%s' deleted from %s.", name, engine))
	}
	return nil
}

// CreateUser creates a user with password, with every privilege on
// database when set (the owner on PostgreSQL). On MariaDB and MySQL the
// user connects from anywhere ('%'): the apps run in other containers.
func (s *Service) CreateUser(ctx context.Context, engine, name, password, database string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if password == "" {
		return i18n.NewError("A password is required.")
	}
	if database != "" {
		if err := ValidateName(database); err != nil {
			return err
		}
	}
	var sql []string
	if engine == "postgresql" {
		sql = append(sql, "CREATE ROLE "+ident(engine, name)+" LOGIN PASSWORD "+quote(engine, password))
		if database != "" {
			sql = append(sql, "ALTER DATABASE "+ident(engine, database)+" OWNER TO "+ident(engine, name),
				"GRANT ALL PRIVILEGES ON DATABASE "+ident(engine, database)+" TO "+ident(engine, name))
		}
	} else {
		account := quote(engine, name) + "@'%'"
		sql = append(sql, "CREATE USER "+account+" IDENTIFIED BY "+quote(engine, password))
		if database != "" {
			sql = append(sql, "GRANT ALL PRIVILEGES ON "+ident(engine, database)+".* TO "+account)
		}
		sql = append(sql, "FLUSH PRIVILEGES")
	}
	if _, err := s.query(ctx, engine, strings.Join(sql, ";\n")+";"); err != nil {
		return err
	}
	s.Log.Success(i18n.Tf("User '%s' created on %s.", name, engine))
	return nil
}

// DropUsers deletes users (every host of a MariaDB or MySQL user), the
// system ones excepted.
func (s *Service) DropUsers(ctx context.Context, engine string, names []string) error {
	for _, name := range names {
		if err := ValidateName(name); err != nil {
			return err
		}
		if slices.Contains(systemUsers, name) || name == s.Env.Get("POSTGRES_USER") {
			return i18n.Errorf("'%s' is a system account: it cannot be deleted.", name)
		}
	}
	for _, name := range names {
		if engine == "postgresql" {
			// Its objects go to the administrator first
			sql := "REASSIGN OWNED BY " + ident(engine, name) + " TO CURRENT_USER;\nDROP OWNED BY " +
				ident(engine, name) + ";\nDROP ROLE " + ident(engine, name) + ";"
			if _, err := s.query(ctx, engine, sql); err != nil {
				return err
			}
		} else {
			rows, err := s.query(ctx, engine, "SELECT Host FROM mysql.user WHERE User = "+quote(engine, name))
			if err != nil {
				return err
			}
			for _, r := range rows {
				if _, err := s.query(ctx, engine, "DROP USER "+quote(engine, name)+"@"+quote(engine, r[0])); err != nil {
					return err
				}
			}
		}
		s.Log.Success(i18n.Tf("User '%s' deleted from %s.", name, engine))
	}
	return nil
}

package databases

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/events"
)

type mapConfig map[string]string

func (m mapConfig) Get(k string) string { return m[k] }

type call struct {
	env  map[string]string
	args []string
}

type fakeDocker struct{ calls []call }

func (f *fakeDocker) Run(_ context.Context, _ string, env map[string]string, args ...string) (docker.Result, error) {
	f.calls = append(f.calls, call{env, args})
	return docker.Result{}, nil
}

// The password and the query go through the environment of docker, never
// its command line (seen by every user in ps).
func TestSecretsStayOffTheCommandLine(t *testing.T) {
	f := &fakeDocker{}
	s := &Service{Env: mapConfig{"MARIADB_ROOT_PASSWORD": "rootpw"}, Engines: []string{"mariadb"},
		Docker: &docker.Manager{Runner: f}, Log: events.NewLog(nil)}
	if err := s.CreateUser(context.Background(), "mariadb", "app", "s3cr'et", "shop"); err != nil {
		t.Fatal(err)
	}
	c := f.calls[0]
	line := strings.Join(c.args, " ")
	if strings.Contains(line, "rootpw") || strings.Contains(line, "s3cr") {
		t.Errorf("secret on the command line: %s", line)
	}
	if !slices.Equal(c.args[:5], []string{"exec", "-e", "FM_SQL", "-e", "MYSQL_PWD"}) || c.env["MYSQL_PWD"] != "rootpw" {
		t.Errorf("args %v, env %v", c.args, c.env)
	}
	want := "CREATE USER 'app'@'%' IDENTIFIED BY 's3cr''et';\nGRANT ALL PRIVILEGES ON `shop`.* TO 'app'@'%';\nFLUSH PRIVILEGES;"
	if c.env["FM_SQL"] != want {
		t.Errorf("sql = %q", c.env["FM_SQL"])
	}
}

func TestRefusals(t *testing.T) {
	f := &fakeDocker{}
	s := &Service{Env: mapConfig{"POSTGRES_USER": "admin"}, Engines: []string{"postgresql"},
		Docker: &docker.Manager{Runner: f}, Log: events.NewLog(nil)}
	ctx := context.Background()
	for _, err := range []error{
		s.CreateDatabase(ctx, "postgresql", "shop; DROP DATABASE x"),
		s.CreateDatabase(ctx, "mariadb", "shop"), // not enabled
		s.DropDatabases(ctx, "postgresql", []string{"template1"}),
		s.DropUsers(ctx, "postgresql", []string{"admin"}),
		s.CreateUser(ctx, "postgresql", "app", "", ""),
	} {
		if err == nil {
			t.Error("accepted")
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("docker called: %v", f.calls)
	}
}

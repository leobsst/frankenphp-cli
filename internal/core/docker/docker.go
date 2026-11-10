// Package docker drives Docker and Compose.
//
// Everything goes through the docker command: it honors the active Docker
// context like the user's shell, and docker compose is needed anyway.
package docker

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/core/compose"
	"github.com/leobsst/frankenphp-cli/internal/core/phpversions"
	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
)

// BuildHashLabel is the image label holding the fingerprint of the inputs
// an image was built from.
const BuildHashLabel = "frankenmanager.build-hash"

// buildContextFiles are the files of the data directory the PHP image
// depends on, in fingerprint order.
var buildContextFiles = []string{"Dockerfile", "docker/entrypoint.sh"}

// Error is a Docker failure shown to the user.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// Result is the outcome of a docker command.
type Result struct {
	Stdout, Stderr []byte
	ExitCode       int // -1 when the command could not run at all
}

// Runner runs the docker command. Tests replace it: they never reach Docker.
type Runner interface {
	// Run runs `docker <args>` in dir (the current directory when empty),
	// with env added to the process environment. err is set only when the
	// command could not run; a non-zero exit is reported in ExitCode.
	Run(ctx context.Context, dir string, env map[string]string, args ...string) (Result, error)
}

// ExecRunner runs the real docker command.
type ExecRunner struct{}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, dir string, env map[string]string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
		err = nil
	default:
		res.ExitCode = -1
	}
	return res, err
}

// Logger receives the progress messages.
type Logger interface {
	Info(msg string)
}

// Manager runs the containers of a data directory.
type Manager struct {
	Dir    string // data directory: compose project and build context
	Runner Runner
	Log    Logger
}

// New returns a Manager using the real docker command.
func New(dataDir string, log Logger) *Manager {
	return &Manager{Dir: dataDir, Runner: ExecRunner{}, Log: log}
}

func (m *Manager) run(ctx context.Context, args ...string) (Result, error) {
	return m.Runner.Run(ctx, m.Dir, nil, args...)
}

// ok runs a command and reports whether it exited with 0.
func (m *Manager) ok(ctx context.Context, args ...string) bool {
	res, err := m.run(ctx, args...)
	return err == nil && res.ExitCode == 0
}

// Containers is what the stack runs, to list its containers.
type Containers struct {
	PHPVersions []string
	DBEngines   []string // nil means ["mariadb"]
	Production  bool
	DNS         bool
	Redis       bool
	Websocket   bool
}

// AllContainers returns the container names of the stack: PHP versions
// (sorted), reverse proxy, Redis, Soketi, databases in DB_ENGINES order,
// then phpMyAdmin and pgAdmin (development only) and the DNS server.
func AllContainers(c Containers) []string {
	engines := c.DBEngines
	if engines == nil {
		engines = []string{"mariadb"}
	}
	var names []string
	for _, v := range slices.Sorted(slices.Values(c.PHPVersions)) {
		names = append(names, phpversions.ContainerName(v))
	}
	names = append(names, compose.ReverseProxyContainer)
	if c.Redis {
		names = append(names, compose.RedisContainer)
	}
	if c.Websocket {
		names = append(names, soketi.ContainerName)
	}
	for _, e := range engines {
		if name, ok := compose.DBContainers[e]; ok {
			names = append(names, name)
		}
	}
	if !c.Production {
		if len(compose.PMAServers(engines)) > 0 {
			names = append(names, compose.PHPMyAdminContainer)
		}
		if slices.Contains(engines, "postgresql") {
			names = append(names, compose.PGAdminContainer)
		}
	}
	if c.DNS {
		names = append(names, compose.DNSContainer)
	}
	return names
}

// BuildHash fingerprints everything a PHP image is built from: the build
// context files and the build arguments. Byte for byte the one of earlier
// releases, so that no image is rebuilt after upgrading.
func (m *Manager) BuildHash(buildArgs map[string]string) (string, error) {
	h := sha256.New()
	for _, rel := range buildContextFiles {
		h.Write([]byte(rel))
		data, err := os.ReadFile(filepath.Join(m.Dir, filepath.FromSlash(rel)))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		h.Write(data)
	}
	h.Write([]byte(buildHashJSON(buildArgs)))
	return hex.EncodeToString(h.Sum(nil)), nil
}

// buildHashJSON serializes m the way earlier releases did for the build hash:
// keys sorted, ", " and ": " separators, every character outside printable
// ASCII escaped as \uXXXX (surrogate pairs beyond the BMP).
func buildHashJSON(m map[string]string) string {
	keys := slices.Sorted(func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	})
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		buildHashJSONString(&b, k)
		b.WriteString(": ")
		buildHashJSONString(&b, m[k])
	}
	b.WriteByte('}')
	return b.String()
}

func buildHashJSONString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			switch {
			case r >= 0x20 && r <= 0x7e:
				b.WriteRune(r)
			case r > 0xffff:
				r -= 0x10000
				fmt.Fprintf(b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
			default:
				fmt.Fprintf(b, `\u%04x`, r)
			}
		}
	}
	b.WriteByte('"')
}

// IsImageCurrent reports whether image exists and was built from the
// inputs fingerprinted by buildHash.
func (m *Manager) IsImageCurrent(ctx context.Context, image, buildHash string) bool {
	res, err := m.run(ctx, "image", "inspect", "--format", "{{json .Config.Labels}}", image)
	if err != nil || res.ExitCode != 0 {
		return false
	}
	var labels map[string]string
	if json.Unmarshal(bytes.TrimSpace(res.Stdout), &labels) != nil {
		return false
	}
	return labels[BuildHashLabel] == buildHash
}

// ImageExists reports whether Docker has image.
func (m *Manager) ImageExists(ctx context.Context, image string) bool {
	return m.ok(ctx, "image", "inspect", "--format", "{{.Id}}", image)
}

// Build is how a PHP image is built.
type Build struct {
	CustomPath            string // projects root, mounted at the same path
	PHPVersion            string
	WWWGroup              string
	ExtraSystemPackages   string // apk or apt packages, space-separated
	ExtraComposerPackages string
	Distro                string // "" means phpversions.DefaultDistro
	InstallPdftk          bool
	Force                 bool // build even if up to date, pulling the base image
}

// Args returns the build arguments: CUSTOM_PATH and PHP_IMAGE_TAG always,
// the others only when set.
func (b Build) Args() (map[string]string, error) {
	distro := b.Distro
	if distro == "" {
		distro = phpversions.DefaultDistro
	}
	tag, err := phpversions.ImageTag(b.PHPVersion, distro)
	if err != nil {
		return nil, err
	}
	args := map[string]string{"CUSTOM_PATH": b.CustomPath, "PHP_IMAGE_TAG": tag}
	if b.WWWGroup != "" {
		args["WWWGROUP"] = b.WWWGroup
	}
	if b.ExtraSystemPackages != "" {
		args["EXTRA_SYSTEM_PACKAGES"] = b.ExtraSystemPackages
	}
	if b.ExtraComposerPackages != "" {
		args["EXTRA_COMPOSER_PACKAGES"] = b.ExtraComposerPackages
	}
	if b.InstallPdftk {
		args["INSTALL_PDFTK"] = "true"
	}
	return args, nil
}

// BuildImage builds the image of a PHP version, unless an up-to-date one
// exists and Force is off. It reports whether it built.
func (m *Manager) BuildImage(ctx context.Context, b Build) (bool, error) {
	args, err := b.Args()
	if err != nil {
		return false, err
	}
	hash, err := m.BuildHash(args)
	if err != nil {
		return false, err
	}
	image := phpversions.ImageName(b.PHPVersion)
	if !b.Force && m.IsImageCurrent(ctx, image, hash) {
		m.Log.Info(fmt.Sprintf("PHP %s image is up to date, skipping build", b.PHPVersion))
		return false, nil
	}

	m.Log.Info(fmt.Sprintf("Building Docker image for PHP %s...", b.PHPVersion))
	cmd := []string{"build", "--quiet", "--pull", "--rm", "--tag", image, "--label", BuildHashLabel + "=" + hash}
	for _, k := range slices.Sorted(func(yield func(string) bool) {
		for k := range args {
			if !yield(k) {
				return
			}
		}
	}) {
		cmd = append(cmd, "--build-arg", k+"="+args[k])
	}
	cmd = append(cmd, m.Dir)
	res, err := m.run(ctx, cmd...)
	if err == nil && res.ExitCode != 0 {
		err = errors.New(strings.TrimSpace(string(res.Stderr)))
	}
	if err != nil {
		return false, &Error{fmt.Sprintf("Failed to build image for PHP %s: %v", b.PHPVersion, err)}
	}
	return true, nil
}

// composeArgs prefixes a compose subcommand on the generated file.
func composeArgs(args ...string) []string {
	return append([]string{"--log-level", "error", "compose", "-f", compose.FileName}, args...)
}

// ComposeUp brings the stack up to date with env taking precedence over
// .env. With services, only those are updated, without their dependencies;
// otherwise --remove-orphans drops the services removed from the file
// (e.g. Redis once disabled).
func (m *Manager) ComposeUp(ctx context.Context, env map[string]string, services ...string) error {
	args := []string{"up", "-d"}
	if len(services) > 0 {
		args = append(append(args, "--no-deps"), services...)
	} else {
		args = append(args, "--remove-orphans")
	}
	res, err := m.Runner.Run(ctx, m.Dir, env, composeArgs(args...)...)
	if err == nil && res.ExitCode != 0 {
		err = errors.New(string(res.Stderr))
	}
	if err != nil {
		return &Error{"Failed to start containers: " + err.Error()}
	}
	return nil
}

// ComposeDown stops the stack, ignoring failures. Only the generated compose
// file is used.
func (m *Manager) ComposeDown(ctx context.Context) {
	_, _ = m.run(ctx, composeArgs("down")...)
}

// RestartContainer restarts a container, false when it failed (missing).
func (m *Manager) RestartContainer(ctx context.Context, name string) bool {
	return m.ok(ctx, "restart", name)
}

// RestartAll restarts every container of the stack.
func (m *Manager) RestartAll(ctx context.Context, c Containers) {
	for _, name := range AllContainers(c) {
		m.RestartContainer(ctx, name)
	}
}

// StopContainer stops then removes a container, false when it failed.
func (m *Manager) StopContainer(ctx context.Context, name string) bool {
	return m.ok(ctx, "stop", name) && m.ok(ctx, "rm", name)
}

// Exec runs command in a container and returns its exit code and output
// (stdout then stderr). The code is -1 when docker could not run.
func (m *Manager) Exec(ctx context.Context, container string, command ...string) (int, string) {
	res, err := m.run(ctx, append([]string{"exec", container}, command...)...)
	if err != nil {
		return -1, err.Error()
	}
	return res.ExitCode, string(res.Stdout) + string(res.Stderr)
}

// Status is a container's state ("running", "exited"...) and health
// ("healthy", "unhealthy", "starting" or "N/A").
type Status struct {
	State  string
	Health string
}

// NotFound is the status of a container Docker does not know.
var NotFound = Status{State: "not found", Health: "N/A"}

// healthPattern finds the health in the human-readable Status of the
// container list, e.g. "Up 2 minutes (healthy)" or "(health: starting)".
var healthPattern = regexp.MustCompile(`\((?:health: )?(healthy|unhealthy|starting)\)`)

// Statuses maps every container name to its status, in a single call. It
// is empty when Docker is unreachable.
type Statuses map[string]Status

// Get returns the status of name, NotFound when absent.
func (s Statuses) Get(name string) Status {
	if st, ok := s[name]; ok {
		return st
	}
	return NotFound
}

// Running reports whether name is running.
func (s Statuses) Running(name string) bool { return s[name].State == "running" }

// ContainerStatuses lists every container, stopped ones included.
func (m *Manager) ContainerStatuses(ctx context.Context) Statuses {
	res, err := m.run(ctx, "ps", "--all", "--no-trunc", "--format", "json")
	if err != nil || res.ExitCode != 0 {
		return Statuses{}
	}
	return parsePS(res.Stdout)
}

// parsePS parses `docker ps --format json`: one JSON object per line.
func parsePS(out []byte) Statuses {
	statuses := Statuses{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // label-heavy lines can be long
	for sc.Scan() {
		var line struct {
			Names  string // comma-separated
			State  string
			Status string
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		st := Status{State: line.State, Health: "N/A"}
		if st.State == "" {
			st.State = "unknown"
		}
		if m := healthPattern.FindStringSubmatch(line.Status); m != nil {
			st.Health = m[1]
		}
		for _, name := range strings.Split(line.Names, ",") {
			if name = strings.TrimSpace(name); name != "" {
				statuses[name] = st
			}
		}
	}
	return statuses
}

// IsRunning reports whether the server runs: the reverse proxy runs and,
// when PHP domains exist, the container of at least one of their versions.
// A server without PHP domains (proxies only, fresh install) runs none.
func IsRunning(s Statuses, activeVersions []string) bool {
	if !s.Running(compose.ReverseProxyContainer) {
		return false
	}
	if len(activeVersions) == 0 {
		return true
	}
	return slices.ContainsFunc(activeVersions, func(v string) bool {
		return s.Running(phpversions.ContainerName(v))
	})
}

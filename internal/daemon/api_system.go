package daemon

import (
	"context"
	"net"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/platform"
)

// processStart is when the daemon process started.
var processStart = time.Now()

// dockerInfoTimeout bounds the question to Docker: a stopped engine must
// not slow the page down.
const dockerInfoTimeout = 2 * time.Second

// HostSystem is the detailed view of the machine the daemon runs on. Every
// field is best-effort: one that cannot be read is left out.
type HostSystem struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	OSName   string `json:"os_name"`
	Kernel   string `json:"kernel,omitempty"`
	Arch     string `json:"arch"`
	CPU      struct {
		Model string `json:"model,omitempty"`
		Cores int    `json:"cores"`
	} `json:"cpu"`
	Memory struct {
		Total int64 `json:"total"`
	} `json:"memory"`
	// Uptime is the seconds since the host booted
	Uptime  int64        `json:"uptime,omitempty"`
	Disks   []HostDisk   `json:"disks"`
	Daemon  HostDaemon   `json:"daemon"`
	Docker  *docker.Info `json:"docker"`
	Network HostNetwork  `json:"network"`
}

// HostDisk is the space of a filesystem the daemon uses.
type HostDisk struct {
	Label string `json:"label"` // data or projects
	Path  string `json:"path"`
	Total int64  `json:"total"`
	Free  int64  `json:"free"`
}

// HostDaemon describes the daemon process.
type HostDaemon struct {
	Version   string    `json:"version"`
	Go        string    `json:"go"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// HostNetwork is the network of the machine.
type HostNetwork struct {
	LANIP      string          `json:"lan_ip,omitempty"`
	Interfaces []HostInterface `json:"interfaces"`
}

// HostInterface is a network interface with at least one address.
type HostInterface struct {
	Name      string   `json:"name"`
	MAC       string   `json:"mac,omitempty"`
	Up        bool     `json:"up"`
	Addresses []string `json:"addresses"` // CIDR
}

// hostSystem answers GET /api/v1/host/system.
func (s *Server) hostSystem(w http.ResponseWriter, r *http.Request) {
	s.read(w, func(ws *Workspace) (any, error) {
		sys := platform.ReadSystem()
		hs := HostSystem{OS: runtime.GOOS, OSName: sys.OSName, Kernel: sys.Kernel, Arch: runtime.GOARCH,
			Uptime: sys.Uptime, Disks: []HostDisk{}, Network: HostNetwork{Interfaces: []HostInterface{}},
			Daemon: HostDaemon{Version: s.Version, Go: runtime.Version(), PID: os.Getpid(), StartedAt: processStart.UTC()}}
		hs.Hostname, _ = os.Hostname()
		if hs.OSName == "" {
			hs.OSName = runtime.GOOS
		}
		hs.CPU.Model, hs.CPU.Cores = sys.CPUModel, runtime.NumCPU()
		hs.Memory.Total = sys.MemTotal

		for _, d := range []HostDisk{{Label: "data", Path: ws.Dir}, {Label: "projects", Path: projectsDir(ws)}} {
			if d.Path == "" {
				continue
			}
			if total, free, ok := platform.DiskUsage(d.Path); ok {
				d.Total, d.Free = total, free
				hs.Disks = append(hs.Disks, d)
			}
		}

		if ws.Stack != nil && ws.Stack.Docker != nil {
			ctx, cancel := context.WithTimeout(r.Context(), dockerInfoTimeout)
			if info, err := ws.Stack.Docker.Info(ctx); err == nil {
				hs.Docker = info
			}
			cancel()
		}

		if s.LANIP != nil {
			hs.Network.LANIP = s.LANIP()
		}
		hs.Network.Interfaces = interfacesOf()
		return hs, nil
	})
}

// projectsDir is the root of the projects: DEFAULT_PROJECT_PATH, or its
// default.
func projectsDir(ws *Workspace) string {
	if ws.Env == nil {
		return platform.DefaultProjectsDir()
	}
	return ws.Env.Get("DEFAULT_PROJECT_PATH")
}

// interfacesOf lists the network interfaces that have an address, the
// loopback excepted.
func interfacesOf() []HostInterface {
	out := []HostInterface{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil || len(addrs) == 0 {
			continue
		}
		hi := HostInterface{Name: ifc.Name, MAC: ifc.HardwareAddr.String(), Up: ifc.Flags&net.FlagUp != 0, Addresses: []string{}}
		for _, a := range addrs {
			hi.Addresses = append(hi.Addresses, a.String())
		}
		out = append(out, hi)
	}
	return out
}

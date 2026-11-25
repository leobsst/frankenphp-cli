package platform

import (
	"bufio"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func readSystem() System {
	var s System
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		s.OSName = parseOSRelease(string(b))
	}
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		s.CPUModel = parseCPUModel(string(b))
	}
	var u unix.Utsname
	if unix.Uname(&u) == nil {
		s.Kernel = unix.ByteSliceToString(u.Release[:])
	}
	var si unix.Sysinfo_t
	if unix.Sysinfo(&si) == nil {
		s.MemTotal = int64(si.Totalram) * int64(si.Unit)
		s.Uptime = int64(si.Uptime)
	}
	return s
}

// parseOSRelease returns the PRETTY_NAME (or NAME) of an os-release file.
func parseOSRelease(content string) string {
	var name string
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "PRETTY_NAME":
			return v
		case "NAME":
			name = v
		}
	}
	return name
}

// parseCPUModel returns the CPU model of /proc/cpuinfo: "model name", or
// "Hardware" then "Model" on the ARM boards.
func parseCPUModel(content string) string {
	found := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if _, seen := found[k]; !seen && v != "" {
			found[k] = v
		}
	}
	for _, k := range []string{"model name", "Hardware", "Model"} {
		if v := found[k]; v != "" {
			return v
		}
	}
	return ""
}

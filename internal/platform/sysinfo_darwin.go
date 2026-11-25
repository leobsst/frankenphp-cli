package platform

import (
	"time"

	"golang.org/x/sys/unix"
)

func readSystem() System {
	var s System
	if v, err := unix.Sysctl("kern.osproductversion"); err == nil && v != "" {
		s.OSName = "macOS " + v
	}
	s.Kernel, _ = unix.Sysctl("kern.osrelease")
	s.CPUModel, _ = unix.Sysctl("machdep.cpu.brand_string")
	if m, err := unix.SysctlUint64("hw.memsize"); err == nil {
		s.MemTotal = int64(m)
	}
	if tv, err := unix.SysctlTimeval("kern.boottime"); err == nil {
		s.Uptime = max(int64(time.Since(time.Unix(tv.Sec, 0)).Seconds()), 0)
	}
	return s
}

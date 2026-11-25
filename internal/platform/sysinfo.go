package platform

// System describes the machine, every field best-effort: one that cannot
// be read stays empty or zero.
type System struct {
	// OSName is the pretty name of the system ("macOS 26.1")
	OSName string
	// Kernel is the kernel release (the build number on Windows)
	Kernel   string
	CPUModel string
	// MemTotal is the physical memory, in bytes
	MemTotal int64
	// Uptime is the seconds since the host booted
	Uptime int64
}

// ReadSystem reads what the OS tells about the machine.
func ReadSystem() System { return readSystem() }

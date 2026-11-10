package platform

import "golang.org/x/sys/unix"

// ARM64Hardware reports whether the Mac has an Apple Silicon CPU, true
// even for an amd64 binary running under Rosetta.
func ARM64Hardware() bool {
	v, err := unix.SysctlUint32("hw.optional.arm64")
	return err == nil && v == 1
}

//go:build !darwin

package platform

import "runtime"

// ARM64Hardware reports whether the CPU is arm64. Only macOS runs amd64
// binaries on arm64 hardware (Rosetta) in a way the updater must undo.
func ARM64Hardware() bool { return runtime.GOARCH == "arm64" }

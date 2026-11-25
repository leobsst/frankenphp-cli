//go:build !darwin && !linux && !windows

package platform

// readSystem knows nothing of the other systems.
func readSystem() System { return System{} }

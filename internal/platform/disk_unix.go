//go:build !windows

package platform

import "golang.org/x/sys/unix"

func diskUsage(path string) (total, free int64, ok bool) {
	var st unix.Statfs_t
	if unix.Statfs(path, &st) != nil {
		return 0, 0, false
	}
	bs := uint64(st.Bsize)
	// Bavail is signed on some BSDs: the conversion is needed there.
	return int64(st.Blocks * bs), int64(uint64(st.Bavail) * bs), true //nolint:unconvert
}

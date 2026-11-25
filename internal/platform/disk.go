package platform

// DiskUsage returns the size and the free space, in bytes, of the
// filesystem holding path; ok is false when it cannot be read.
func DiskUsage(path string) (total, free int64, ok bool) { return diskUsage(path) }

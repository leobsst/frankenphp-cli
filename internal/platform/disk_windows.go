package platform

import "golang.org/x/sys/windows"

func diskUsage(path string) (total, free int64, ok bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, false
	}
	var avail, tot, totFree uint64
	if windows.GetDiskFreeSpaceEx(p, &avail, &tot, &totFree) != nil {
		return 0, 0, false
	}
	return int64(tot), int64(avail), true
}

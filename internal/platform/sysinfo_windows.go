package platform

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func readSystem() System {
	var s System
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE); err == nil {
		product, _, _ := k.GetStringValue("ProductName")
		display, _, _ := k.GetStringValue("DisplayVersion")
		build, _, _ := k.GetStringValue("CurrentBuild")
		_ = k.Close()
		s.OSName = strings.TrimSpace(product + " " + display)
		s.Kernel = build
	}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE); err == nil {
		name, _, _ := k.GetStringValue("ProcessorNameString")
		_ = k.Close()
		s.CPUModel = strings.TrimSpace(name)
	}
	var mem struct {
		Length                                                                         uint32
		MemoryLoad                                                                     uint32
		TotalPhys, AvailPhys, TotalPageFile, AvailPageFile, TotalVirtual, AvailVirtual uint64
		AvailExtendedVirtual                                                           uint64
	}
	mem.Length = uint32(unsafe.Sizeof(mem))
	if r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&mem))); r != 0 {
		s.MemTotal = int64(mem.TotalPhys)
	}
	if r, _, _ := procGetTickCount64.Call(); r != 0 {
		s.Uptime = int64(r) / 1000
	}
	return s
}

var (
	kernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetTickCount64       = kernel32.NewProc("GetTickCount64")
)

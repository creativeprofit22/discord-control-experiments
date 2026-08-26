package main

import (
	"syscall"
	"unsafe"
)

type processMemoryCounters struct {
	cb                         uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
	privateUsage               uintptr
}

func currentRSS() uint64 {
	counters := processMemoryCounters{cb: uint32(unsafe.Sizeof(processMemoryCounters{}))}
	handle, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetCurrentProcess").Call()
	procedure := syscall.NewLazyDLL("psapi.dll").NewProc("GetProcessMemoryInfo")
	result, _, _ := procedure.Call(handle, uintptr(unsafe.Pointer(&counters)), uintptr(counters.cb))
	if result == 0 {
		return 0
	}
	return uint64(counters.peakWorkingSetSize)
}

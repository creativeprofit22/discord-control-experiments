//go:build windows

package main

import (
	"errors"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"
)

const (
	processQueryInformation = 0x0400
	processVMRead           = 0x0010
	createNewProcessGroup   = 0x00000200
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	psapi                    = syscall.NewLazyDLL("psapi.dll")
	openProcessProc          = kernel32.NewProc("OpenProcess")
	closeHandleProc          = kernel32.NewProc("CloseHandle")
	getProcessMemoryInfoProc = psapi.NewProc("GetProcessMemoryInfo")
)

type processMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup, HideWindow: true}
}

func terminateProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	killer := exec.Command("taskkill.exe", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
	killer.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = killer.Run()
	_ = cmd.Process.Kill()
}

func residentBytes(pid int) (uint64, error) {
	handle, _, callErr := openProcessProc.Call(processQueryInformation|processVMRead, 0, uintptr(pid))
	if handle == 0 {
		return 0, callErr
	}
	defer closeHandleProc.Call(handle)

	var counters processMemoryCounters
	counters.CB = uint32(unsafe.Sizeof(counters))
	ok, _, callErr := getProcessMemoryInfoProc.Call(handle, uintptr(unsafe.Pointer(&counters)), uintptr(counters.CB))
	if ok == 0 {
		if callErr == syscall.Errno(0) {
			callErr = errors.New("GetProcessMemoryInfo failed")
		}
		return 0, callErr
	}
	return uint64(counters.WorkingSetSize), nil
}

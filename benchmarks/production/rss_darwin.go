package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func peakRSS() uint64 {
	var usage syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &usage) != nil {
		return 0
	}
	return uint64(usage.Maxrss)
}

func currentRSS() uint64 {
	output, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		return 0
	}
	kilobytes, _ := strconv.ParseUint(strings.TrimSpace(string(output)), 10, 64)
	return kilobytes * 1024
}

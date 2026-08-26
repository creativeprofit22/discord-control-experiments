package main

import "syscall"

func currentRSS() uint64 {
	var usage syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &usage) != nil {
		return 0
	}
	return uint64(usage.Maxrss)
}

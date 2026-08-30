package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

func peakRSS() uint64 {
	return readRSS("VmHWM:")
}

func currentRSS() uint64 {
	return readRSS("VmRSS:")
}

func readRSS(field string) uint64 {
	file, err := os.Open("/proc/self/status")
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 3 && fields[0] == field {
			value, _ := strconv.ParseUint(fields[1], 10, 64)
			return value * 1024
		}
	}
	return 0
}

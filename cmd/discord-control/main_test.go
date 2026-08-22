package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCrashOutputIsGeneric(t *testing.T) {
	var stderr bytes.Buffer
	secret := "crash-secret-canary"
	code := guardedMain(&stderr, func() error { panic(secret) })
	if code != 1 || strings.Contains(stderr.String(), secret) || stderr.String() != "discord-control terminated unexpectedly\n" {
		t.Fatalf("crash output = %q, code=%d", stderr.String(), code)
	}
}

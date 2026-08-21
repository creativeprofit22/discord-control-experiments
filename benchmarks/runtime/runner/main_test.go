package main

import (
	"path/filepath"
	"testing"
)

func TestCommittedBenchmarkEvidence(t *testing.T) {
	t.Parallel()

	resultPath := filepath.Join("..", "results", "2026-08-21-windows-amd64.json")
	if err := validateResultFile(resultPath); err != nil {
		t.Fatalf("validate committed benchmark evidence: %v", err)
	}
}

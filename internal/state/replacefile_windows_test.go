//go:build windows

package state

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicAutomationJSONReplacementFailurePreservesProjection(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "schedule-workflow.json")
	previous := []byte(`{"revision":1}`)
	if err := os.WriteFile(path, previous, 0o600); err != nil {
		t.Fatal(err)
	}

	locked, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicAutomationJSON(path, map[string]uint64{"revision": 2}); err == nil {
		_ = locked.Close()
		t.Fatal("replacement unexpectedly succeeded while projection was locked")
	}
	if err := locked.Close(); err != nil {
		t.Fatal(err)
	}

	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read prior projection after failed replacement: %v", err)
	}
	if !bytes.Equal(actual, previous) {
		t.Fatalf("prior projection changed after failed replacement: %s", actual)
	}
	if temporary, err := filepath.Glob(filepath.Join(directory, ".automation-*.tmp")); err != nil || len(temporary) != 0 {
		t.Fatalf("temporary files after failed replacement = %v, %v", temporary, err)
	}
}

func TestAtomicAutomationJSONReplacementSurvivesReopen(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "schedule-workflow.json")
	if err := os.WriteFile(path, []byte(`{"revision":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := atomicAutomationJSON(path, map[string]uint64{"revision": 2}); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("reopen replaced projection: %v", err)
	}
	actual, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read reopened projection: %v; close: %v", readErr, closeErr)
	}
	if expected := []byte(`{"revision":2}`); !bytes.Equal(actual, expected) {
		t.Fatalf("reopened projection = %s", actual)
	}
}

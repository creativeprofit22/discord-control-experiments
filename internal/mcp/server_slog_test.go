package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestSanitizedSlogHandlerPreservesContextGroupsAndRedaction(t *testing.T) {
	var output bytes.Buffer
	destination := slog.New(slog.NewJSONHandler(&output, nil))
	logger := slog.New(&sanitizedSlogHandler{logger: destination})

	withSession := logger.With("session", "stable")
	withSession.
		WithGroup("request").
		With("method", "tools/call", "authorization", "Authorization: Bearer should-not-leak").
		InfoContext(context.Background(), "SDK request", "status", "ok")

	entry := decodeSDKLogEntry(t, output.Bytes())
	if entry["component"] != "mcp-sdk" || entry["session"] != "stable" {
		t.Fatalf("persistent root context missing: %#v", entry)
	}
	request, ok := entry["request"].(map[string]any)
	if !ok {
		t.Fatalf("request group missing: %#v", entry)
	}
	if request["method"] != "tools/call" || request["status"] != "ok" {
		t.Fatalf("group context missing: %#v", request)
	}
	if request["authorization"] != "[redacted]" {
		t.Fatalf("secret-shaped persistent value was not redacted: %#v", request)
	}
}

func TestSanitizedSlogHandlerCapsMergedPersistentAndRecordAttrs(t *testing.T) {
	var output bytes.Buffer
	destination := slog.New(slog.NewJSONHandler(&output, nil))
	logger := slog.New(&sanitizedSlogHandler{logger: destination}).With(
		"persistent_1", "one",
		"persistent_2", "two",
		"persistent_3", "three",
		"persistent_4", "four",
	)

	logger.Info("SDK request", "kept", "yes", "dropped", "no")

	entry := decodeSDKLogEntry(t, output.Bytes())
	if entry["persistent_1"] != "one" || entry["persistent_4"] != "four" || entry["kept"] != "yes" {
		t.Fatalf("merged attributes missing: %#v", entry)
	}
	if _, exists := entry["dropped"]; exists {
		t.Fatalf("attribute cap was exceeded: %#v", entry)
	}
}

func decodeSDKLogEntry(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var entry map[string]any
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("decode log entry: %v\n%s", err, data)
	}
	return entry
}

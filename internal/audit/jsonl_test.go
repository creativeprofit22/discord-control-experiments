package audit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func testEvent(n int) domain.AuditEvent {
	return domain.AuditEvent{Kind: "decision", Timestamp: time.Now().UTC(), ActorID: "actor", Adapter: "test", GuildID: "1", Operation: domain.OperationName("inspect_guild"), Capability: domain.CapabilityGuildInspect, ResourceKinds: []domain.ResourceKind{domain.ResourceGuild}, Intent: map[string]string{"count": "1"}, Decision: true, Reason: domain.PolicyAllowed, Mode: domain.ModeApply, DestructiveClass: domain.DestructiveNone, Result: "success", CorrelationID: domain.CorrelationID(string(rune('a' + n%20)))}
}

func TestAppendOnlyHashChainIntegrity(t *testing.T) {
	dir := t.TempDir()
	sink, err := Open(dir, "audit/events.jsonl", 1<<20, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := sink.Append(context.Background(), testEvent(i)); err != nil {
			t.Fatal(err)
		}
	}
	sequence, hash := sink.Head()
	if sequence != 3 || len(hash) != 64 {
		t.Fatalf("head = %d %q", sequence, hash)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir, "audit/events.jsonl", 1<<20, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, _ := reopened.Head(); got != 3 {
		t.Fatalf("restart sequence = %d", got)
	}
}

func TestAuditDetectsTamperReorderAndTruncation(t *testing.T) {
	for _, mode := range []string{"tamper", "reorder", "truncate"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			sink, err := Open(dir, "events.jsonl", 1<<20, nil)
			if err != nil {
				t.Fatal(err)
			}
			_ = sink.Append(context.Background(), testEvent(1))
			_ = sink.Append(context.Background(), testEvent(2))
			_ = sink.Close()
			path := filepath.Join(dir, "events.jsonl")
			data, _ := os.ReadFile(path)
			lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			switch mode {
			case "tamper":
				lines[0] = strings.Replace(lines[0], "inspect_guild", "inspect_roles", 1)
			case "reorder":
				lines[0], lines[1] = lines[1], lines[0]
			case "truncate":
				lines = lines[:1]
			}
			if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(dir, "events.jsonl", 1<<20, nil); !errors.Is(err, ErrIntegrity) {
				t.Fatalf("expected integrity error, got %v", err)
			}
		})
	}
}

func TestAuditConcurrentAppendAndSanitization(t *testing.T) {
	dir := t.TempDir()
	canary := "secret-canary"
	sanitizer := func(value string) (string, bool, error) {
		return strings.ReplaceAll(value, canary, "[REDACTED]"), strings.Contains(value, canary), nil
	}
	sink, err := Open(dir, "events.jsonl", 2<<20, sanitizer)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			event := testEvent(i)
			event.AuditReason = canary
			if err := sink.Append(context.Background(), event); err != nil {
				t.Errorf("append: %v", err)
			}
		}()
	}
	wg.Wait()
	_ = sink.Close()
	data, _ := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if strings.Contains(string(data), canary) || !strings.Contains(string(data), "[REDACTED]") {
		t.Fatal("audit sanitizer failed")
	}
	reopened, err := Open(dir, "events.jsonl", 2<<20, sanitizer)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if sequence, _ := reopened.Head(); sequence != 32 {
		t.Fatalf("sequence = %d", sequence)
	}
}

func FuzzAuditParser(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("not-json\n"))
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > maxAuditRecordBytes*2 {
			return
		}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), input, 0o600); err != nil {
			t.Fatal(err)
		}
		sink, _ := Open(dir, "events.jsonl", 1<<20, nil)
		if sink != nil {
			_ = sink.Close()
		}
	})
}

func TestAuditRejectsUnsafePathsAndMalformedTail(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{"../escape.jsonl", filepath.Join(dir, "absolute.jsonl"), "."} {
		if _, err := Open(dir, path, 1<<20, nil); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("path %q: %v", path, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.jsonl"), []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, "bad.jsonl", 1<<20, nil); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("malformed tail: %v", err)
	}
}

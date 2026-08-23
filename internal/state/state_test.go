package state

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestResolvePathsRejectsTraversalAndSymlinkEscape(t *testing.T) {
	configuration := t.TempDir()
	if _, err := ResolvePaths(configuration, "../outside"); err == nil {
		t.Fatal("path traversal accepted")
	}
	outside := t.TempDir()
	link := filepath.Join(configuration, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := ResolvePaths(configuration, "linked/state"); err == nil {
		t.Fatal("symlink escape accepted")
	}
}

func TestJournalValidReplay(t *testing.T) {
	path := journalTestPath(t)
	journal, events, err := OpenJournal(path, "1", "plan-hash")
	if err != nil || len(events) != 0 {
		t.Fatalf("OpenJournal() events=%v err=%v", events, err)
	}
	first, err := journal.Append(domain.JournalEvent{RunID: "run-1", Type: domain.JournalRunStarted, OccurredAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Append(domain.JournalEvent{RunID: "run-1", Type: domain.JournalStepStarted, OperationID: "op-1", OccurredAt: time.Unix(2, 0)}); err != nil {
		t.Fatal(err)
	}
	replayed, err := ReplayJournal(path, "1", "plan-hash")
	if err != nil || len(replayed) != 2 || replayed[0].RecordHash != first.RecordHash {
		t.Fatalf("ReplayJournal() events=%#v err=%v", replayed, err)
	}
}

func TestJournalToleratesAndRepairsTruncatedFinalJSON(t *testing.T) {
	path := journalTestPath(t)
	journal, _, err := OpenJournal(path, "1", "plan-hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Append(domain.JournalEvent{RunID: "run-1", Type: domain.JournalRunStarted, OccurredAt: time.Unix(1, 0)}); err != nil {
		t.Fatal(err)
	}
	verified, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	appendJournalBytes(t, path, []byte(`{"run_id":`))
	beforeRepair, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := ReplayJournal(path, "1", "plan-hash")
	if err != nil || len(replayed) != 1 {
		t.Fatalf("truncated final record not tolerated: events=%d err=%v", len(replayed), err)
	}
	afterReplay, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterReplay, beforeRepair) {
		t.Fatal("read-only replay repaired the journal")
	}
	_, opened, err := OpenJournal(path, "1", "plan-hash")
	if err != nil || len(opened) != 1 {
		t.Fatalf("OpenJournal() events=%d err=%v", len(opened), err)
	}
	repaired, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(repaired, verified) {
		t.Fatalf("repaired journal differs from verified prefix: got %d bytes want %d", len(repaired), len(verified))
	}
}

func TestJournalRejectsOversizedUnterminatedTail(t *testing.T) {
	path := journalTestPath(t)
	journal, _, err := OpenJournal(path, "1", "plan-hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Append(domain.JournalEvent{RunID: "run-1", Type: domain.JournalRunStarted}); err != nil {
		t.Fatal(err)
	}
	appendJournalBytes(t, path, bytes.Repeat([]byte{'x'}, MaxJournalRecordBytes+1))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenJournal(path, "1", "plan-hash"); err == nil {
		t.Fatal("oversized unterminated tail accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("rejected oversized tail changed the journal")
	}
}

func TestJournalRepairAppendAndReopen(t *testing.T) {
	path := journalTestPath(t)
	journal, _, err := OpenJournal(path, "1", "plan-hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Append(domain.JournalEvent{RunID: "run-1", Type: domain.JournalRunStarted, OccurredAt: time.Unix(1, 0)}); err != nil {
		t.Fatal(err)
	}
	appendJournalBytes(t, path, []byte(`{"run_id":`))
	repaired, events, err := OpenJournal(path, "1", "plan-hash")
	if err != nil || len(events) != 1 {
		t.Fatalf("repair OpenJournal() events=%d err=%v", len(events), err)
	}
	if _, err := repaired.Append(domain.JournalEvent{RunID: "run-1", Type: domain.JournalStepStarted, OperationID: "op-1", OccurredAt: time.Unix(2, 0)}); err != nil {
		t.Fatal(err)
	}
	_, reopened, err := OpenJournal(path, "1", "plan-hash")
	if err != nil || len(reopened) != 2 || reopened[1].Sequence != 2 || reopened[1].PreviousHash != reopened[0].RecordHash {
		t.Fatalf("reopened journal events=%#v err=%v", reopened, err)
	}
}

func TestJournalRejectsInteriorCorruption(t *testing.T) {
	path := journalTestPath(t)
	journal, _, err := OpenJournal(path, "1", "plan-hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Append(domain.JournalEvent{RunID: "run-1", Type: domain.JournalRunStarted}); err != nil {
		t.Fatal(err)
	}
	appendJournalBytes(t, path, []byte("not-json\n"))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenJournal(path, "1", "plan-hash"); err == nil {
		t.Fatal("interior corruption accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("rejected interior corruption changed the journal")
	}
}

func TestJournalRejectsTerminatedValidJSONWithTrailingGarbage(t *testing.T) {
	path, record := journalSingleRecord(t)
	corrupt := append(append([]byte(nil), record...), []byte("garbage\n")...)
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReplayJournal(path, "1", "plan-hash"); err == nil {
		t.Fatal("newline-terminated trailing garbage accepted")
	}
}

func TestJournalRejectsConcatenatedJSONObjects(t *testing.T) {
	path, record := journalSingleRecord(t)
	corrupt := make([]byte, 0, len(record)*2+1)
	corrupt = append(corrupt, record...)
	corrupt = append(corrupt, record...)
	corrupt = append(corrupt, '\n')
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReplayJournal(path, "1", "plan-hash"); err == nil {
		t.Fatal("concatenated JSON objects accepted")
	}
}

func TestJournalAcceptsTrailingJSONWhitespace(t *testing.T) {
	path, record := journalSingleRecord(t)
	withWhitespace := append(append([]byte(nil), record...), []byte(" \t\r\n")...)
	if err := os.WriteFile(path, withWhitespace, 0o600); err != nil {
		t.Fatal(err)
	}
	events, err := ReplayJournal(path, "1", "plan-hash")
	if err != nil || len(events) != 1 {
		t.Fatalf("trailing JSON whitespace rejected: events=%d err=%v", len(events), err)
	}
}

func TestJournalToleratesMalformedTailOnlyWhenUnterminated(t *testing.T) {
	path, record := journalSingleRecord(t)
	malformedTail := []byte(`{"run_id":`)
	unterminated := append(append(append([]byte(nil), record...), '\n'), malformedTail...)
	if err := os.WriteFile(path, unterminated, 0o600); err != nil {
		t.Fatal(err)
	}
	events, err := ReplayJournal(path, "1", "plan-hash")
	if err != nil || len(events) != 1 {
		t.Fatalf("unterminated malformed tail rejected: events=%d err=%v", len(events), err)
	}
	terminated := append(append([]byte(nil), unterminated...), '\n')
	if err := os.WriteFile(path, terminated, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReplayJournal(path, "1", "plan-hash"); err == nil {
		t.Fatal("newline-terminated malformed tail accepted")
	}
}

func journalSingleRecord(t *testing.T) (string, []byte) {
	t.Helper()
	path := journalTestPath(t)
	journal, _, err := OpenJournal(path, "1", "plan-hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Append(domain.JournalEvent{RunID: "run-1", Type: domain.JournalRunStarted, OccurredAt: time.Unix(1, 0)}); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, bytes.TrimSuffix(encoded, []byte{'\n'})
}

func journalTestPath(t *testing.T) string {
	t.Helper()
	paths, err := ResolvePaths(t.TempDir(), "state")
	if err != nil {
		t.Fatal(err)
	}
	path, err := paths.JournalPath("1", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func appendJournalBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBindingsAtomicGuildIsolationAndNoTokens(t *testing.T) {
	paths, err := ResolvePaths(t.TempDir(), "state")
	if err != nil {
		t.Fatal(err)
	}
	store := NewBindingStore(paths)
	bindings := []domain.ResourceBinding{{Ref: domain.StableResourceRef{Kind: domain.ResourceChannel, LogicalID: "chat"}, RemoteID: "2", Fingerprint: "abc"}}
	if err := store.Save("1", bindings); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load("1")
	if err != nil || len(loaded) != 1 || loaded[0].RemoteID != "2" {
		t.Fatalf("Load()=%#v err=%v", loaded, err)
	}
	path, _ := paths.BindingsPath("1")
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(encoded)), "token") || strings.Contains(string(encoded), "https://") {
		t.Fatal("bindings contain secret-capable fields")
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkPrivateFile(file); err == nil {
			t.Fatal("broad permissions accepted")
		}
		file.Close()
	}
	document := strings.Replace(string(encoded), `"guild_id":"1"`, `"guild_id":"9"`, 1)
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("1"); err == nil {
		t.Fatal("cross-guild bindings accepted")
	}
}

func TestGuildLockSerializesApply(t *testing.T) {
	paths, err := ResolvePaths(t.TempDir(), "state")
	if err != nil {
		t.Fatal(err)
	}
	first, err := AcquireGuildLock(paths, "1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireGuildLock(paths, "1"); err == nil {
		t.Fatal("concurrent guild lock acquired")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := AcquireGuildLock(paths, "1")
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

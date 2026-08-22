package state

import (
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

func TestJournalHashChainTruncationAndCorruption(t *testing.T) {
	paths, err := ResolvePaths(t.TempDir(), "state")
	if err != nil {
		t.Fatal(err)
	}
	path, err := paths.JournalPath("1", "run-1")
	if err != nil {
		t.Fatal(err)
	}
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
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"run_id":`); err != nil {
		t.Fatal(err)
	}
	file.Close()
	replayed, err = ReplayJournal(path, "1", "plan-hash")
	if err != nil || len(replayed) != 2 {
		t.Fatalf("truncated final record not tolerated: events=%d err=%v", len(replayed), err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 'X'
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReplayJournal(path, "1", "plan-hash"); err == nil {
		t.Fatal("earlier corruption accepted")
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

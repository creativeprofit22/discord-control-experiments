package state

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func automationTestStore(t *testing.T) (*AutomationStore, Paths) {
	t.Helper()
	root := t.TempDir()
	paths, err := ResolvePaths(root, "state")
	if err != nil {
		t.Fatal(err)
	}
	return NewAutomationStore(paths), paths
}

func automationTestRun() domain.AutomationRun {
	return domain.AutomationRun{ID: "run-123", WorkflowHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", IdempotencyKey: "idem-123", Provenance: domain.AutomationProvenance{WorkflowID: "workflow-1", Definition: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", TriggerID: "manual-1"}}
}

func TestAutomationRestartDuplicateAndCAS(t *testing.T) {
	store, paths := automationTestStore(t)
	created, err := store.EnqueueRun(context.Background(), automationTestRun())
	if err != nil || !created {
		t.Fatalf("enqueue = %v, %v", created, err)
	}
	created, err = NewAutomationStore(paths).EnqueueRun(context.Background(), automationTestRun())
	if err != nil || created {
		t.Fatalf("duplicate = %v, %v", created, err)
	}
	run, err := store.LoadRun("run-123")
	if err != nil || run.Revision != 1 || run.State != domain.AutomationRunQueued {
		t.Fatalf("loaded = %#v, %v", run, err)
	}
	next := run
	next.State = domain.AutomationRunLeased
	next.Lease = domain.AutomationLease{Owner: "worker-1", Token: "token-1", Expires: time.Now().Add(time.Minute)}
	if err := store.CompareAndSwapRun(context.Background(), run.Revision, next); err != nil {
		t.Fatal(err)
	}
	if err := store.CompareAndSwapRun(context.Background(), run.Revision, next); !errors.Is(err, ErrAutomationConflict) {
		t.Fatalf("CAS conflict = %v", err)
	}
}

func TestAutomationConcurrentLeaseReclaimAndHeartbeat(t *testing.T) {
	store, _ := automationTestStore(t)
	if _, err := store.EnqueueRun(context.Background(), automationTestRun()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var successes int
	var mu sync.Mutex
	var wait sync.WaitGroup
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			if _, err := store.ClaimRun(context.Background(), "run-123", "worker", "token-"+string(rune('a'+i)), now, time.Minute); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}(i)
	}
	wait.Wait()
	if successes != 1 {
		t.Fatalf("lease successes = %d", successes)
	}
	run, err := store.LoadRun("run-123")
	if err != nil {
		t.Fatal(err)
	}
	next := run
	next.State = domain.AutomationRunRunning
	if err := store.CompareAndSwapOwnedRun(context.Background(), run.Revision, next, "other", run.Lease.Token, now); err == nil {
		t.Fatal("wrong owner projection write succeeded")
	}
	if err := store.CompareAndSwapOwnedRun(context.Background(), run.Revision, next, run.Lease.Owner, run.Lease.Token, now); err != nil {
		t.Fatalf("owned projection write: %v", err)
	}
	run, _ = store.LoadRun(run.ID)
	if err := store.Heartbeat(context.Background(), run.ID, "other", run.Lease.Token, now, time.Minute); err == nil {
		t.Fatal("wrong owner heartbeat succeeded")
	}
	if _, err := store.ClaimRun(context.Background(), run.ID, "worker-2", "reclaimed", now.Add(2*time.Minute), time.Minute); err != nil {
		t.Fatalf("reclaim: %v", err)
	}
}

func TestAutomationJournalCorruptionAndCrashTail(t *testing.T) {
	_, paths := automationTestStore(t)
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	journal, _, err := OpenAutomationJournal(paths, "run-123", hash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.AppendDispatching("step-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Append("completed", "step-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	path, _ := paths.AutomationPath("journal", "run-123", ".jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString(`{"sequence":3`)
	_ = file.Close()
	events, err := ReplayAutomationJournal(path, "run-123", hash)
	if err != nil || len(events) != 2 {
		t.Fatalf("crash tail replay = %d, %v", len(events), err)
	}
	data, _ := os.ReadFile(path)
	data[20] ^= 1
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReplayAutomationJournal(path, "run-123", hash); err == nil {
		t.Fatal("interior corruption accepted")
	}
}

func TestAutomationJournalRepairsCrashTailBeforeAppend(t *testing.T) {
	_, paths := automationTestStore(t)
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	journal, _, err := OpenAutomationJournal(paths, "run-123", hash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.AppendDispatching("step-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	path, _ := paths.AutomationPath("journal", "run-123", ".jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"sequence":2`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	journal, events, err := OpenAutomationJournal(paths, "run-123", hash)
	if err != nil || len(events) != 1 {
		t.Fatalf("reopen crash tail = %d, %v", len(events), err)
	}
	if _, err := journal.Append("completed", "step-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	events, err = ReplayAutomationJournal(path, "run-123", hash)
	if err != nil || len(events) != 2 || events[1].State != "completed" {
		t.Fatalf("replay repaired journal = %#v, %v", events, err)
	}
}

func TestAutomationScheduleCursorAdvancesDurably(t *testing.T) {
	store, paths := automationTestStore(t)
	schedule := domain.AutomationSchedule{WorkflowID: "workflow-1", WorkflowHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", NextRun: time.Now(), Cursor: "item-1"}
	if err := store.SaveSchedule(context.Background(), 0, schedule); err != nil {
		t.Fatal(err)
	}
	restarted := NewAutomationStore(paths)
	loaded, err := restarted.LoadSchedule("workflow-1")
	if err != nil || loaded.Cursor != "item-1" || loaded.Revision != 1 {
		t.Fatalf("schedule = %#v, %v", loaded, err)
	}
	loaded.Cursor = "item-2"
	if err := restarted.SaveSchedule(context.Background(), 0, loaded); !errors.Is(err, ErrAutomationConflict) {
		t.Fatalf("stale cursor write = %v", err)
	}
}

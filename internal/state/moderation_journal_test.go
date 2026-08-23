package state

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestModerationJournalCrashTailCorruptionAndGuildBinding(t *testing.T) {
	paths, err := ResolvePaths(t.TempDir(), "state")
	if err != nil {
		t.Fatal(err)
	}
	store := NewModerationStore(paths)
	now := time.Unix(100, 0).UTC()
	caseValue := moderationCase(now)
	if _, err := store.Append("1", domain.ModerationJournalEvent{Type: domain.ModerationCaseUpsert, OccurredAt: now, Case: &caseValue}); err != nil {
		t.Fatal(err)
	}
	path, _ := paths.ModerationJournalPath("1")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"sequence":2`); err != nil {
		t.Fatal(err)
	}
	file.Close()
	events, err := store.Replay("1")
	if err != nil || len(events) != 1 {
		t.Fatalf("crash replay events=%d err=%v", len(events), err)
	}
	if _, err := store.Append("1", domain.ModerationJournalEvent{Type: domain.ModerationCaseUpsert, OccurredAt: now.Add(time.Second), Case: &caseValue}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	corrupted := strings.Replace(string(data), `"guild_id":"1"`, `"guild_id":"2"`, 1)
	if err := os.WriteFile(path, []byte(corrupted), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Replay("1"); err == nil || !strings.Contains(err.Error(), "guild mismatch") {
		t.Fatalf("expected guild corruption rejection, got %v", err)
	}
}

func TestModerationStoreRetentionTombstoneRestartDedupAndAppealExport(t *testing.T) {
	paths, err := ResolvePaths(t.TempDir(), "state")
	if err != nil {
		t.Fatal(err)
	}
	store := NewModerationStore(paths)
	now := time.Unix(10_000, 0).UTC()
	old := now.Add(-48 * time.Hour)
	closedAt := old.Add(time.Hour)
	caseValue := moderationCase(old)
	caseValue.Status, caseValue.ClosedAt, caseValue.UpdatedAt = domain.CaseClosed, &closedAt, closedAt
	if _, err := store.Append("1", domain.ModerationJournalEvent{Type: domain.ModerationCaseUpsert, OccurredAt: old, Case: &caseValue}); err != nil {
		t.Fatal(err)
	}
	action := moderationAction(old)
	if _, err := store.Append("1", domain.ModerationJournalEvent{Type: domain.ModerationActionAppend, OccurredAt: old, Action: &action}); err != nil {
		t.Fatal(err)
	}
	completed := old.Add(time.Minute)
	action.Status, action.Result, action.CompletedAt = domain.ActionCompleted, "applied", &completed
	if _, err := store.Append("1", domain.ModerationJournalEvent{Type: domain.ModerationActionAppend, OccurredAt: completed, Action: &action}); err != nil {
		t.Fatal(err)
	}
	evidence := domain.EvidenceReference{ID: "e1", CaseID: "c1", GuildID: "1", SubjectUserID: "2", ActorID: "actor", CorrelationID: "corr", Kind: domain.EvidenceDiscordMessage, ChannelID: "3", MessageID: "4", CreatedAt: old}
	if _, err := store.Append("1", domain.ModerationJournalEvent{Type: domain.ModerationEvidenceAttach, OccurredAt: old, Evidence: &evidence}); err != nil {
		t.Fatal(err)
	}
	before, err := store.AppealExport("1", "2", now)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.AppealExport("1", "2", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Cases) != 1 || len(before.Cases[0].Actions) != 1 || len(before.Cases[0].Evidence) != 1 || !equalWithoutJournal(before, again) {
		t.Fatalf("appeal export unstable: %#v", before)
	}
	if err := store.Compact("1", 24*time.Hour, now); err != nil {
		t.Fatal(err)
	}
	events, err := NewModerationStore(paths).Replay("1")
	if err != nil {
		t.Fatal(err)
	}
	tombstones := 0
	for _, event := range events {
		if event.Tombstone != nil {
			tombstones++
		}
		if event.Evidence != nil {
			t.Fatal("expired evidence survived compaction")
		}
	}
	if tombstones != 1 {
		t.Fatalf("tombstones = %d", tombstones)
	}
	duplicate := moderationAction(old)
	duplicate.ID = "different"
	if _, err := store.Append("1", domain.ModerationJournalEvent{Type: domain.ModerationActionAppend, OccurredAt: now, Action: &duplicate}); err == nil || !strings.Contains(err.Error(), "idempotency") {
		t.Fatalf("expected restart duplicate detection, got %v", err)
	}
}

func moderationCase(now time.Time) domain.Case {
	return domain.Case{ID: "c1", GuildID: "1", SubjectUserID: "2", ActorID: "actor", CorrelationID: "corr", ReasonCode: "spam", Status: domain.CaseOpen, CreatedAt: now, UpdatedAt: now}
}

func moderationAction(now time.Time) domain.ModerationAction {
	return domain.ModerationAction{ID: "a1", CaseID: "c1", GuildID: "1", SubjectUserID: "2", ActorID: "actor", CorrelationID: "corr", IdempotencyKey: "idem", ReasonCode: "spam", Kind: domain.ActionWarn, Status: domain.ActionPending, CreatedAt: now}
}

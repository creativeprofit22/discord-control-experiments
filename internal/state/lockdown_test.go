package state

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestLockdownStorePersistsImmutableHashBoundPlanAndJournal(t *testing.T) {
	paths, err := ResolvePaths(t.TempDir(), "state")
	if err != nil {
		t.Fatal(err)
	}
	store := NewLockdownStore(paths)
	before := domain.PermissionOverwrite{ID: "1", Type: 0, Allow: "0", Deny: "0"}
	after := before
	after.Deny = "2048"
	plan := domain.LockdownPlan{SchemaVersion: domain.LockdownPlanSchemaV1, Kind: domain.LockdownApplyPlan, GuildID: "1", ActorID: "actor", SnapshotHash: "snapshot", ProtectedRoleIDs: []domain.RoleID{"2"}, Scope: domain.LockdownScope{ChannelIDs: []domain.ChannelID{"3"}}, Operations: []domain.LockdownOperation{{ID: "op", ChannelID: "3", Changes: []domain.LockdownOverwriteChange{{ChannelID: "3", TargetID: "1", Type: 0, Before: &before, After: &after}}}}, CreatedAt: time.Unix(1, 0), ExpiresAt: time.Unix(61, 0)}
	hash, err := plan.CanonicalHash()
	if err != nil {
		t.Fatal(err)
	}
	plan.Hash = hash
	if err := store.SavePlan(plan); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(plan); err == nil {
		t.Fatal("immutable plan overwrite accepted")
	}
	loaded, err := store.LoadPlan("1", hash)
	if err != nil || loaded.Hash != hash {
		t.Fatalf("load=%#v err=%v", loaded, err)
	}
	event := domain.LockdownJournalEvent{Type: domain.LockdownOperationStarted, OperationID: "op", OccurredAt: time.Unix(2, 0)}
	if _, err := store.Append("1", hash, event); err != nil {
		t.Fatal(err)
	}
	journalPath, _ := paths.LockdownJournalPath("1", hash)
	file, _ := os.OpenFile(journalPath, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = file.WriteString(`{"sequence":2`)
	file.Close()
	events, err := store.Replay("1", hash)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%d err=%v", len(events), err)
	}
	data, _ := os.ReadFile(journalPath)
	corrupted := strings.Replace(string(data), `"guild_id":"1"`, `"guild_id":"2"`, 1)
	_ = os.WriteFile(journalPath, []byte(corrupted), 0o600)
	if _, err := store.Replay("1", hash); err == nil || !strings.Contains(err.Error(), "binding mismatch") {
		t.Fatalf("expected binding rejection, got %v", err)
	}
}

package application

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/ken-kai/discord-control-experiments/internal/reconcile"
)

type reconciliationClock struct{ now time.Time }

func (clock reconciliationClock) Now() time.Time { return clock.now }

type reconciliationIDs struct{ value domain.CorrelationID }

func (ids reconciliationIDs) NewCorrelationID() (domain.CorrelationID, error) { return ids.value, nil }

type memoryJournal struct{ events []domain.JournalEvent }

func (journal *memoryJournal) Append(event domain.JournalEvent) (domain.JournalEvent, error) {
	event.Sequence = uint64(len(journal.events) + 1)
	journal.events = append(journal.events, event)
	return event, nil
}

type reconciliationState struct {
	mu       sync.Mutex
	bindings []domain.ResourceBinding
	plans    map[domain.RunID]domain.Plan
	journals map[domain.RunID]*memoryJournal
	locked   bool
}

func newReconciliationState() *reconciliationState {
	return &reconciliationState{plans: map[domain.RunID]domain.Plan{}, journals: map[domain.RunID]*memoryJournal{}}
}
func (state *reconciliationState) LoadBindings(domain.GuildID) ([]domain.ResourceBinding, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	return append([]domain.ResourceBinding(nil), state.bindings...), nil
}
func (state *reconciliationState) SaveBindings(_ domain.GuildID, bindings []domain.ResourceBinding) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.bindings = append([]domain.ResourceBinding(nil), bindings...)
	return nil
}
func (state *reconciliationState) AcquireGuildLock(domain.GuildID) (io.Closer, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.locked {
		return nil, fmt.Errorf("locked")
	}
	state.locked = true
	return unlocker{state: state}, nil
}

type unlocker struct{ state *reconciliationState }

func (lock unlocker) Close() error {
	lock.state.mu.Lock()
	defer lock.state.mu.Unlock()
	lock.state.locked = false
	return nil
}
func (state *reconciliationState) OpenJournal(_ domain.GuildID, runID domain.RunID, _ domain.PlanHash) (ReconciliationJournal, []domain.JournalEvent, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	journal := state.journals[runID]
	if journal == nil {
		journal = &memoryJournal{}
		state.journals[runID] = journal
	}
	return journal, append([]domain.JournalEvent(nil), journal.events...), nil
}
func (state *reconciliationState) SavePlan(_ domain.GuildID, runID domain.RunID, plan domain.Plan) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.plans[runID] = plan
	return nil
}
func (state *reconciliationState) LoadPlan(_ domain.GuildID, runID domain.RunID) (domain.Plan, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	plan, ok := state.plans[runID]
	if !ok {
		return domain.Plan{}, fmt.Errorf("missing plan")
	}
	return plan, nil
}

func TestReconciliationApplyRequiresExactHashAndFreshSnapshot(t *testing.T) {
	principal := domain.Principal{Adapter: "test", Subject: "actor", AuthenticationMethod: "in-process"}
	guildID := domain.GuildID("1")
	inspection := domain.GuildInspection{Guild: domain.Guild{ID: guildID}, Channels: []domain.Channel{}, Roles: []domain.Role{}, Webhooks: []domain.Webhook{}, ApplicationCommands: []domain.ApplicationCommand{}}
	snapshot, err := reconcile.NormalizeSnapshot(reconcile.SnapshotInput{Guild: inspection.Guild, CapturedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	desired := domain.DesiredGuild{SchemaVersion: domain.DeclarativeSchemaV1, GuildID: guildID, Ownership: domain.OwnershipMetadata{Owner: "team"}, Categories: []domain.DesiredCategory{}, Channels: []domain.DesiredChannel{{LogicalID: "chat", Name: "chat", Type: 0, Position: 0, Overwrites: []domain.DesiredOverwrite{}}}, Roles: []domain.DesiredRole{}, Webhooks: []domain.DesiredWebhook{}, ApplicationCommands: []domain.DesiredApplicationCommand{}}
	plan, err := reconcile.BuildPlan(reconcile.PlanRequest{Desired: desired, Snapshot: snapshot, ActorID: "actor", CreatedAt: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("hash mismatch blocks before Discord", func(t *testing.T) {
		port := &scriptedPort{}
		secured, _ := securedServiceWithPolicy(port, allowTestPolicy{}, domain.ModeApply)
		service, err := NewReconciliationService(secured, newReconciliationState(), reconciliationClock{time.Unix(3, 0)}, reconciliationIDs{"run-1"}, nil, ReconciliationLimits{MaxOperations: 10, MaxDestructiveOperations: 1})
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.Apply(context.Background(), domain.ApplyRequest{Plan: plan, PlanHash: "wrong", Principal: principal, ActorID: "actor", CorrelationID: "correlation"})
		if err == nil || port.count() != 0 {
			t.Fatalf("err=%v calls=%d", err, port.count())
		}
	})

	t.Run("stale snapshot blocks mutation", func(t *testing.T) {
		name := "unmanaged"
		stale := inspection
		stale.Channels = []domain.Channel{{ID: "9", GuildID: &guildID, Name: &name, Type: 0}}
		port := &scriptedPort{results: []DiscordPortResult{{Value: stale, Attempts: 1}}}
		secured, _ := securedServiceWithPolicy(port, allowTestPolicy{}, domain.ModeApply)
		service, _ := NewReconciliationService(secured, newReconciliationState(), reconciliationClock{time.Unix(3, 0)}, reconciliationIDs{"run-1"}, nil, ReconciliationLimits{MaxOperations: 10, MaxDestructiveOperations: 1})
		_, err := service.Apply(context.Background(), domain.ApplyRequest{Plan: plan, PlanHash: plan.Hash, Principal: principal, ActorID: "actor", CorrelationID: "correlation"})
		if err == nil || port.count() != 1 {
			t.Fatalf("err=%v calls=%d", err, port.count())
		}
	})

	t.Run("journals and binds confirmed create", func(t *testing.T) {
		createdName := "chat"
		created := domain.Channel{ID: "2", GuildID: &guildID, Name: &createdName, Type: 0}
		port := &scriptedPort{results: []DiscordPortResult{{Value: inspection, Attempts: 1}, {Value: []domain.Channel{}, Attempts: 1}, {Value: created, Attempts: 1}}}
		secured, _ := securedServiceWithPolicy(port, allowTestPolicy{}, domain.ModeApply)
		state := newReconciliationState()
		service, _ := NewReconciliationService(secured, state, reconciliationClock{time.Unix(3, 0)}, reconciliationIDs{"run-1"}, nil, ReconciliationLimits{MaxOperations: 10, MaxDestructiveOperations: 1})
		result, err := service.Apply(context.Background(), domain.ApplyRequest{Plan: plan, PlanHash: plan.Hash, Principal: principal, ActorID: "actor", CorrelationID: "correlation"})
		if err != nil {
			t.Fatal(err)
		}
		if result.State != domain.RunCompleted || len(state.bindings) != 1 || state.bindings[0].RemoteID != "2" {
			t.Fatalf("result=%#v bindings=%#v", result, state.bindings)
		}
		events := state.journals["run-1"].events
		if len(events) != 4 || events[0].Type != domain.JournalRunStarted || events[1].Type != domain.JournalStepStarted || events[2].Type != domain.JournalStepComplete || events[3].Type != domain.JournalRunComplete {
			t.Fatalf("events=%#v", events)
		}
	})
}

func TestReconciliationDriftReportsManagedAndUnmanagedConflicts(t *testing.T) {
	principal := domain.Principal{Adapter: "test", Subject: "actor", AuthenticationMethod: "in-process"}
	guildID := domain.GuildID("1")
	baseSnapshot, err := reconcile.NormalizeSnapshot(reconcile.SnapshotInput{Guild: domain.Guild{ID: guildID}, CapturedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	desired := domain.DesiredGuild{SchemaVersion: domain.DeclarativeSchemaV1, GuildID: guildID, Ownership: domain.OwnershipMetadata{Owner: "team"}, Categories: []domain.DesiredCategory{}, Channels: []domain.DesiredChannel{}, Roles: []domain.DesiredRole{}, Webhooks: []domain.DesiredWebhook{}, ApplicationCommands: []domain.DesiredApplicationCommand{}}
	plan, err := reconcile.BuildPlan(reconcile.PlanRequest{Desired: desired, Snapshot: baseSnapshot, ActorID: "actor", CreatedAt: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	plan.Conflicts = append(plan.Conflicts, domain.PlanConflict{Kind: domain.ConflictUnmanagedResource, Resource: domain.StableResourceRef{Kind: domain.ResourceChannel, LogicalID: "external"}, Message: "unmanaged fixture"})
	channelName := "external"
	inspection := domain.GuildInspection{Guild: domain.Guild{ID: guildID}, Channels: []domain.Channel{{ID: "9", GuildID: &guildID, Name: &channelName, Type: 0}}, Roles: []domain.Role{}, Webhooks: []domain.Webhook{}, ApplicationCommands: []domain.ApplicationCommand{}}
	port := &scriptedPort{results: []DiscordPortResult{{Value: inspection, Attempts: 1}}}
	secured, _ := securedServiceWithPolicy(port, allowTestPolicy{}, domain.ModeApply)
	service, err := NewReconciliationService(secured, newReconciliationState(), reconciliationClock{time.Unix(3, 0)}, reconciliationIDs{"run-1"}, nil, ReconciliationLimits{MaxOperations: 10, MaxDestructiveOperations: 1})
	if err != nil {
		t.Fatal(err)
	}
	report, err := service.Drift(context.Background(), DriftRequest{Plan: plan, ActorID: "actor", Principal: principal, CorrelationID: "correlation"})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Stale || len(report.ManagedDrift) != 1 || len(report.UnmanagedConflicts) != 1 {
		t.Fatalf("drift report = %#v", report)
	}
}

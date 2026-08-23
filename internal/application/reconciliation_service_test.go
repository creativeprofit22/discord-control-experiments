package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
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

type boundReconciliationApproval struct {
	id             domain.ApprovalID
	actor          domain.ActorID
	guild          domain.GuildID
	planHash       domain.PlanHash
	expiresAt      time.Time
	consumed       bool
	planConsumes   int
	operationCalls int
}

func (approval *boundReconciliationApproval) ConsumeOperation(context.Context, domain.ApprovalID, domain.ActorID, domain.GuildID, string, time.Time) (domain.ApprovalGrant, error) {
	approval.operationCalls++
	return domain.ApprovalGrant{}, fmt.Errorf("unexpected operation approval consumption")
}

func (approval *boundReconciliationApproval) ConsumeLockdownPlan(context.Context, domain.ApprovalID, domain.ActorID, domain.GuildID, string, time.Time) (domain.ApprovalGrant, error) {
	return domain.ApprovalGrant{}, fmt.Errorf("unexpected lockdown approval consumption")
}

func (approval *boundReconciliationApproval) ConsumeReconciliationPlan(_ context.Context, id domain.ApprovalID, actor domain.ActorID, guild domain.GuildID, planHash string, now time.Time) (domain.ApprovalGrant, error) {
	approval.planConsumes++
	if id != approval.id || actor != approval.actor || guild != approval.guild || planHash != string(approval.planHash) {
		return domain.ApprovalGrant{}, fmt.Errorf("plan approval binding mismatch")
	}
	if (!approval.expiresAt.IsZero() && !now.Before(approval.expiresAt)) || approval.consumed {
		return domain.ApprovalGrant{}, fmt.Errorf("plan approval is expired or consumed")
	}
	approval.consumed = true
	expiresAt := approval.expiresAt
	if expiresAt.IsZero() {
		expiresAt = now.Add(time.Minute)
	}
	return domain.ApprovalGrant{ID: id, ActorID: actor, GuildID: guild, ScopeKind: domain.ApprovalScopeReconciliationPlan, ScopeHash: planHash, ApproverID: actor, IssuedAt: now.Add(-time.Minute), ExpiresAt: expiresAt}, nil
}

type memoryJournal struct{ events []domain.JournalEvent }

func (journal *memoryJournal) Append(event domain.JournalEvent) (domain.JournalEvent, error) {
	event.Sequence = uint64(len(journal.events) + 1)
	journal.events = append(journal.events, event)
	return event, nil
}

type reconciliationState struct {
	mu               sync.Mutex
	bindings         []domain.ResourceBinding
	plans            map[domain.RunID]domain.Plan
	journals         map[domain.RunID]*memoryJournal
	journalOpenCalls int
	locked           bool
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
func (state *reconciliationState) LoadJournalEvents(_ domain.GuildID, runID domain.RunID, _ domain.PlanHash) ([]domain.JournalEvent, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	journal := state.journals[runID]
	if journal == nil {
		return []domain.JournalEvent{}, nil
	}
	return append([]domain.JournalEvent(nil), journal.events...), nil
}

func (state *reconciliationState) OpenJournal(_ domain.GuildID, runID domain.RunID, _ domain.PlanHash) (ReconciliationJournal, []domain.JournalEvent, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.journalOpenCalls++
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

type durableReconciliationState struct {
	bindings []domain.ResourceBinding
	plans    map[domain.RunID]domain.Plan
	journals map[domain.RunID][]domain.JournalEvent
}

func captureDurableReconciliationState(state *reconciliationState) durableReconciliationState {
	state.mu.Lock()
	defer state.mu.Unlock()
	captured := durableReconciliationState{
		bindings: append([]domain.ResourceBinding(nil), state.bindings...),
		plans:    make(map[domain.RunID]domain.Plan, len(state.plans)),
		journals: make(map[domain.RunID][]domain.JournalEvent, len(state.journals)),
	}
	for runID, plan := range state.plans {
		captured.plans[runID] = plan
	}
	for runID, journal := range state.journals {
		captured.journals[runID] = append([]domain.JournalEvent(nil), journal.events...)
	}
	return captured
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

func TestReconciliationPermissionOverwriteRequiresBoundPlanApproval(t *testing.T) {
	now := time.Unix(3, 0)
	principal := domain.Principal{Adapter: "test", Subject: "actor", AuthenticationMethod: "in-process"}
	actorID := domain.ActorID("actor")
	guildID := domain.GuildID("1")
	channelRef := domain.StableResourceRef{Kind: domain.ResourceChannel, LogicalID: "chat"}
	binding := domain.ResourceBinding{Ref: channelRef, RemoteID: "2", Fingerprint: "bound-channel"}
	channelName := "chat"
	observedChannel := domain.Channel{ID: "2", GuildID: &guildID, Name: &channelName, Type: 0, PermissionOverwrites: []domain.PermissionOverwrite{{ID: "99", Type: 0, Allow: "1", Deny: "0"}}}
	inspection := domain.GuildInspection{Guild: domain.Guild{ID: guildID}, Channels: []domain.Channel{observedChannel}, Roles: []domain.Role{}, Webhooks: []domain.Webhook{}, ApplicationCommands: []domain.ApplicationCommand{}}
	snapshot, err := reconcile.NormalizeSnapshot(reconcile.SnapshotInput{Guild: inspection.Guild, Channels: inspection.Channels, Bindings: []domain.ResourceBinding{binding}, CapturedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	desired := domain.DesiredGuild{SchemaVersion: domain.DeclarativeSchemaV1, GuildID: guildID, Ownership: domain.OwnershipMetadata{Owner: "team"}, Categories: []domain.DesiredCategory{}, Channels: []domain.DesiredChannel{{LogicalID: "chat", Name: "chat", Type: 0, Overwrites: []domain.DesiredOverwrite{}}}, Roles: []domain.DesiredRole{}, Webhooks: []domain.DesiredWebhook{}, ApplicationCommands: []domain.DesiredApplicationCommand{}}
	plan, err := reconcile.BuildPlan(reconcile.PlanRequest{Desired: desired, Snapshot: snapshot, ActorID: actorID, CreatedAt: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 1 || plan.Operations[0].Kind != domain.OperationOverwrite || plan.Operations[0].Risk != domain.RiskPermissionSensitive || !plan.Operations[0].RequiresApproval {
		t.Fatalf("permission-overwrite plan = %#v", plan.Operations)
	}

	newService := func(t *testing.T, port *scriptedPort, approvals ApprovalStore, runID domain.CorrelationID) (*ReconciliationService, *memoryTestAudit, *reconciliationState) {
		t.Helper()
		audit := &memoryTestAudit{}
		secured, err := NewSecuredDiscordService(port, NewInMemoryIdempotencyStore(), NewInMemoryResourceOwnershipStore(), nil, SecurityDependencies{Policy: allowTestPolicy{}, Approvals: approvals, Audit: audit, DefaultInvocation: Invocation{Principal: principal, RequestedMode: domain.ModeApply, CorrelationID: "default"}})
		if err != nil {
			t.Fatal(err)
		}
		state := newReconciliationState()
		state.bindings = []domain.ResourceBinding{binding}
		service, err := NewReconciliationService(secured, state, reconciliationClock{now: now}, reconciliationIDs{value: runID}, nil, ReconciliationLimits{MaxOperations: 10, MaxDestructiveOperations: 1})
		if err != nil {
			t.Fatal(err)
		}
		return service, audit, state
	}

	t.Run("fails without approval", func(t *testing.T) {
		port := &scriptedPort{results: []DiscordPortResult{{Value: inspection, Attempts: 1}}}
		service, _, _ := newService(t, port, &boundReconciliationApproval{}, "run-without-approval")
		_, err := service.Apply(context.Background(), domain.ApplyRequest{Plan: plan, PlanHash: plan.Hash, Principal: principal, ActorID: actorID, CorrelationID: "without-approval"})
		var authorization *domain.AuthorizationError
		if !errors.As(err, &authorization) || authorization.Reason != domain.PolicyApprovalRequired {
			t.Fatalf("error = %v", err)
		}
		if port.count() != 1 {
			t.Fatalf("transport calls = %d, want snapshot only", port.count())
		}
	})

	t.Run("apply success emits no plan-authorization denial", func(t *testing.T) {
		approval := &boundReconciliationApproval{id: "bound-approval", actor: actorID, guild: guildID, planHash: plan.Hash}
		modifiedChannel := observedChannel
		modifiedChannel.PermissionOverwrites = []domain.PermissionOverwrite{}
		port := &scriptedPort{results: []DiscordPortResult{{Value: inspection, Attempts: 1}, {Value: modifiedChannel, Attempts: 1}}}
		service, audit, _ := newService(t, port, approval, "run-with-approval")
		result, err := service.Apply(context.Background(), domain.ApplyRequest{Plan: plan, PlanHash: plan.Hash, Principal: principal, ActorID: actorID, ApprovalID: approval.id, CorrelationID: "with-approval"})
		if err != nil {
			t.Fatal(err)
		}
		if result.State != domain.RunCompleted || port.count() != 2 {
			t.Fatalf("result=%#v transport calls=%d", result, port.count())
		}
		if approval.planConsumes != 1 || approval.operationCalls != 0 {
			t.Fatalf("plan consumes=%d operation calls=%d", approval.planConsumes, approval.operationCalls)
		}
		for _, event := range audit.events {
			if event.Operation == "apply_reconciliation_plan" {
				t.Fatalf("unexpected plan-authorization event: %#v", event)
			}
		}
	})

	t.Run("resume success emits no plan-authorization denial", func(t *testing.T) {
		approval := &boundReconciliationApproval{id: "bound-resume-approval", actor: actorID, guild: guildID, planHash: plan.Hash}
		port := &scriptedPort{results: []DiscordPortResult{{Value: inspection, Attempts: 1}}}
		service, audit, state := newService(t, port, approval, "unused-run")
		resource := plan.Operations[0].Resource
		state.plans["resume-run"] = plan
		state.journals["resume-run"] = &memoryJournal{events: []domain.JournalEvent{
			{RunID: "resume-run", Type: domain.JournalRunStarted, OccurredAt: time.Unix(2, 0)},
			{RunID: "resume-run", Type: domain.JournalStepComplete, OperationID: plan.Operations[0].ID, Resource: &resource, State: domain.StepCompleted, RemoteID: binding.RemoteID, Fingerprint: binding.Fingerprint, OccurredAt: time.Unix(2, 0)},
		}}
		result, err := service.Resume(context.Background(), ResumeRequest{GuildID: guildID, RunID: "resume-run", PlanHash: plan.Hash, ActorID: actorID, Principal: principal, ApprovalID: approval.id, CorrelationID: "resume-with-approval"})
		if err != nil {
			t.Fatal(err)
		}
		if result.State != domain.RunCompleted || port.count() != 1 || approval.planConsumes != 1 {
			t.Fatalf("result=%#v transport calls=%d plan consumes=%d", result, port.count(), approval.planConsumes)
		}
		for _, event := range audit.events {
			if event.Operation == "resume_reconciliation_plan" {
				t.Fatalf("unexpected plan-authorization event: %#v", event)
			}
		}
	})
}

func TestReconciliationApprovalDenialsLeaveDurableStateUnchanged(t *testing.T) {
	now := time.Unix(3, 0)
	principal := domain.Principal{Adapter: "test", Subject: "actor", AuthenticationMethod: "in-process"}
	actorID := domain.ActorID("actor")
	guildID := domain.GuildID("1")
	channelRef := domain.StableResourceRef{Kind: domain.ResourceChannel, LogicalID: "chat"}
	binding := domain.ResourceBinding{Ref: channelRef, RemoteID: "2", Fingerprint: "bound-channel"}
	channelName := "chat"
	observedChannel := domain.Channel{ID: "2", GuildID: &guildID, Name: &channelName, Type: 0, PermissionOverwrites: []domain.PermissionOverwrite{{ID: "99", Type: 0, Allow: "1", Deny: "0"}}}
	inspection := domain.GuildInspection{Guild: domain.Guild{ID: guildID}, Channels: []domain.Channel{observedChannel}, Roles: []domain.Role{}, Webhooks: []domain.Webhook{}, ApplicationCommands: []domain.ApplicationCommand{}}
	snapshot, err := reconcile.NormalizeSnapshot(reconcile.SnapshotInput{Guild: inspection.Guild, Channels: inspection.Channels, Bindings: []domain.ResourceBinding{binding}, CapturedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	desired := domain.DesiredGuild{SchemaVersion: domain.DeclarativeSchemaV1, GuildID: guildID, Ownership: domain.OwnershipMetadata{Owner: "team"}, Categories: []domain.DesiredCategory{}, Channels: []domain.DesiredChannel{{LogicalID: "chat", Name: "chat", Type: 0, Overwrites: []domain.DesiredOverwrite{}}}, Roles: []domain.DesiredRole{}, Webhooks: []domain.DesiredWebhook{}, ApplicationCommands: []domain.DesiredApplicationCommand{}}
	plan, err := reconcile.BuildPlan(reconcile.PlanRequest{Desired: desired, Snapshot: snapshot, ActorID: actorID, CreatedAt: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 1 || !plan.Operations[0].RequiresApproval {
		t.Fatalf("approval-boundary plan = %#v", plan.Operations)
	}

	denials := []struct {
		name     string
		expected domain.PolicyReason
		prepare  func(*boundReconciliationApproval) domain.ApprovalID
	}{
		{name: "missing", expected: domain.PolicyApprovalRequired, prepare: func(*boundReconciliationApproval) domain.ApprovalID { return "" }},
		{name: "expired", expected: domain.PolicyApprovalInvalid, prepare: func(approval *boundReconciliationApproval) domain.ApprovalID {
			approval.expiresAt = now
			return approval.id
		}},
		{name: "actor_mismatched", expected: domain.PolicyApprovalInvalid, prepare: func(approval *boundReconciliationApproval) domain.ApprovalID {
			approval.actor = "other-actor"
			return approval.id
		}},
		{name: "guild_mismatched", expected: domain.PolicyApprovalInvalid, prepare: func(approval *boundReconciliationApproval) domain.ApprovalID {
			approval.guild = "other-guild"
			return approval.id
		}},
		{name: "plan_hash_mismatched", expected: domain.PolicyApprovalInvalid, prepare: func(approval *boundReconciliationApproval) domain.ApprovalID {
			approval.planHash = "other-plan-hash"
			return approval.id
		}},
		{name: "replayed", expected: domain.PolicyApprovalInvalid, prepare: func(approval *boundReconciliationApproval) domain.ApprovalID {
			approval.consumed = true
			return approval.id
		}},
	}

	for _, action := range []string{"apply", "resume"} {
		for _, denial := range denials {
			t.Run(action+"/"+denial.name, func(t *testing.T) {
				approval := &boundReconciliationApproval{id: "approval", actor: actorID, guild: guildID, planHash: plan.Hash, expiresAt: now.Add(time.Minute)}
				approvalID := denial.prepare(approval)
				state := newReconciliationState()
				state.bindings = []domain.ResourceBinding{binding}
				if action == "resume" {
					resource := plan.Operations[0].Resource
					state.plans["resume-run"] = plan
					state.journals["resume-run"] = &memoryJournal{events: []domain.JournalEvent{
						{RunID: "resume-run", Type: domain.JournalRunStarted, OccurredAt: time.Unix(2, 0)},
						{RunID: "resume-run", Type: domain.JournalStepComplete, OperationID: plan.Operations[0].ID, Resource: &resource, State: domain.StepCompleted, RemoteID: "journal-remote", Fingerprint: "journal-fingerprint", OccurredAt: time.Unix(2, 0)},
					}}
				}
				before := captureDurableReconciliationState(state)
				port := &scriptedPort{results: []DiscordPortResult{{Value: inspection, Attempts: 1}}}
				audit := &memoryTestAudit{}
				secured, err := NewSecuredDiscordService(port, NewInMemoryIdempotencyStore(), NewInMemoryResourceOwnershipStore(), nil, SecurityDependencies{Policy: allowTestPolicy{}, Approvals: approval, Audit: audit, DefaultInvocation: Invocation{Principal: principal, RequestedMode: domain.ModeApply, CorrelationID: "default"}})
				if err != nil {
					t.Fatal(err)
				}
				service, err := NewReconciliationService(secured, state, reconciliationClock{now: now}, reconciliationIDs{value: "apply-run"}, nil, ReconciliationLimits{MaxOperations: 10, MaxDestructiveOperations: 1})
				if err != nil {
					t.Fatal(err)
				}

				if action == "apply" {
					_, err = service.Apply(context.Background(), domain.ApplyRequest{Plan: plan, PlanHash: plan.Hash, Principal: principal, ActorID: actorID, ApprovalID: approvalID, CorrelationID: "apply-denied"})
				} else {
					_, err = service.Resume(context.Background(), ResumeRequest{GuildID: guildID, RunID: "resume-run", PlanHash: plan.Hash, ActorID: actorID, Principal: principal, ApprovalID: approvalID, CorrelationID: "resume-denied"})
				}
				var authorization *domain.AuthorizationError
				if !errors.As(err, &authorization) || authorization.Reason != denial.expected {
					t.Fatalf("error = %v, want authorization reason %s", err, denial.expected)
				}
				if port.count() != 1 {
					t.Fatalf("transport calls = %d, want snapshot validation only", port.count())
				}
				operation := domain.OperationName("apply_reconciliation_plan")
				correlationID := domain.CorrelationID("apply-denied")
				if action == "resume" {
					operation = "resume_reconciliation_plan"
					correlationID = "resume-denied"
				}
				denialEvents := []domain.AuditEvent{}
				for _, event := range audit.events {
					if event.Operation == operation {
						denialEvents = append(denialEvents, event)
					}
				}
				if len(denialEvents) != 1 {
					t.Fatalf("plan-authorization denial events = %d, want 1", len(denialEvents))
				}
				event := denialEvents[0]
				if event.Kind != "decision" || event.ActorID != actorID || event.Adapter != principal.Adapter || event.GuildID != guildID || event.Operation != operation || event.Decision || event.Reason != denial.expected || event.Mode != domain.ModeApply || event.Result != "approval_denied" || event.CorrelationID != correlationID || event.OperationHash != string(plan.Hash) {
					t.Fatalf("denial event = %#v", event)
				}
				if event.ApprovalID != "" || event.ApproverID != "" || len(event.Intent) != 0 {
					t.Fatalf("denial event contains approval or intent data: %#v", event)
				}
				if state.journalOpenCalls != 0 {
					t.Fatalf("journal opens = %d, want none before authorization", state.journalOpenCalls)
				}
				after := captureDurableReconciliationState(state)
				if !reflect.DeepEqual(after, before) {
					t.Fatalf("durable state changed after denial\nbefore=%#v\nafter=%#v", before, after)
				}
			})
		}
	}
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

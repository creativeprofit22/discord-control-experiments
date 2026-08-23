package application

import (
	"context"
	"errors"
	"fmt"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/ken-kai/discord-control-experiments/internal/reconcile"
	"reflect"
	"testing"
	"time"
)

func TestReconciliationResumeValidatesBeforeConsumingPlanApproval(t *testing.T) {
	guildID := domain.GuildID("1")
	actorID := domain.ActorID("actor")
	principal := domain.Principal{Adapter: "test", Subject: "actor", AuthenticationMethod: "in-process"}
	channelRef := domain.StableResourceRef{Kind: domain.ResourceChannel, LogicalID: "chat"}
	binding := domain.ResourceBinding{Ref: channelRef, RemoteID: "2", Fingerprint: "bound-channel"}
	channelName := "chat"
	observedChannel := domain.Channel{ID: "2", GuildID: &guildID, Name: &channelName, Type: 0}
	inspection := domain.GuildInspection{Guild: domain.Guild{ID: guildID}, Channels: []domain.Channel{observedChannel}, Roles: []domain.Role{}, Webhooks: []domain.Webhook{}, ApplicationCommands: []domain.ApplicationCommand{}}
	snapshot, err := reconcile.NormalizeSnapshot(reconcile.SnapshotInput{Guild: inspection.Guild, Channels: inspection.Channels, Bindings: []domain.ResourceBinding{binding}, CapturedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	desired := domain.DesiredGuild{SchemaVersion: domain.DeclarativeSchemaV1, GuildID: guildID, Ownership: domain.OwnershipMetadata{Owner: "team", DeleteOrphans: true}, Categories: []domain.DesiredCategory{}, Channels: []domain.DesiredChannel{{LogicalID: "chat", Name: "chat", Type: 2, Overwrites: []domain.DesiredOverwrite{}}}, Roles: []domain.DesiredRole{}, Webhooks: []domain.DesiredWebhook{}, ApplicationCommands: []domain.DesiredApplicationCommand{}}
	plan, err := reconcile.BuildPlan(reconcile.PlanRequest{Desired: desired, Snapshot: snapshot, ActorID: actorID, CreatedAt: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 1 || plan.Operations[0].Kind != domain.OperationReplace || !plan.Operations[0].RequiresApproval {
		t.Fatalf("approval-bound resume plan = %#v", plan.Operations)
	}
	operation := plan.Operations[0]

	tests := []struct {
		name            string
		journalEvents   []domain.JournalEvent
		inspection      domain.GuildInspection
		wantReason      string
		wantConsumes    int
		wantJournalOpen int
	}{
		{
			name: "stale precondition",
			journalEvents: []domain.JournalEvent{
				{RunID: "run", Type: domain.JournalRunStarted, OccurredAt: time.Unix(2, 0)},
				{RunID: "run", Type: domain.JournalStepStarted, OperationID: operation.ID, Resource: &operation.Resource, State: domain.StepStarted, OccurredAt: time.Unix(2, 0)},
			},
			inspection: func() domain.GuildInspection {
				changed := inspection
				changedName := "changed"
				changed.Channels = []domain.Channel{{ID: "2", GuildID: &guildID, Name: &changedName, Type: 0}}
				return changed
			}(),
			wantReason: "started operation precondition changed",
		},
		{
			name: "journal conflict",
			journalEvents: []domain.JournalEvent{
				{RunID: "run", Type: domain.JournalRunStarted, OccurredAt: time.Unix(2, 0)},
				{RunID: "run", Type: domain.JournalStepStarted, OperationID: "unknown-operation", State: domain.StepStarted, OccurredAt: time.Unix(2, 0)},
			},
			inspection: inspection,
			wantReason: "journal references unknown operation",
		},
		{
			name: "valid resume",
			journalEvents: []domain.JournalEvent{
				{RunID: "run", Type: domain.JournalRunStarted, OccurredAt: time.Unix(2, 0)},
				{RunID: "run", Type: domain.JournalStepComplete, OperationID: operation.ID, Resource: &operation.Resource, State: domain.StepCompleted, RemoteID: binding.RemoteID, Fingerprint: operation.PayloadFingerprint, OccurredAt: time.Unix(2, 0)},
			},
			inspection:      inspection,
			wantConsumes:    1,
			wantJournalOpen: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			approval := &boundReconciliationApproval{id: "resume-approval", actor: actorID, guild: guildID, planHash: plan.Hash, expiresAt: time.Unix(4, 0).Add(time.Minute)}
			state := newReconciliationState()
			state.bindings = []domain.ResourceBinding{binding}
			state.plans["run"] = plan
			state.journals["run"] = &memoryJournal{events: append([]domain.JournalEvent(nil), test.journalEvents...)}
			before := captureDurableReconciliationState(state)
			port := &scriptedPort{results: []DiscordPortResult{{Value: test.inspection, Attempts: 1}}}
			secured, err := NewSecuredDiscordService(port, NewInMemoryIdempotencyStore(), NewInMemoryResourceOwnershipStore(), nil, SecurityDependencies{Policy: allowTestPolicy{}, Approvals: approval, Audit: &memoryTestAudit{}, DefaultInvocation: Invocation{Principal: principal, RequestedMode: domain.ModeApply, CorrelationID: "default"}})
			if err != nil {
				t.Fatal(err)
			}
			service, err := NewReconciliationService(secured, state, reconciliationClock{now: time.Unix(4, 0)}, reconciliationIDs{value: "unused"}, nil, ReconciliationLimits{MaxOperations: 10, MaxDestructiveOperations: 1})
			if err != nil {
				t.Fatal(err)
			}
			result, resumeErr := service.Resume(context.Background(), ResumeRequest{GuildID: guildID, RunID: "run", PlanHash: plan.Hash, ActorID: actorID, Principal: principal, ApprovalID: approval.id, AllowDelete: true, CorrelationID: "resume"})
			if test.wantReason != "" {
				var conflict *domain.ConflictError
				if !errors.As(resumeErr, &conflict) || conflict.Reason != test.wantReason {
					t.Fatalf("error = %v, want conflict %q", resumeErr, test.wantReason)
				}
				if !reflect.DeepEqual(captureDurableReconciliationState(state), before) {
					t.Fatalf("durable state changed after pre-authorization conflict")
				}
			} else if resumeErr != nil || result.State != domain.RunCompleted {
				t.Fatalf("result=%#v error=%v", result, resumeErr)
			}
			if approval.planConsumes != test.wantConsumes {
				t.Fatalf("approval consumes = %d, want %d", approval.planConsumes, test.wantConsumes)
			}
			if state.journalOpenCalls != test.wantJournalOpen {
				t.Fatalf("journal opens = %d, want %d", state.journalOpenCalls, test.wantJournalOpen)
			}
			if port.count() != 1 {
				t.Fatalf("transport calls = %d, want snapshot only", port.count())
			}
		})
	}
}

func TestResumeReconcilesCrashAfterEveryCreateWithoutRepeatingIt(t *testing.T) {
	guildID := domain.GuildID("1")
	principal := domain.Principal{Adapter: "test", Subject: "actor", AuthenticationMethod: "in-process"}
	baseSnapshot, err := reconcile.NormalizeSnapshot(reconcile.SnapshotInput{Guild: domain.Guild{ID: guildID}, CapturedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	categoryRef := domain.StableResourceRef{Kind: domain.ResourceCategory, LogicalID: "general"}
	desired := domain.DesiredGuild{SchemaVersion: domain.DeclarativeSchemaV1, GuildID: guildID, Ownership: domain.OwnershipMetadata{Owner: "team"}, Categories: []domain.DesiredCategory{{LogicalID: "general", Name: "General", Position: 0}}, Channels: []domain.DesiredChannel{{LogicalID: "chat", Name: "chat", Type: 0, Category: &categoryRef, Position: 0, Overwrites: []domain.DesiredOverwrite{}}}, Roles: []domain.DesiredRole{}, Webhooks: []domain.DesiredWebhook{}, ApplicationCommands: []domain.DesiredApplicationCommand{}}
	plan, err := reconcile.BuildPlan(reconcile.PlanRequest{Desired: desired, Snapshot: baseSnapshot, ActorID: "actor", CreatedAt: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 2 {
		t.Fatalf("operations=%#v", plan.Operations)
	}
	categoryOperation, channelOperation := plan.Operations[0], plan.Operations[1]
	if categoryOperation.Resource.Kind != domain.ResourceCategory {
		categoryOperation, channelOperation = channelOperation, categoryOperation
	}

	for crashAfter := 0; crashAfter < 2; crashAfter++ {
		t.Run(fmt.Sprintf("step_%d", crashAfter+1), func(t *testing.T) {
			state := newReconciliationState()
			state.plans["run"] = plan
			categoryName, channelName := "General", "chat"
			category := domain.Channel{ID: "10", GuildID: &guildID, Type: 4, Name: &categoryName, Position: 0}
			inspection := domain.GuildInspection{Guild: domain.Guild{ID: guildID}, Channels: []domain.Channel{category}, Roles: []domain.Role{}, Webhooks: []domain.Webhook{}, ApplicationCommands: []domain.ApplicationCommand{}}
			journal := &memoryJournal{events: []domain.JournalEvent{{RunID: "run", Type: domain.JournalRunStarted, OccurredAt: time.Unix(3, 0)}}}
			categoryResource := categoryOperation.Resource
			channelResource := channelOperation.Resource
			if crashAfter == 0 {
				journal.events = append(journal.events, domain.JournalEvent{RunID: "run", Type: domain.JournalStepStarted, OperationID: categoryOperation.ID, Resource: &categoryResource, State: domain.StepStarted})
			} else {
				channel := domain.Channel{ID: "20", GuildID: &guildID, Type: 0, Name: &channelName, ParentID: func() *domain.ChannelID { value := domain.ChannelID("10"); return &value }(), Position: 0}
				inspection.Channels = append(inspection.Channels, channel)
				state.bindings = []domain.ResourceBinding{{Ref: categoryResource, RemoteID: "10", Fingerprint: categoryOperation.PayloadFingerprint}}
				journal.events = append(journal.events,
					domain.JournalEvent{RunID: "run", Type: domain.JournalStepStarted, OperationID: categoryOperation.ID, Resource: &categoryResource, State: domain.StepStarted},
					domain.JournalEvent{RunID: "run", Type: domain.JournalStepComplete, OperationID: categoryOperation.ID, Resource: &categoryResource, State: domain.StepCompleted, RemoteID: "10", Fingerprint: categoryOperation.PayloadFingerprint},
					domain.JournalEvent{RunID: "run", Type: domain.JournalStepStarted, OperationID: channelOperation.ID, Resource: &channelResource, State: domain.StepStarted},
				)
			}
			state.journals["run"] = journal
			results := []DiscordPortResult{{Value: inspection, Attempts: 1}}
			if crashAfter == 0 {
				createdChannel := domain.Channel{ID: "20", GuildID: &guildID, Type: 0, Name: &channelName}
				results = append(results, DiscordPortResult{Value: []domain.Channel{category}, Attempts: 1}, DiscordPortResult{Value: createdChannel, Attempts: 1})
			}
			port := &scriptedPort{results: results}
			secured, _ := securedServiceWithPolicy(port, allowTestPolicy{}, domain.ModeApply)
			service, err := NewReconciliationService(secured, state, reconciliationClock{time.Unix(4, 0)}, reconciliationIDs{"unused"}, nil, ReconciliationLimits{MaxOperations: 10, MaxDestructiveOperations: 1})
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Resume(context.Background(), ResumeRequest{GuildID: guildID, RunID: "run", PlanHash: plan.Hash, ActorID: "actor", Principal: principal, CorrelationID: "resume"})
			if err != nil {
				t.Fatal(err)
			}
			if result.State != domain.RunCompleted || len(state.bindings) != 2 {
				t.Fatalf("result=%#v bindings=%#v", result, state.bindings)
			}
			expectedCalls := 3
			if crashAfter == 1 {
				expectedCalls = 1
			}
			if port.count() != expectedCalls {
				t.Fatalf("Discord calls=%d, want %d; completed create was repeated", port.count(), expectedCalls)
			}
		})
	}
}

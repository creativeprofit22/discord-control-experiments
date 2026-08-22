package application

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/ken-kai/discord-control-experiments/internal/reconcile"
)

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

package application_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/approval"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/ken-kai/discord-control-experiments/internal/reconcile"
	"github.com/ken-kai/discord-control-experiments/internal/state"
)

func TestResumeWithRealStoresPreservesStaleStateConsumesOnceAndRepairsCrashTail(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	guildID := domain.GuildID("1")
	runID := domain.RunID("resume-run")
	actorID := domain.ActorID("actor")
	principal := domain.Principal{Adapter: "test", Subject: "actor", AuthenticationMethod: "in-process"}

	paths, err := state.ResolvePaths(t.TempDir(), "state")
	if err != nil {
		t.Fatal(err)
	}
	stateStore := state.NewStore(paths)
	plan, binding, inspection := resumeIntegrationPlan(t, guildID, actorID)
	if err := stateStore.SavePlan(guildID, runID, plan); err != nil {
		t.Fatal(err)
	}
	if err := stateStore.SaveBindings(guildID, []domain.ResourceBinding{binding}); err != nil {
		t.Fatal(err)
	}
	journal, _, err := stateStore.OpenJournal(guildID, runID, plan.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Append(domain.JournalEvent{RunID: runID, Type: domain.JournalRunStarted, OccurredAt: now.Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	resource := plan.Operations[0].Resource
	if _, err := journal.Append(domain.JournalEvent{RunID: runID, Type: domain.JournalStepComplete, OperationID: plan.Operations[0].ID, Resource: &resource, State: domain.StepCompleted, RemoteID: binding.RemoteID, Fingerprint: binding.Fingerprint, OccurredAt: now.Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	journalPath, err := paths.JournalPath(guildID, runID)
	if err != nil {
		t.Fatal(err)
	}
	appendCrashTail(t, journalPath, []byte(`{"run_id":`))

	approvalStore, err := approval.NewStoreWithDependencies(time.Hour, func() time.Time { return now }, bytes.NewReader(bytes.Repeat([]byte{7}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	grant, err := approvalStore.Issue(ctx, domain.ApprovalGrant{ActorID: actorID, GuildID: guildID, ScopeKind: domain.ApprovalScopeReconciliationPlan, ScopeHash: string(plan.Hash), ApproverID: actorID, IssuedAt: now, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}

	secured, err := application.NewSecuredDiscordService(resumeDiscordPort{inspection: inspection}, application.NewInMemoryIdempotencyStore(), application.NewInMemoryResourceOwnershipStore(), nil, application.SecurityDependencies{
		Policy:            resumeAllowPolicy{},
		Approvals:         approvalStore,
		Audit:             resumeAuditSink{},
		DefaultInvocation: application.Invocation{Principal: principal, RequestedMode: domain.ModeApply, CorrelationID: "default"},
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := application.NewReconciliationService(secured, stateStore, resumeClock{now: now}, resumeIDs{}, nil, application.ReconciliationLimits{MaxOperations: 10, MaxDestructiveOperations: 10})
	if err != nil {
		t.Fatal(err)
	}

	planPath := filepath.Join(mustGuildDirectory(t, paths, guildID), "plan-"+string(runID)+".json")
	bindingsPath, err := paths.BindingsPath(guildID)
	if err != nil {
		t.Fatal(err)
	}
	before := readResumeFiles(t, planPath, journalPath, bindingsPath)
	_, err = service.Resume(ctx, application.ResumeRequest{GuildID: guildID, RunID: runID, PlanHash: "stale-plan-hash", ActorID: actorID, Principal: principal, ApprovalID: grant.ID, CorrelationID: "stale-resume"})
	if err == nil {
		t.Fatal("stale resume accepted")
	}
	afterStale := readResumeFiles(t, planPath, journalPath, bindingsPath)
	if !reflect.DeepEqual(afterStale, before) {
		t.Fatalf("stale resume changed durable state\nbefore=%#v\nafter=%#v", before, afterStale)
	}

	result, err := service.Resume(ctx, application.ResumeRequest{GuildID: guildID, RunID: runID, PlanHash: plan.Hash, ActorID: actorID, Principal: principal, ApprovalID: grant.ID, CorrelationID: "valid-resume"})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != domain.RunCompleted {
		t.Fatalf("resume state = %s, want %s", result.State, domain.RunCompleted)
	}
	if _, err := approvalStore.ConsumeReconciliationPlan(ctx, grant.ID, actorID, guildID, string(plan.Hash), now); !errors.Is(err, approval.ErrConsumed) {
		t.Fatalf("second approval consumption error = %v, want %v", err, approval.ErrConsumed)
	}

	loaded, err := stateStore.LoadJournalEvents(guildID, runID, plan.Hash)
	if err != nil {
		t.Fatal(err)
	}
	_, opened, err := stateStore.OpenJournal(guildID, runID, plan.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(opened, loaded) {
		t.Fatalf("read-only and writable journal replay differ\nloaded=%#v\nopened=%#v", loaded, opened)
	}
	if len(loaded) != 3 || loaded[len(loaded)-1].Type != domain.JournalRunComplete {
		t.Fatalf("repaired journal events = %#v", loaded)
	}
	journalBytes, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(journalBytes, []byte{'\n'}) {
		t.Fatal("repaired journal is not newline-terminated")
	}
}

func resumeIntegrationPlan(t *testing.T, guildID domain.GuildID, actorID domain.ActorID) (domain.Plan, domain.ResourceBinding, domain.GuildInspection) {
	t.Helper()
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
		t.Fatalf("approval-bound plan = %#v", plan.Operations)
	}
	return plan, binding, inspection
}

type resumeDiscordPort struct{ inspection domain.GuildInspection }

func (p resumeDiscordPort) ExecuteDiscord(context.Context, domain.Command) (application.DiscordPortResult, error) {
	return application.DiscordPortResult{Value: p.inspection, Attempts: 1}, nil
}

type resumeAllowPolicy struct{}

func (resumeAllowPolicy) Decide(_ context.Context, request domain.PolicyRequest) (domain.PolicyDecision, error) {
	return domain.PolicyDecision{Allowed: true, ActorID: request.ActorID, EffectiveMode: domain.ModeApply, Reason: domain.PolicyAllowed}, nil
}

type resumeAuditSink struct{}

func (resumeAuditSink) Append(context.Context, domain.AuditEvent) error { return nil }

type resumeClock struct{ now time.Time }

func (c resumeClock) Now() time.Time { return c.now }

type resumeIDs struct{}

func (resumeIDs) NewCorrelationID() (domain.CorrelationID, error) { return "unused", nil }

func mustGuildDirectory(t *testing.T, paths state.Paths, guildID domain.GuildID) string {
	t.Helper()
	directory, err := paths.GuildDirectory(guildID)
	if err != nil {
		t.Fatal(err)
	}
	return directory
}

func readResumeFiles(t *testing.T, paths ...string) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = data
	}
	return files
}

func appendCrashTail(t *testing.T, path string, tail []byte) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(tail); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/ken-kai/discord-control-experiments/internal/reconcile"
)

type ReconciliationLimits struct {
	MaxOperations            int
	MaxDestructiveOperations int
}
type SnapshotRequest struct {
	GuildID       domain.GuildID
	ApplicationID domain.ApplicationID
	Principal     domain.Principal
	CorrelationID domain.CorrelationID
}
type ReconciliationPlanRequest struct {
	Desired       []byte
	ActorID       domain.ActorID
	Principal     domain.Principal
	CorrelationID domain.CorrelationID
}
type DriftRequest struct {
	Plan          domain.Plan
	ActorID       domain.ActorID
	Principal     domain.Principal
	CorrelationID domain.CorrelationID
}
type ResumeRequest struct {
	GuildID       domain.GuildID
	RunID         domain.RunID
	PlanHash      domain.PlanHash
	ActorID       domain.ActorID
	Principal     domain.Principal
	ApprovalID    domain.ApprovalID
	AllowDelete   bool
	CorrelationID domain.CorrelationID
}

type ReconciliationService struct {
	discord   *DiscordService
	state     ReconciliationState
	clock     Clock
	ids       CorrelationIDGenerator
	telemetry ReconciliationTelemetry
	limits    ReconciliationLimits
}

type noopReconciliationTelemetry struct{}

func (noopReconciliationTelemetry) ObserveReconciliation(context.Context, ReconciliationObservation) {
}

func NewReconciliationService(discord *DiscordService, state ReconciliationState, clock Clock, ids CorrelationIDGenerator, telemetry ReconciliationTelemetry, limits ReconciliationLimits) (*ReconciliationService, error) {
	if discord == nil || state == nil || clock == nil || ids == nil {
		return nil, errors.New("reconciliation service dependencies are required")
	}
	if limits.MaxOperations <= 0 || limits.MaxDestructiveOperations < 0 || limits.MaxDestructiveOperations > limits.MaxOperations {
		return nil, errors.New("invalid reconciliation operation limits")
	}
	if telemetry == nil {
		telemetry = noopReconciliationTelemetry{}
	}
	return &ReconciliationService{discord: discord, state: state, clock: clock, ids: ids, telemetry: telemetry, limits: limits}, nil
}

func (s *ReconciliationService) Snapshot(ctx context.Context, request SnapshotRequest) (domain.GuildSnapshot, error) {
	if err := validateReconciliationIdentity(request.Principal, domain.ActorID(request.Principal.Subject), request.CorrelationID); err != nil {
		return domain.GuildSnapshot{}, err
	}
	if request.GuildID == "" {
		return domain.GuildSnapshot{}, &domain.ValidationError{Field: "guild_id", Rule: "required"}
	}
	invocation := Invocation{Principal: request.Principal, RequestedMode: domain.ModeReadOnly, CorrelationID: request.CorrelationID}
	var applicationID *domain.ApplicationID
	if request.ApplicationID != "" {
		applicationID = &request.ApplicationID
	}
	execution, err := ExecuteAs(ctx, s.discord, invocation, domain.InspectGuild{CommandContext: domain.CommandContext{GuildID: request.GuildID}, ApplicationID: applicationID})
	if err != nil {
		return domain.GuildSnapshot{}, err
	}
	inspection := execution.Result
	if inspection.Guild.ID != request.GuildID {
		return domain.GuildSnapshot{}, &domain.OwnershipError{Resource: "guild", Expected: request.GuildID, Actual: inspection.Guild.ID}
	}
	bindings, err := s.state.LoadBindings(request.GuildID)
	if err != nil {
		return domain.GuildSnapshot{}, err
	}
	support := domain.SupportUnknown
	if inspection.Onboarding != nil {
		support = domain.SupportAvailable
	} else if inspection.OnboardingUnsupported {
		support = domain.SupportUnavailable
	}
	snapshot, err := reconcile.NormalizeSnapshot(reconcile.SnapshotInput{Guild: inspection.Guild, ApplicationID: request.ApplicationID, Channels: inspection.Channels, Roles: inspection.Roles, Webhooks: inspection.Webhooks, ApplicationCommands: inspection.ApplicationCommands, Onboarding: inspection.Onboarding, OnboardingSupport: support, Bindings: bindings, CapturedAt: s.clock.Now()})
	if err != nil {
		return domain.GuildSnapshot{}, err
	}
	s.telemetry.ObserveReconciliation(ctx, ReconciliationObservation{CorrelationID: request.CorrelationID, Outcome: "snapshot"})
	return snapshot, nil
}

func (s *ReconciliationService) Plan(ctx context.Context, request ReconciliationPlanRequest) (domain.Plan, error) {
	if err := validateReconciliationIdentity(request.Principal, request.ActorID, request.CorrelationID); err != nil {
		return domain.Plan{}, err
	}
	desired, err := reconcile.ParseDesired(request.Desired)
	if err != nil {
		return domain.Plan{}, err
	}
	snapshot, err := s.Snapshot(ctx, SnapshotRequest{GuildID: desired.GuildID, ApplicationID: desired.ApplicationID, Principal: request.Principal, CorrelationID: request.CorrelationID})
	if err != nil {
		return domain.Plan{}, err
	}
	plan, err := reconcile.BuildPlan(reconcile.PlanRequest{Desired: desired, Snapshot: snapshot, ActorID: request.ActorID, CreatedAt: s.clock.Now()})
	if err != nil {
		return domain.Plan{}, err
	}
	if err := s.enforceCaps(plan); err != nil {
		return domain.Plan{}, err
	}
	risks := map[domain.RiskClass]int{}
	for _, operation := range plan.Operations {
		risks[operation.Risk]++
	}
	s.telemetry.ObserveReconciliation(ctx, ReconciliationObservation{CorrelationID: request.CorrelationID, PlanHashPrefix: prefix(string(plan.Hash)), Outcome: "planned", OperationCount: len(plan.Operations), RiskCounts: risks, UnmanagedConflicts: len(plan.Conflicts)})
	return plan, nil
}

func (s *ReconciliationService) Drift(ctx context.Context, request DriftRequest) (domain.DriftReport, error) {
	if err := validateReconciliationIdentity(request.Principal, request.ActorID, request.CorrelationID); err != nil {
		return domain.DriftReport{}, err
	}
	if err := reconcile.ValidatePlan(request.Plan); err != nil && !errors.Is(err, domain.ErrPlanConflict) {
		return domain.DriftReport{}, err
	}
	if request.Plan.Binding.ActorID != request.ActorID {
		return domain.DriftReport{}, &domain.OwnershipError{Resource: "plan_actor"}
	}
	snapshot, err := s.Snapshot(ctx, SnapshotRequest{GuildID: request.Plan.Binding.GuildID, ApplicationID: request.Plan.Binding.ApplicationID, Principal: request.Principal, CorrelationID: request.CorrelationID})
	if err != nil {
		return domain.DriftReport{}, err
	}
	report := domain.DriftReport{PlanHash: request.Plan.Hash, SnapshotHash: snapshot.Metadata.CanonicalHash, ManagedDrift: []domain.PlanConflict{}, UnmanagedConflicts: append([]domain.PlanConflict(nil), request.Plan.Conflicts...)}
	report.Stale = snapshot.Metadata.CanonicalHash != request.Plan.Binding.SnapshotHash
	if report.Stale {
		report.ManagedDrift = append(report.ManagedDrift, domain.PlanConflict{Kind: domain.ConflictStalePrecondition, Resource: domain.StableResourceRef{Kind: domain.ResourceGuild, LogicalID: "guild"}, Message: "live snapshot no longer matches the plan precondition"})
	}
	s.telemetry.ObserveReconciliation(ctx, ReconciliationObservation{CorrelationID: request.CorrelationID, PlanHashPrefix: prefix(string(request.Plan.Hash)), Outcome: "drift", ManagedDrift: len(report.ManagedDrift), UnmanagedConflicts: len(report.UnmanagedConflicts)})
	return report, nil
}

func (s *ReconciliationService) Apply(ctx context.Context, request domain.ApplyRequest) (domain.ApplyResult, error) {
	if err := validateReconciliationIdentity(request.Principal, request.ActorID, request.CorrelationID); err != nil {
		return domain.ApplyResult{}, err
	}
	if request.PlanHash == "" || request.Plan.Hash != request.PlanHash {
		return domain.ApplyResult{}, &domain.StalePlanError{Reason: "caller-supplied plan hash does not match"}
	}
	encoded, err := reconcile.RenderPlanJSON(request.Plan)
	if err != nil {
		return domain.ApplyResult{}, err
	}
	plan, err := reconcile.ParsePlan(encoded)
	if err != nil {
		return domain.ApplyResult{}, err
	}
	if plan.Binding.ActorID != request.ActorID || plan.Binding.GuildID == "" {
		return domain.ApplyResult{}, &domain.OwnershipError{Resource: "plan_binding", Expected: plan.Binding.GuildID}
	}
	if err := s.enforceCaps(plan); err != nil {
		return domain.ApplyResult{}, err
	}
	if hasDelete(plan) && (!plan.Binding.AllowDesiredDeletes || !request.AllowDelete) {
		return domain.ApplyResult{}, &domain.UnsafeOperationError{Reason: "deletion requires desired-state and runtime opt-in"}
	}
	lock, err := s.state.AcquireGuildLock(plan.Binding.GuildID)
	if err != nil {
		return domain.ApplyResult{}, err
	}
	defer lock.Close()
	snapshot, err := s.Snapshot(ctx, SnapshotRequest{GuildID: plan.Binding.GuildID, ApplicationID: plan.Binding.ApplicationID, Principal: request.Principal, CorrelationID: request.CorrelationID})
	if err != nil {
		return domain.ApplyResult{}, err
	}
	if snapshot.Metadata.CanonicalHash != plan.Binding.SnapshotHash {
		return domain.ApplyResult{}, &domain.StalePlanError{Reason: "live snapshot precondition changed"}
	}
	generatedRunID, err := s.ids.NewCorrelationID()
	if err != nil || generatedRunID == "" {
		return domain.ApplyResult{}, errors.New("generate reconciliation run ID")
	}
	runID := domain.RunID(generatedRunID)
	if err := s.state.SavePlan(plan.Binding.GuildID, runID, plan); err != nil {
		return domain.ApplyResult{}, err
	}
	journal, events, err := s.state.OpenJournal(plan.Binding.GuildID, runID, plan.Hash)
	if err != nil {
		return domain.ApplyResult{}, err
	}
	if len(events) != 0 {
		return domain.ApplyResult{}, fmt.Errorf("new apply run journal is not empty")
	}
	startedAt := s.clock.Now()
	if _, err := journal.Append(domain.JournalEvent{RunID: runID, Type: domain.JournalRunStarted, OccurredAt: startedAt}); err != nil {
		return domain.ApplyResult{}, err
	}
	result := domain.ApplyResult{RunID: runID, PlanHash: plan.Hash, State: domain.RunApplying, ResumeStatus: domain.ResumeNotNeeded, Steps: []domain.ApplyStepResult{}, StartedAt: startedAt}
	bindings := bindingMap(snapshot.Bindings)
	invocation := Invocation{Principal: request.Principal, ActorID: request.ActorID, RequestedMode: domain.ModeApply, CorrelationID: request.CorrelationID, ApprovalID: request.ApprovalID}
	for _, operation := range plan.Operations {
		step, stepErr := s.runOperation(ctx, journal, runID, plan, operation, bindings, invocation)
		result.Steps = append(result.Steps, step)
		if stepErr != nil {
			result.State, result.FinishedAt = domain.RunPartial, s.clock.Now()
			_, _ = journal.Append(domain.JournalEvent{RunID: runID, Type: domain.JournalRunFailed, OccurredAt: result.FinishedAt, ErrorCode: "step_failed"})
			s.telemetry.ObserveReconciliation(ctx, ReconciliationObservation{CorrelationID: request.CorrelationID, PlanHashPrefix: prefix(string(plan.Hash)), RunID: runID, Outcome: "partial", Partial: true})
			return result, stepErr
		}
	}
	result.State, result.FinishedAt = domain.RunCompleted, s.clock.Now()
	if _, err := journal.Append(domain.JournalEvent{RunID: runID, Type: domain.JournalRunComplete, OccurredAt: result.FinishedAt}); err != nil {
		result.State = domain.RunPartial
		return result, err
	}
	s.telemetry.ObserveReconciliation(ctx, ReconciliationObservation{CorrelationID: request.CorrelationID, PlanHashPrefix: prefix(string(plan.Hash)), RunID: runID, Outcome: "completed", OperationCount: len(plan.Operations)})
	return result, nil
}

func (s *ReconciliationService) Resume(ctx context.Context, request ResumeRequest) (domain.ApplyResult, error) {
	if err := validateReconciliationIdentity(request.Principal, request.ActorID, request.CorrelationID); err != nil {
		return domain.ApplyResult{}, err
	}
	lock, err := s.state.AcquireGuildLock(request.GuildID)
	if err != nil {
		return domain.ApplyResult{}, err
	}
	defer lock.Close()
	plan, err := s.state.LoadPlan(request.GuildID, request.RunID)
	if err != nil {
		return domain.ApplyResult{}, err
	}
	if plan.Hash != request.PlanHash || plan.Binding.GuildID != request.GuildID || plan.Binding.ActorID != request.ActorID {
		return domain.ApplyResult{}, &domain.StalePlanError{Reason: "resume binding does not match saved plan"}
	}
	if err := reconcile.ValidatePlan(plan); err != nil {
		return domain.ApplyResult{}, err
	}
	if hasDelete(plan) && (!plan.Binding.AllowDesiredDeletes || !request.AllowDelete) {
		return domain.ApplyResult{}, &domain.UnsafeOperationError{Reason: "deletion requires desired-state and runtime opt-in"}
	}
	journal, events, err := s.state.OpenJournal(request.GuildID, request.RunID, plan.Hash)
	if err != nil {
		return domain.ApplyResult{}, err
	}
	completed, started := replayStepStates(events)
	bindings, err := s.state.LoadBindings(request.GuildID)
	if err != nil {
		return domain.ApplyResult{}, err
	}
	bindingByRef := bindingMap(bindings)
	for _, event := range events {
		if event.Type == domain.JournalStepComplete && event.Resource != nil && event.RemoteID != "" {
			bindingByRef[*event.Resource] = domain.ResourceBinding{Ref: *event.Resource, RemoteID: event.RemoteID, Fingerprint: event.Fingerprint}
		}
	}
	if err := s.saveBindingMap(request.GuildID, bindingByRef); err != nil {
		return domain.ApplyResult{}, err
	}
	snapshot, err := s.Snapshot(ctx, SnapshotRequest{GuildID: request.GuildID, ApplicationID: plan.Binding.ApplicationID, Principal: request.Principal, CorrelationID: request.CorrelationID})
	if err != nil {
		return domain.ApplyResult{}, err
	}
	for operationID := range started {
		if completed[operationID] {
			continue
		}
		operation := findOperation(plan.Operations, operationID)
		if operation == nil {
			return domain.ApplyResult{}, &domain.ConflictError{Reason: "journal references unknown operation"}
		}
		if operation.Kind == domain.OperationCreate {
			match, conflict := reconcileUncertainCreate(*operation, snapshot)
			if conflict != nil {
				return domain.ApplyResult{}, conflict
			}
			bindingByRef[operation.Resource] = *match
			if _, err := journal.Append(domain.JournalEvent{RunID: request.RunID, Type: domain.JournalStepComplete, OperationID: operation.ID, Resource: &operation.Resource, State: domain.StepCompleted, RemoteID: match.RemoteID, Fingerprint: match.Fingerprint, OccurredAt: s.clock.Now()}); err != nil {
				return domain.ApplyResult{}, err
			}
			completed[operation.ID] = true
			continue
		}
		if operation.Kind == domain.OperationDelete && operation.Preconditions.ResourceRemoteID != "" && !resourcePresent(operation.Preconditions.ResourceRemoteID, snapshot) {
			delete(bindingByRef, operation.Resource)
			if _, err := journal.Append(domain.JournalEvent{RunID: request.RunID, Type: domain.JournalStepComplete, OperationID: operation.ID, Resource: &operation.Resource, State: domain.StepCompleted, RemoteID: operation.Preconditions.ResourceRemoteID, Fingerprint: operation.PayloadFingerprint, OccurredAt: s.clock.Now()}); err != nil {
				return domain.ApplyResult{}, err
			}
			completed[operation.ID] = true
			continue
		}
		if !operationPreconditionHolds(*operation, snapshot) {
			return domain.ApplyResult{}, &domain.ConflictError{Reason: "started operation precondition changed", Conflicts: []domain.PlanConflict{{Kind: domain.ConflictStalePrecondition, Resource: operation.Resource, Message: "resource changed after an uncertain operation"}}}
		}
	}
	if err := s.saveBindingMap(request.GuildID, bindingByRef); err != nil {
		return domain.ApplyResult{}, err
	}
	result := domain.ApplyResult{RunID: request.RunID, PlanHash: plan.Hash, State: domain.RunApplying, ResumeStatus: domain.ResumeContinued, Steps: []domain.ApplyStepResult{}, StartedAt: firstRunTime(events)}
	invocation := Invocation{Principal: request.Principal, ActorID: request.ActorID, RequestedMode: domain.ModeApply, CorrelationID: request.CorrelationID, ApprovalID: request.ApprovalID}
	for _, operation := range plan.Operations {
		if completed[operation.ID] {
			result.Steps = append(result.Steps, domain.ApplyStepResult{OperationID: operation.ID, Resource: operation.Resource, State: domain.StepSkipped})
			continue
		}
		if !dependenciesComplete(operation, completed) {
			return result, &domain.ConflictError{Reason: "operation dependency is incomplete"}
		}
		step, stepErr := s.runOperation(ctx, journal, request.RunID, plan, operation, bindingByRef, invocation)
		result.Steps = append(result.Steps, step)
		if stepErr != nil {
			result.State, result.FinishedAt = domain.RunPartial, s.clock.Now()
			return result, stepErr
		}
		completed[operation.ID] = true
	}
	result.State, result.FinishedAt = domain.RunCompleted, s.clock.Now()
	if _, err := journal.Append(domain.JournalEvent{RunID: request.RunID, Type: domain.JournalRunComplete, OccurredAt: result.FinishedAt}); err != nil {
		result.State = domain.RunPartial
		return result, err
	}
	s.telemetry.ObserveReconciliation(ctx, ReconciliationObservation{CorrelationID: request.CorrelationID, PlanHashPrefix: prefix(string(plan.Hash)), RunID: request.RunID, Outcome: "resumed", Resumed: true})
	return result, nil
}

func validateReconciliationIdentity(principal domain.Principal, actor domain.ActorID, correlation domain.CorrelationID) error {
	if principal.Adapter == "" || principal.Subject == "" || principal.AuthenticationMethod == "" {
		return &domain.ValidationError{Field: "principal", Rule: "authenticated principal required"}
	}
	if actor == "" {
		return &domain.OwnershipError{Resource: "actor"}
	}
	if correlation == "" {
		return &domain.ValidationError{Field: "correlation_id", Rule: "required"}
	}
	return nil
}
func (s *ReconciliationService) enforceCaps(plan domain.Plan) error {
	if len(plan.Operations) > s.limits.MaxOperations {
		return &domain.UnsafeOperationError{Reason: "plan operation cap exceeded"}
	}
	destructive := 0
	for _, operation := range plan.Operations {
		if reconcile.IsDestructiveRisk(operation.Risk) {
			destructive++
		}
	}
	if destructive > s.limits.MaxDestructiveOperations {
		return &domain.UnsafeOperationError{Reason: "plan destructive-operation cap exceeded"}
	}
	return nil
}
func hasDelete(plan domain.Plan) bool {
	for _, operation := range plan.Operations {
		if operation.Kind == domain.OperationDelete || operation.Kind == domain.OperationReplace {
			return true
		}
	}
	return false
}
func prefix(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

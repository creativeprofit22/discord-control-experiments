package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type DiscordService struct {
	port              DiscordPort
	idempotency       IdempotencyStore
	ownership         ResourceOwnershipStore
	telemetry         DiscordTelemetry
	policy            PolicyEvaluator
	approvals         ApprovalStore
	audit             AuditSink
	securityTelemetry SecurityTelemetry
	defaultInvocation Invocation
	now               func() time.Time
}

type noopDiscordTelemetry struct{}

func (noopDiscordTelemetry) ObserveDiscordCommand(ctx context.Context, _ *DiscordObservation, run func(context.Context) error) error {
	return run(ctx)
}

type SecurityDependencies struct {
	Policy            PolicyEvaluator
	Approvals         ApprovalStore
	Audit             AuditSink
	Telemetry         SecurityTelemetry
	DefaultInvocation Invocation
}

type denyPolicy struct{}

func (denyPolicy) Decide(context.Context, domain.PolicyRequest) (domain.PolicyDecision, error) {
	return domain.PolicyDecision{Reason: domain.PolicyUnknownPrincipal}, nil
}

type unavailableAudit struct{}

func (unavailableAudit) Append(context.Context, domain.AuditEvent) error {
	return errors.New("audit unavailable")
}

type noopSecurityTelemetry struct{}

func (noopSecurityTelemetry) RecordSecurityEvent(context.Context, domain.SecurityEvent) {}

func (noopSecurityTelemetry) ObserveApprovalLatency(context.Context, time.Duration) {}

func NewDiscordService(port DiscordPort, idempotency IdempotencyStore, ownership ResourceOwnershipStore, telemetry DiscordTelemetry) (*DiscordService, error) {
	return NewSecuredDiscordService(port, idempotency, ownership, telemetry, SecurityDependencies{Policy: denyPolicy{}, Audit: unavailableAudit{}})
}

func NewSecuredDiscordService(port DiscordPort, idempotency IdempotencyStore, ownership ResourceOwnershipStore, telemetry DiscordTelemetry, security SecurityDependencies) (*DiscordService, error) {
	if port == nil || idempotency == nil || ownership == nil || security.Policy == nil || security.Audit == nil {
		return nil, errors.New("discord service dependencies are required")
	}
	if telemetry == nil {
		telemetry = noopDiscordTelemetry{}
	}
	if security.Telemetry == nil {
		security.Telemetry = noopSecurityTelemetry{}
	}
	return &DiscordService{port: port, idempotency: idempotency, ownership: ownership, telemetry: telemetry, policy: security.Policy, approvals: security.Approvals, audit: security.Audit, securityTelemetry: security.Telemetry, defaultInvocation: security.DefaultInvocation, now: time.Now}, nil
}

func Execute[R any](ctx context.Context, service *DiscordService, command domain.DiscordCommand[R]) (domain.Execution[R], error) {
	return ExecuteAs(ctx, service, service.defaultInvocation, command)
}

func ExecuteAs[R any](ctx context.Context, service *DiscordService, invocation Invocation, command domain.DiscordCommand[R]) (domain.Execution[R], error) {
	var zero domain.Execution[R]
	execution, err := service.dispatchDiscord(ctx, invocation, command)
	if err != nil {
		return domain.Execution[R]{Plan: execution.Plan, Attempts: execution.Attempts, Replay: execution.Replay, Partial: execution.Partial}, err
	}
	if execution.Plan != nil {
		return domain.Execution[R]{Plan: execution.Plan}, nil
	}
	value, ok := execution.Result.(R)
	if !ok {
		return zero, &domain.InvariantError{Detail: "transport returned wrong result type"}
	}
	return domain.Execution[R]{Result: value, Attempts: execution.Attempts, Replay: execution.Replay, Partial: execution.Partial}, nil
}

type untypedExecution struct {
	Result   any
	Plan     *domain.OperationPlan
	Attempts int
	Replay   domain.ReplayState
	Partial  *domain.PartialResult
}

func (s *DiscordService) execute(ctx context.Context, command domain.Command) (untypedExecution, error) {
	return s.dispatchDiscord(ctx, s.defaultInvocation, command)
}

func (s *DiscordService) dispatchDiscord(ctx context.Context, invocation Invocation, command domain.Command) (untypedExecution, error) {
	if err := validateCommand(command); err != nil {
		return untypedExecution{}, err
	}
	spec, ok := LookupCommandSpec(command.CommandName())
	if !ok || spec.ResourceExtractor == nil || spec.IntentSanitizer == nil {
		return untypedExecution{}, &domain.AuthorizationError{Reason: domain.PolicyUnknownCommand}
	}
	if !invocation.valid() {
		return untypedExecution{}, s.auditDenial(ctx, invocation, command, spec, domain.PolicyMalformedPrincipal, "invalid_invocation")
	}
	if ownershipErr := s.enforceOwnership(command, spec); ownershipErr != nil {
		s.securityTelemetry.RecordSecurityEvent(ctx, domain.SecurityEvent{Kind: "cross_guild", Reason: domain.PolicyResourceDenied, Mode: invocation.RequestedMode, DestructiveClass: spec.DestructiveClass, CorrelationID: invocation.CorrelationID, OccurredAt: s.now()})
		auditErr := s.auditDenial(ctx, invocation, command, spec, domain.PolicyResourceDenied, "ownership_denied")
		var unavailable *domain.AuditError
		if errors.As(auditErr, &unavailable) {
			return untypedExecution{}, auditErr
		}
		return untypedExecution{}, ownershipErr
	}
	resources := spec.ResourceExtractor(command)
	destructiveClass := DestructiveClassForCommand(command, spec)
	request := domain.PolicyRequest{Principal: invocation.Principal, ActorID: invocation.ActorID, GuildID: command.GuildScope(), Operation: domain.OperationName(spec.Name), Capability: spec.Capability, Resources: resources, Mode: invocation.RequestedMode, DestructiveClass: destructiveClass, CorrelationID: invocation.CorrelationID}
	decision, policyErr := s.evaluatePolicy(ctx, request)
	if policyErr != nil || !decision.Allowed {
		reason := decision.Reason
		if policyErr != nil || reason == "" {
			reason = domain.PolicyError
		}
		if reason == domain.PolicyError {
			s.securityTelemetry.RecordSecurityEvent(ctx, domain.SecurityEvent{Kind: "policy_error", Reason: reason, Mode: invocation.RequestedMode, DestructiveClass: destructiveClass, CorrelationID: invocation.CorrelationID, OccurredAt: s.now()})
		}
		return untypedExecution{}, s.auditDenial(ctx, invocation, command, spec, reason, "policy_denied")
	}
	request.ActorID = decision.ActorID
	request.Mode = decision.EffectiveMode
	operationHash, err := operationHash(request, command)
	if err != nil {
		return untypedExecution{}, &domain.InvariantError{Detail: "operation hash failed"}
	}
	if decision.EffectiveMode == domain.ModeReadOnly && spec.MutationExposed {
		return untypedExecution{}, s.auditDenial(ctx, invocation, command, spec, domain.PolicyModeDenied, "read_only_mutation")
	}
	var approval domain.ApprovalGrant
	if decision.EffectiveMode == domain.ModeApply && domain.RequiresApproval(destructiveClass) {
		if s.approvals == nil || invocation.ApprovalID == "" {
			s.securityTelemetry.RecordSecurityEvent(ctx, domain.SecurityEvent{Kind: "approval_bypass", Reason: domain.PolicyApprovalRequired, Mode: decision.EffectiveMode, DestructiveClass: destructiveClass, CorrelationID: invocation.CorrelationID, OccurredAt: s.now()})
			return untypedExecution{}, s.auditDenial(ctx, invocation, command, spec, domain.PolicyApprovalRequired, "approval_missing")
		}
		approval, err = s.approvals.Consume(ctx, invocation.ApprovalID, decision.ActorID, operationHash, s.now())
		if err != nil {
			s.securityTelemetry.RecordSecurityEvent(ctx, domain.SecurityEvent{Kind: "approval_invalid", Reason: domain.PolicyApprovalInvalid, Mode: decision.EffectiveMode, DestructiveClass: destructiveClass, CorrelationID: invocation.CorrelationID, OccurredAt: s.now()})
			return untypedExecution{}, s.auditDenial(ctx, invocation, command, spec, domain.PolicyApprovalInvalid, "approval_invalid")
		}
		s.securityTelemetry.ObserveApprovalLatency(ctx, s.now().Sub(approval.IssuedAt))
	}
	precommit := s.auditEvent(invocation, command, spec, decision, approval, operationHash, "decision", "pending")
	if err := s.audit.Append(ctx, precommit); err != nil {
		s.securityTelemetry.RecordSecurityEvent(ctx, domain.SecurityEvent{Kind: "audit_precommit_failure", Reason: domain.PolicyAuditUnavailable, Mode: decision.EffectiveMode, DestructiveClass: destructiveClass, CorrelationID: invocation.CorrelationID, OccurredAt: s.now()})
		return untypedExecution{}, &domain.AuditError{Stage: "precommit"}
	}
	if decision.EffectiveMode == domain.ModeApply && domain.RequiresApproval(destructiveClass) {
		s.securityTelemetry.RecordSecurityEvent(ctx, domain.SecurityEvent{Kind: "destructive_apply", Reason: domain.PolicyAllowed, Mode: decision.EffectiveMode, DestructiveClass: destructiveClass, CorrelationID: invocation.CorrelationID, OccurredAt: s.now()})
	}
	complete := func(result string) error {
		if err := s.audit.Append(context.WithoutCancel(ctx), s.auditEvent(invocation, command, spec, decision, approval, operationHash, "completion", result)); err != nil {
			s.securityTelemetry.RecordSecurityEvent(ctx, domain.SecurityEvent{Kind: "audit_completion_failure", Reason: domain.PolicyAuditUnavailable, Mode: decision.EffectiveMode, DestructiveClass: destructiveClass, CorrelationID: invocation.CorrelationID, OccurredAt: s.now()})
			return &domain.AuditError{Stage: "completion"}
		}
		return nil
	}
	if decision.EffectiveMode == domain.ModeDryRun {
		plan := &domain.OperationPlan{Operation: domain.OperationName(spec.Name), Capability: spec.Capability, Mode: decision.EffectiveMode, DestructiveClass: destructiveClass, ResourceKinds: resourceKinds(resources), Intent: spec.IntentSanitizer(command), OperationHash: operationHash}
		if err := complete("dry_run"); err != nil {
			return untypedExecution{Plan: plan}, err
		}
		return untypedExecution{Plan: plan}, nil
	}
	contextData := commandContext(command)
	fingerprint, err := commandFingerprint(command)
	if err != nil {
		return untypedExecution{}, &domain.InvariantError{Detail: "fingerprint failed"}
	}
	var record IdempotencyRecord
	var recordKey string
	var leader bool
	if spec.Idempotency == IdempotencyRequired {
		recordKey = string(command.GuildScope()) + ":" + command.CommandName() + ":" + string(contextData.IdempotencyKey)
		record, leader, err = s.claim(ctx, recordKey, fingerprint, contextData.ReconciliationKey)
		if err != nil {
			return untypedExecution{}, err
		}
		if !leader {
			switch record.State {
			case IdempotencySucceeded:
				if err := complete("replayed"); err != nil {
					return untypedExecution{Result: record.Result, Attempts: record.Attempts, Replay: domain.ReplaySucceeded}, err
				}
				return untypedExecution{Result: record.Result, Attempts: record.Attempts, Replay: domain.ReplaySucceeded}, nil
			case IdempotencyUncertain:
				return s.reconcile(ctx, invocation, command, recordKey, record)
			}
		}
		if leader && len(record.Baseline) == 0 {
			baseline, baselineAttempts, baselineErr := s.captureBaseline(ctx, invocation, command)
			if baselineErr != nil {
				return untypedExecution{Attempts: baselineAttempts}, baselineErr
			}
			next := record
			next.Baseline = baseline
			swapped, swapErr := s.idempotency.CompareAndSwap(ctx, recordKey, record.Version, next)
			if swapErr != nil || !swapped {
				return untypedExecution{}, &domain.InvariantError{Detail: "persist create baseline"}
			}
			next.Version = record.Version + 1
			record = next
		}
	}
	var portResult DiscordPortResult
	observation := &DiscordObservation{CommandName: command.CommandName(), RouteTemplate: spec.RouteTemplate, Method: spec.Method, ResultClass: "pending"}
	runErr := s.telemetry.ObserveDiscordCommand(ctx, observation, func(callCtx context.Context) error {
		var callErr error
		portResult, callErr = s.port.ExecuteDiscord(callCtx, command)
		observation.Attempts = portResult.Attempts
		observation.StatusClass = portResult.StatusClass
		if callErr != nil {
			observation.ResultClass = "error"
		} else {
			observation.ResultClass = "ok"
		}
		if portResult.Uncertain {
			observation.Partial = partialForFailure(command, portResult)
		}
		return callErr
	})
	if runErr != nil {
		if ctx.Err() != nil {
			runErr = &domain.CancellationError{Err: ctx.Err()}
		}
		partial := partialForFailure(command, portResult)
		if spec.Idempotency == IdempotencyRequired {
			state := IdempotencyFailed
			if portResult.Uncertain || portResult.Sent {
				state = IdempotencyUncertain
			}
			next := record
			next.State = state
			next.Attempts = portResult.Attempts
			next.Partial = partial
			_, _ = s.idempotency.CompareAndSwap(context.WithoutCancel(ctx), recordKey, record.Version, next)
		}
		if auditErr := complete("error"); auditErr != nil {
			return untypedExecution{Attempts: portResult.Attempts, Partial: partial}, auditErr
		}
		return untypedExecution{Attempts: portResult.Attempts, Partial: partial}, runErr
	}
	if err := validateDiscordResultOwnership(command, portResult.Value); err != nil {
		if auditErr := complete("error"); auditErr != nil {
			return untypedExecution{Attempts: portResult.Attempts}, auditErr
		}
		return untypedExecution{Attempts: portResult.Attempts, Partial: partialForFailure(command, portResult)}, err
	}
	s.recordOwnership(command, portResult.Value)
	if spec.Idempotency == IdempotencyRequired {
		next := record
		next.State = IdempotencySucceeded
		next.Result = portResult.Value
		next.Attempts = portResult.Attempts
		_, _ = s.idempotency.CompareAndSwap(ctx, recordKey, record.Version, next)
	}
	if err := complete("success"); err != nil {
		return untypedExecution{Result: portResult.Value, Attempts: portResult.Attempts}, err
	}
	return untypedExecution{Result: portResult.Value, Attempts: portResult.Attempts, Replay: domain.ReplayNone}, nil
}

func (s *DiscordService) claim(ctx context.Context, key, fingerprint string, reconciliation domain.ReconciliationKey) (IdempotencyRecord, bool, error) {
	for {
		record, ok, err := s.idempotency.Load(ctx, key)
		if err != nil {
			return record, false, err
		}
		if !ok {
			record = IdempotencyRecord{Namespace: key, Fingerprint: fingerprint, ReconciliationKey: reconciliation, State: IdempotencyPending, LeaseUntil: s.now().Add(30 * time.Second)}
			created, err := s.idempotency.Create(ctx, record)
			if err != nil {
				return record, false, err
			}
			if created {
				record.Version = 1
				return record, true, nil
			}
			continue
		}
		if record.Fingerprint != fingerprint || record.ReconciliationKey != reconciliation {
			return record, false, &domain.IdempotencyConflictError{}
		}
		if record.State != IdempotencyPending {
			return record, false, nil
		}
		if s.now().After(record.LeaseUntil) {
			next := record
			next.LeaseUntil = s.now().Add(30 * time.Second)
			swapped, err := s.idempotency.CompareAndSwap(ctx, key, record.Version, next)
			if err != nil {
				return record, false, err
			}
			if swapped {
				next.Version = record.Version + 1
				return next, true, nil
			}
			continue
		}
		record, err = s.idempotency.Wait(ctx, key, record.Version)
		if err != nil {
			return record, false, err
		}
	}
}
func (s *DiscordService) evaluatePolicy(ctx context.Context, request domain.PolicyRequest) (decision domain.PolicyDecision, err error) {
	defer func() {
		if recover() != nil {
			decision = domain.PolicyDecision{Reason: domain.PolicyError}
			err = errors.New("policy evaluator panic")
		}
	}()
	return s.policy.Decide(ctx, request)
}

func (s *DiscordService) auditDenial(ctx context.Context, invocation Invocation, command domain.Command, spec CommandSpec, reason domain.PolicyReason, result string) error {
	s.securityTelemetry.RecordSecurityEvent(ctx, domain.SecurityEvent{Kind: "policy_denial", Reason: reason, Mode: invocation.RequestedMode, DestructiveClass: spec.DestructiveClass, CorrelationID: invocation.CorrelationID, OccurredAt: s.now()})
	decision := domain.PolicyDecision{Reason: reason, EffectiveMode: invocation.RequestedMode}
	event := s.auditEvent(invocation, command, spec, decision, domain.ApprovalGrant{}, "", "decision", result)
	if err := s.audit.Append(context.WithoutCancel(ctx), event); err != nil {
		s.securityTelemetry.RecordSecurityEvent(ctx, domain.SecurityEvent{Kind: "audit_precommit_failure", Reason: domain.PolicyAuditUnavailable, Mode: invocation.RequestedMode, DestructiveClass: spec.DestructiveClass, CorrelationID: invocation.CorrelationID, OccurredAt: s.now()})
		return &domain.AuditError{Stage: "denial"}
	}
	return &domain.AuthorizationError{Reason: reason}
}

func (s *DiscordService) auditEvent(invocation Invocation, command domain.Command, spec CommandSpec, decision domain.PolicyDecision, approval domain.ApprovalGrant, hash, kind, result string) domain.AuditEvent {
	reason := ""
	if contextData := commandContext(command); contextData.AuditReason != nil {
		reason = *contextData.AuditReason
	}
	return domain.AuditEvent{Kind: domain.AuditEventKind(kind), Timestamp: s.now(), ActorID: decision.ActorID, ApproverID: approval.ApproverID, Adapter: invocation.Principal.Adapter, GuildID: command.GuildScope(), Operation: domain.OperationName(spec.Name), Capability: spec.Capability, ResourceKinds: resourceKinds(spec.ResourceExtractor(command)), Intent: spec.IntentSanitizer(command), Decision: decision.Allowed, Reason: decision.Reason, ApprovalID: approval.ID, Mode: decision.EffectiveMode, DestructiveClass: DestructiveClassForCommand(command, spec), Result: result, CorrelationID: invocation.CorrelationID, ParentCorrelationID: invocation.ParentCorrelationID, AuditReason: reason, OperationHash: hash}
}

func operationHash(request domain.PolicyRequest, command domain.Command) (string, error) {
	fingerprint, err := commandFingerprint(command)
	if err != nil {
		return "", err
	}
	canonical := struct {
		Actor       domain.ActorID          `json:"actor"`
		Adapter     domain.Adapter          `json:"adapter"`
		Guild       domain.GuildID          `json:"guild"`
		Operation   domain.OperationName    `json:"operation"`
		Capability  domain.Capability       `json:"capability"`
		Resources   []domain.ResourceRef    `json:"resources"`
		Mode        domain.ExecutionMode    `json:"mode"`
		Destructive domain.DestructiveClass `json:"destructive"`
		Fingerprint string                  `json:"fingerprint"`
	}{request.ActorID, request.Principal.Adapter, request.GuildID, request.Operation, request.Capability, request.Resources, request.Mode, request.DestructiveClass, fingerprint}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func resourceKinds(resources []domain.ResourceRef) []domain.ResourceKind {
	seen := map[domain.ResourceKind]bool{}
	for _, resource := range resources {
		seen[resource.Kind] = true
	}
	result := make([]domain.ResourceKind, 0, len(seen))
	for kind := range seen {
		result = append(result, kind)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func internalInvocation(parent Invocation, stage, operation string) Invocation {
	sum := sha256.Sum256([]byte(string(parent.CorrelationID) + ":" + stage + ":" + operation))
	return Invocation{Principal: parent.Principal, ActorID: parent.ActorID, RequestedMode: parent.RequestedMode, CorrelationID: domain.CorrelationID("internal-" + hex.EncodeToString(sum[:8])), ParentCorrelationID: parent.CorrelationID}
}

func commandFingerprint(command domain.Command) (string, error) {
	encoded, err := json.Marshal(command)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
func partialForFailure(command domain.Command, result DiscordPortResult) *domain.PartialResult {
	contextData := commandContext(command)
	outcome := domain.PartialNotSent
	if result.Sent {
		outcome = domain.PartialResponseReceived
	}
	if result.Uncertain {
		outcome = domain.PartialSentUnknown
	}
	return &domain.PartialResult{Outcome: outcome, IdempotencyKey: contextData.IdempotencyKey, ReconciliationKey: contextData.ReconciliationKey, Recoverable: result.Uncertain}
}

func (s *DiscordService) enforceOwnership(command domain.Command, spec CommandSpec) error {
	for _, ref := range spec.ResourceExtractor(command) {
		if ref.Kind == domain.ResourceGuild || ref.Kind == domain.ResourceApplication || ref.ID == "" {
			continue
		}
		actual, known := s.ownership.Guild(ref.Kind, ref.ID)
		if known && actual != command.GuildScope() {
			return &domain.OwnershipError{Resource: string(ref.Kind), Expected: command.GuildScope(), Actual: actual}
		}
		if !known && spec.Ownership != OwnershipGuildQualified {
			return &domain.OwnershipError{Resource: string(ref.Kind), Expected: command.GuildScope()}
		}
	}
	return nil
}

func validateDiscordResultOwnership(command domain.Command, value any) error {
	expectedGuild := command.GuildScope()
	checkGuild := func(actual *domain.GuildID, resource string) error {
		if actual != nil && *actual != expectedGuild {
			return &domain.OwnershipError{Resource: resource, Expected: expectedGuild, Actual: *actual}
		}
		return nil
	}
	checkChannel := func(value domain.Channel) error { return checkGuild(value.GuildID, "channel") }
	checkWebhook := func(value domain.Webhook) error { return checkGuild(value.GuildID, "webhook") }
	checkCommand := func(value domain.ApplicationCommand) error {
		if err := checkGuild(value.GuildID, "application_command"); err != nil {
			return err
		}
		var expectedApplication domain.ApplicationID
		switch typed := command.(type) {
		case domain.InspectGuild:
			if typed.ApplicationID != nil {
				expectedApplication = *typed.ApplicationID
			}
		case domain.ListGuildApplicationCommands:
			expectedApplication = typed.ApplicationID
		case domain.GetGuildApplicationCommand:
			expectedApplication = typed.ApplicationID
		case domain.CreateGuildApplicationCommand:
			expectedApplication = typed.ApplicationID
		case domain.ModifyGuildApplicationCommand:
			expectedApplication = typed.ApplicationID
		case domain.BulkOverwriteGuildApplicationCommands:
			expectedApplication = typed.ApplicationID
		}
		if expectedApplication != "" && value.ApplicationID != expectedApplication {
			return &domain.OwnershipError{Resource: "application_command", Expected: expectedGuild}
		}
		return nil
	}
	switch result := value.(type) {
	case domain.Guild:
		if result.ID != expectedGuild {
			return &domain.OwnershipError{Resource: "guild", Expected: expectedGuild, Actual: result.ID}
		}
	case domain.GuildInspection:
		if result.Guild.ID != expectedGuild {
			return &domain.OwnershipError{Resource: "guild", Expected: expectedGuild, Actual: result.Guild.ID}
		}
		for _, item := range result.Channels {
			if err := checkChannel(item); err != nil {
				return err
			}
		}
		for _, item := range result.Webhooks {
			if err := checkWebhook(item); err != nil {
				return err
			}
		}
		for _, item := range result.ApplicationCommands {
			if err := checkCommand(item); err != nil {
				return err
			}
		}
		if result.Onboarding != nil && result.Onboarding.GuildID != expectedGuild {
			return &domain.OwnershipError{Resource: "onboarding", Expected: expectedGuild, Actual: result.Onboarding.GuildID}
		}
	case domain.Channel:
		return checkChannel(result)
	case []domain.Channel:
		for _, item := range result {
			if err := checkChannel(item); err != nil {
				return err
			}
		}
	case domain.Webhook:
		return checkWebhook(result)
	case []domain.Webhook:
		for _, item := range result {
			if err := checkWebhook(item); err != nil {
				return err
			}
		}
	case domain.ApplicationCommand:
		return checkCommand(result)
	case []domain.ApplicationCommand:
		for _, item := range result {
			if err := checkCommand(item); err != nil {
				return err
			}
		}
	case domain.Onboarding:
		if result.GuildID != expectedGuild {
			return &domain.OwnershipError{Resource: "onboarding", Expected: expectedGuild, Actual: result.GuildID}
		}
	case domain.AutoModerationRule:
		if result.GuildID != expectedGuild {
			return &domain.OwnershipError{Resource: "auto_moderation_rule", Expected: expectedGuild, Actual: result.GuildID}
		}
	case []domain.AutoModerationRule:
		for _, rule := range result {
			if rule.GuildID != expectedGuild {
				return &domain.OwnershipError{Resource: "auto_moderation_rule", Expected: expectedGuild, Actual: rule.GuildID}
			}
		}
	}
	return nil
}

func (s *DiscordService) recordOwnership(command domain.Command, value any) {
	guild := command.GuildScope()
	recordChannel := func(c domain.Channel) { s.ownership.Record(ResourceChannel, string(c.ID), guild) }
	recordRole := func(r domain.Role) { s.ownership.Record(ResourceRole, string(r.ID), guild) }
	recordWebhook := func(w domain.Webhook) { s.ownership.Record(ResourceWebhook, string(w.ID), guild) }
	recordInvite := func(i domain.Invite) { s.ownership.Record(ResourceInvite, string(i.Code), guild) }
	recordEvent := func(e domain.ScheduledEvent) { s.ownership.Record(ResourceScheduledEvent, string(e.ID), guild) }
	switch result := value.(type) {
	case domain.GuildInspection:
		for _, x := range result.Channels {
			recordChannel(x)
		}
		for _, x := range result.Roles {
			recordRole(x)
		}
		for _, x := range result.Webhooks {
			recordWebhook(x)
		}
		for _, x := range result.Invites {
			recordInvite(x)
		}
		for _, x := range result.ScheduledEvents {
			recordEvent(x)
		}
	case domain.Channel:
		recordChannel(result)
	case []domain.Channel:
		for _, x := range result {
			recordChannel(x)
		}
	case domain.Role:
		recordRole(result)
	case []domain.Role:
		for _, x := range result {
			recordRole(x)
		}
	case domain.Webhook:
		recordWebhook(result)
	case []domain.Webhook:
		for _, x := range result {
			recordWebhook(x)
		}
	case domain.Invite:
		recordInvite(result)
	case []domain.Invite:
		for _, x := range result {
			recordInvite(x)
		}
	case domain.ScheduledEvent:
		recordEvent(result)
	case []domain.ScheduledEvent:
		for _, x := range result {
			recordEvent(x)
		}
	case domain.Message:
		s.ownership.Record(ResourceMessage, string(result.ID), guild)
	case []domain.Message:
		for _, x := range result {
			s.ownership.Record(ResourceMessage, string(x.ID), guild)
		}
	case domain.ApplicationCommand:
		s.ownership.Record(ResourceApplicationCommand, string(result.ID), guild)
	case []domain.ApplicationCommand:
		for _, x := range result {
			s.ownership.Record(ResourceApplicationCommand, string(x.ID), guild)
		}
	}
	s.removeDeleted(command)
}
func (s *DiscordService) removeDeleted(command domain.Command) {
	switch c := command.(type) {
	case domain.DeleteChannel:
		s.ownership.Remove(ResourceChannel, string(c.ChannelID))
	case domain.DeleteRole:
		s.ownership.Remove(ResourceRole, string(c.RoleID))
	case domain.DeleteWebhook:
		s.ownership.Remove(ResourceWebhook, string(c.WebhookID))
	case domain.DeleteInvite:
		s.ownership.Remove(ResourceInvite, string(c.Code))
	case domain.DeleteScheduledEvent:
		s.ownership.Remove(ResourceScheduledEvent, string(c.ScheduledEventID))
	}
}

func (s *DiscordService) captureBaseline(ctx context.Context, invocation Invocation, command domain.Command) ([]domain.Snowflake, int, error) {
	base := domain.CommandContext{GuildID: command.GuildScope()}
	var discovery domain.Command
	switch c := command.(type) {
	case domain.CreateChannel:
		discovery = domain.ListGuildChannels{CommandContext: base}
	case domain.CreateRole:
		discovery = domain.ListGuildRoles{CommandContext: base}
	case domain.CreateWebhook:
		discovery = domain.ListChannelWebhooks{CommandContext: base, ChannelID: c.ChannelID}
	case domain.CreateGuildApplicationCommand:
		discovery = domain.ListGuildApplicationCommands{CommandContext: base, ApplicationID: c.ApplicationID}
	case domain.CreateInvite:
		discovery = domain.ListChannelInvites{CommandContext: base, ChannelID: c.ChannelID}
	case domain.CreateScheduledEvent:
		discovery = domain.ListScheduledEvents{CommandContext: base}
	case domain.CreateMessage:
		limit := 100
		discovery = domain.ListMessages{CommandContext: base, ChannelID: c.ChannelID, Limit: &limit}
	default:
		return nil, 0, nil
	}
	child := internalInvocation(invocation, "baseline", discovery.CommandName())
	result, err := s.dispatchDiscord(ctx, child, discovery)
	if err != nil {
		return nil, result.Attempts, err
	}
	s.recordOwnership(discovery, result.Result)
	return resourceIDs(result.Result), result.Attempts, nil
}

func (s *DiscordService) reconcile(ctx context.Context, invocation Invocation, command domain.Command, recordKey string, record IdempotencyRecord) (untypedExecution, error) {
	base := domain.CommandContext{GuildID: command.GuildScope()}
	var discovery domain.Command
	switch c := command.(type) {
	case domain.CreateChannel:
		discovery = domain.ListGuildChannels{CommandContext: base}
	case domain.CreateRole:
		discovery = domain.ListGuildRoles{CommandContext: base}
	case domain.CreateWebhook:
		discovery = domain.ListChannelWebhooks{CommandContext: base, ChannelID: c.ChannelID}
	case domain.CreateGuildApplicationCommand:
		discovery = domain.ListGuildApplicationCommands{CommandContext: base, ApplicationID: c.ApplicationID}
	case domain.CreateInvite:
		discovery = domain.ListChannelInvites{CommandContext: base, ChannelID: c.ChannelID}
	case domain.CreateScheduledEvent:
		discovery = domain.ListScheduledEvents{CommandContext: base}
	case domain.CreateMessage:
		limit := 100
		discovery = domain.ListMessages{CommandContext: base, ChannelID: c.ChannelID, Limit: &limit}
	default:
		return untypedExecution{Attempts: record.Attempts, Replay: domain.ReplayCoalesced, Partial: record.Partial}, &domain.RetryExhaustedError{Attempts: record.Attempts, Reason: "no reconciliation strategy"}
	}
	child := internalInvocation(invocation, "reconcile", discovery.CommandName())
	current, err := s.dispatchDiscord(ctx, child, discovery)
	attempts := current.Attempts
	if err != nil {
		return untypedExecution{Attempts: record.Attempts + attempts, Replay: domain.ReplayCoalesced, Partial: record.Partial}, err
	}
	matches := reconciliationMatches(command, current.Result, record.Baseline)
	total := record.Attempts + attempts
	if len(matches) == 1 {
		next := record
		next.State = IdempotencySucceeded
		next.Result = matches[0]
		next.Attempts = total
		next.Partial = nil
		swapped, swapErr := s.idempotency.CompareAndSwap(ctx, recordKey, record.Version, next)
		if swapErr != nil || !swapped {
			return untypedExecution{}, &domain.InvariantError{Detail: "complete reconciliation"}
		}
		s.recordOwnership(command, matches[0])
		return untypedExecution{Result: matches[0], Attempts: total, Replay: domain.ReplayCoalesced}, nil
	}
	partial := record.Partial
	if partial == nil {
		contextData := commandContext(command)
		partial = &domain.PartialResult{Outcome: domain.PartialSentUnknown, IdempotencyKey: contextData.IdempotencyKey, ReconciliationKey: contextData.ReconciliationKey, Recoverable: true}
	}
	if len(matches) > 1 {
		partial.Outcome = domain.PartialReconcileAmbiguous
		partial.Recoverable = true
		partial.CompletedResourceIDs = resourceIDs(matches)
		next := record
		next.Partial = partial
		next.Attempts = total
		_, _ = s.idempotency.CompareAndSwap(ctx, recordKey, record.Version, next)
		return untypedExecution{Attempts: total, Replay: domain.ReplayCoalesced, Partial: partial}, &domain.ReconciliationAmbiguityError{Matches: len(matches)}
	}
	return untypedExecution{Attempts: total, Replay: domain.ReplayCoalesced, Partial: partial}, &domain.RetryExhaustedError{Attempts: total, Reason: "reconciliation found no match"}
}

func resourceIDs(value any) []domain.Snowflake {
	var ids []domain.Snowflake
	switch v := value.(type) {
	case []domain.Channel:
		for _, x := range v {
			ids = append(ids, domain.Snowflake(x.ID))
		}
	case []domain.Role:
		for _, x := range v {
			ids = append(ids, domain.Snowflake(x.ID))
		}
	case []domain.Webhook:
		for _, x := range v {
			ids = append(ids, domain.Snowflake(x.ID))
		}
	case []domain.ApplicationCommand:
		for _, x := range v {
			ids = append(ids, domain.Snowflake(x.ID))
		}
	case []domain.Invite:
		for _, x := range v {
			ids = append(ids, domain.Snowflake(x.Code))
		}
	case []domain.ScheduledEvent:
		for _, x := range v {
			ids = append(ids, domain.Snowflake(x.ID))
		}
	case []domain.Message:
		for _, x := range v {
			ids = append(ids, domain.Snowflake(x.ID))
		}
	case []any:
		for _, x := range v {
			ids = append(ids, resourceIDs(x)...)
		}
	case domain.Channel:
		ids = append(ids, domain.Snowflake(v.ID))
	case domain.Role:
		ids = append(ids, domain.Snowflake(v.ID))
	case domain.Webhook:
		ids = append(ids, domain.Snowflake(v.ID))
	case domain.ApplicationCommand:
		ids = append(ids, domain.Snowflake(v.ID))
	case domain.Invite:
		ids = append(ids, domain.Snowflake(v.Code))
	case domain.ScheduledEvent:
		ids = append(ids, domain.Snowflake(v.ID))
	case domain.Message:
		ids = append(ids, domain.Snowflake(v.ID))
	}
	return ids
}
func reconciliationMatches(command domain.Command, current any, baseline []domain.Snowflake) []any {
	old := map[domain.Snowflake]bool{}
	for _, id := range baseline {
		old[id] = true
	}
	var matches []any
	switch c := command.(type) {
	case domain.CreateChannel:
		for _, x := range current.([]domain.Channel) {
			if !old[domain.Snowflake(x.ID)] && x.Name != nil && *x.Name == c.Name && x.Type == c.Type && equalChannelID(x.ParentID, c.ParentID) {
				matches = append(matches, x)
			}
		}
	case domain.CreateRole:
		for _, x := range current.([]domain.Role) {
			if !old[domain.Snowflake(x.ID)] && x.Name == c.Name {
				matches = append(matches, x)
			}
		}
	case domain.CreateWebhook:
		for _, x := range current.([]domain.Webhook) {
			if !old[domain.Snowflake(x.ID)] && x.Name != nil && *x.Name == c.Name && x.ChannelID != nil && *x.ChannelID == c.ChannelID {
				matches = append(matches, x)
			}
		}
	case domain.CreateGuildApplicationCommand:
		for _, x := range current.([]domain.ApplicationCommand) {
			if !old[domain.Snowflake(x.ID)] && x.Name == c.Command.Name && (c.Command.Type == nil || x.Type == *c.Command.Type) {
				matches = append(matches, x)
			}
		}
	case domain.CreateInvite:
		for _, x := range current.([]domain.Invite) {
			if !old[domain.Snowflake(x.Code)] && x.Channel != nil && x.Channel.ID == c.ChannelID {
				matches = append(matches, x)
			}
		}
	case domain.CreateScheduledEvent:
		for _, x := range current.([]domain.ScheduledEvent) {
			if !old[domain.Snowflake(x.ID)] && x.Name == c.Name && x.EntityType == c.EntityType && x.ScheduledStartTime.Equal(c.ScheduledStartTime) {
				matches = append(matches, x)
			}
		}
	case domain.CreateMessage:
		nonce := deterministicApplicationNonce(c.CommandContext)
		for _, x := range current.([]domain.Message) {
			if !old[domain.Snowflake(x.ID)] && x.Nonce != nil && *x.Nonce == nonce {
				matches = append(matches, x)
			}
		}
	}
	return matches
}
func equalChannelID(a, b *domain.ChannelID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
func deterministicApplicationNonce(c domain.CommandContext) string {
	sum := sha256.Sum256([]byte(string(c.GuildID) + "\x00" + string(c.IdempotencyKey) + "\x00" + string(c.ReconciliationKey)))
	return hex.EncodeToString(sum[:16])
}

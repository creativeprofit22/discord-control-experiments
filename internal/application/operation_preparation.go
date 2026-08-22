package application

import (
	"context"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func (s *DiscordService) PrepareOperation(ctx context.Context, invocation Invocation, command domain.Command) (domain.OperationPlan, error) {
	if err := validateCommand(command); err != nil {
		return domain.OperationPlan{}, err
	}
	spec, ok := LookupCommandSpec(command.CommandName())
	if !ok || spec.ResourceExtractor == nil || spec.IntentSanitizer == nil {
		return domain.OperationPlan{}, &domain.AuthorizationError{Reason: domain.PolicyUnknownCommand}
	}
	if !invocation.valid() || invocation.RequestedMode != domain.ModeApply {
		return domain.OperationPlan{}, &domain.AuthorizationError{Reason: domain.PolicyModeDenied}
	}
	if err := s.enforceOwnership(command, spec); err != nil {
		return domain.OperationPlan{}, err
	}
	resources := spec.ResourceExtractor(command)
	destructive := DestructiveClassForCommand(command, spec)
	if !domain.RequiresApproval(destructive) {
		return domain.OperationPlan{}, &domain.ApprovalError{Reason: "operation is not destructive"}
	}
	request := domain.PolicyRequest{Principal: invocation.Principal, ActorID: invocation.ActorID, GuildID: command.GuildScope(), Operation: domain.OperationName(spec.Name), Capability: spec.Capability, Resources: resources, Mode: domain.ModeApply, DestructiveClass: destructive, CorrelationID: invocation.CorrelationID}
	decision, err := s.evaluatePolicy(ctx, request)
	if err != nil || !decision.Allowed || decision.EffectiveMode != domain.ModeApply {
		reason := decision.Reason
		if reason == "" {
			reason = domain.PolicyError
		}
		return domain.OperationPlan{}, s.auditDenial(ctx, invocation, command, spec, reason, "preparation_denied")
	}
	request.ActorID, request.Mode = decision.ActorID, decision.EffectiveMode
	hash, err := operationHash(request, command)
	if err != nil {
		return domain.OperationPlan{}, &domain.InvariantError{Detail: "operation hash failed"}
	}
	plan := domain.OperationPlan{Operation: request.Operation, Capability: request.Capability, Mode: request.Mode, DestructiveClass: destructive, ResourceKinds: resourceKinds(resources), Intent: spec.IntentSanitizer(command), OperationHash: hash, Preapproved: decision.Preapproved}
	if err := s.audit.Append(ctx, s.auditEvent(invocation, command, spec, decision, domain.ApprovalGrant{}, hash, "preparation", "planned")); err != nil {
		return domain.OperationPlan{}, &domain.AuditError{Stage: "preparation"}
	}
	return plan, nil
}

func (s *OperatorService) PrepareDestructive(ctx context.Context, request OperatorRequest) (domain.OperationPlan, error) {
	if s == nil || request.Command == nil || request.Handler != request.Command.CommandName() {
		return domain.OperationPlan{}, &domain.InvariantError{Detail: "destructive request mismatch"}
	}
	return s.discord.PrepareOperation(ctx, request.Invocation, request.Command)
}

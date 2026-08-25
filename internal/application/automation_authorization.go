package application

import (
	"context"
	"errors"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type AutomationOperatorAuthorizationRequest struct {
	Invocation       Invocation
	GuildID          domain.GuildID
	Capability       domain.Capability
	Mode             domain.ExecutionMode
	DestructiveClass domain.DestructiveClass
	Operation        domain.OperationName
}

type AutomationOperatorAuthorizer interface {
	AuthorizeAutomationOperator(context.Context, AutomationOperatorAuthorizationRequest) error
}

type PolicyAutomationOperatorAuthorizer struct {
	policy PolicyEvaluator
	audit  AuditSink
	clock  AutomationClock
}

func NewPolicyAutomationOperatorAuthorizer(policy PolicyEvaluator, audit AuditSink, clock AutomationClock) (*PolicyAutomationOperatorAuthorizer, error) {
	if policy == nil || audit == nil || clock == nil {
		return nil, errors.New("automation operator authorization dependencies are required")
	}
	return &PolicyAutomationOperatorAuthorizer{policy: policy, audit: audit, clock: clock}, nil
}

func (a *PolicyAutomationOperatorAuthorizer) AuthorizeAutomationOperator(ctx context.Context, request AutomationOperatorAuthorizationRequest) error {
	reason := domain.PolicyError
	if request.Invocation.valid() && request.GuildID != "" && request.Operation != "" && domain.ValidCapability(request.Capability) && domain.ValidExecutionMode(request.Mode) && domain.ValidDestructiveClass(request.DestructiveClass) {
		decision, err := a.policy.Decide(ctx, domain.PolicyRequest{
			Principal: request.Invocation.Principal, ActorID: request.Invocation.ActorID, GuildID: request.GuildID,
			Operation: request.Operation, Capability: request.Capability, Mode: request.Mode,
			DestructiveClass: request.DestructiveClass, CorrelationID: request.Invocation.CorrelationID,
		})
		if err == nil && decision.Allowed && decision.ActorID == request.Invocation.ActorID && decision.EffectiveMode == request.Mode {
			return nil
		}
		if err == nil && decision.Reason != "" {
			reason = decision.Reason
		} else if err == nil && decision.EffectiveMode != request.Mode {
			reason = domain.PolicyModeDenied
		}
	}
	event := domain.AuditEvent{
		Kind: "decision", Timestamp: a.clock.Now(), ActorID: request.Invocation.ActorID,
		Adapter: request.Invocation.Principal.Adapter, GuildID: request.GuildID, Operation: request.Operation,
		Capability: request.Capability, Decision: false, Reason: reason, Mode: request.Mode,
		DestructiveClass: request.DestructiveClass, Result: "automation_operator_denied",
		CorrelationID: request.Invocation.CorrelationID, ParentCorrelationID: request.Invocation.ParentCorrelationID,
	}
	if err := a.audit.Append(context.WithoutCancel(ctx), event); err != nil {
		return &domain.AuditError{Stage: "automation_operator_denial"}
	}
	return &domain.AuthorizationError{Reason: reason}
}

func AutomationActionAuthorization(action domain.AutomationAction) (domain.Capability, domain.DestructiveClass, error) {
	switch action.Kind {
	case domain.ActionMessage, domain.ActionForumThread, domain.ActionMediaThread, domain.ActionPoll, domain.ActionWelcome:
		return domain.CapabilityMessageManage, domain.DestructiveNone, nil
	case domain.ActionWebhook:
		return domain.CapabilityWebhookManage, domain.DestructiveNone, nil
	case domain.ActionRoleAdd:
		return domain.CapabilityMemberModerate, domain.DestructiveRoleEscalation, nil
	case domain.ActionRoleRemove:
		return domain.CapabilityMemberModerate, domain.DestructiveNone, nil
	case domain.ActionCleanup:
		return domain.CapabilityMessageManage, domain.DestructiveDelete, nil
	case domain.ActionSnapshot:
		return domain.CapabilityGuildInspect, domain.DestructiveNone, nil
	default:
		return "", "", &domain.AuthorizationError{Reason: domain.PolicyUnknownCommand}
	}
}

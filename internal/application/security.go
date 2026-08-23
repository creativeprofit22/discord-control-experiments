package application

import (
	"context"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

// Invocation carries authenticated adapter evidence separately from caller-controlled commands.
type Invocation struct {
	Principal           domain.Principal
	RequestedMode       domain.ExecutionMode
	CorrelationID       domain.CorrelationID
	ParentCorrelationID domain.CorrelationID
	ApprovalID          domain.ApprovalID
	ActorID             domain.ActorID
	reconciliation      *reconciliationAuthorization
	lockdown            *lockdownAuthorization
}

type lockdownAuthorization struct {
	actor        domain.ActorID
	guild        domain.GuildID
	planHash     domain.LockdownPlanHash
	operationIDs map[domain.LockdownOperationID]struct{}
}

func newLockdownAuthorization(actor domain.ActorID, guild domain.GuildID, planHash domain.LockdownPlanHash, operations []domain.LockdownOperation) *lockdownAuthorization {
	ids := make(map[domain.LockdownOperationID]struct{}, len(operations))
	for _, operation := range operations {
		ids[operation.ID] = struct{}{}
	}
	return &lockdownAuthorization{actor: actor, guild: guild, planHash: planHash, operationIDs: ids}
}

func (a *lockdownAuthorization) allows(actor domain.ActorID, command domain.Command) bool {
	if a == nil || actor != a.actor || command == nil || command.GuildScope() != a.guild {
		return false
	}
	contextData := commandContext(command)
	if domain.LockdownPlanHash(contextData.ReconciliationKey) != a.planHash {
		return false
	}
	_, ok := a.operationIDs[domain.LockdownOperationID(contextData.IdempotencyKey)]
	return ok
}

type reconciliationAuthorization struct {
	actor        domain.ActorID
	guild        domain.GuildID
	planHash     domain.PlanHash
	operationIDs map[domain.OperationID]struct{}
}

func newReconciliationAuthorization(actor domain.ActorID, guild domain.GuildID, planHash domain.PlanHash, operations []domain.PlanOperation) *reconciliationAuthorization {
	ids := make(map[domain.OperationID]struct{}, len(operations))
	for _, operation := range operations {
		ids[operation.ID] = struct{}{}
	}
	return &reconciliationAuthorization{actor: actor, guild: guild, planHash: planHash, operationIDs: ids}
}

func (a *reconciliationAuthorization) allows(actor domain.ActorID, command domain.Command) bool {
	if a == nil || actor != a.actor || command == nil || command.GuildScope() != a.guild {
		return false
	}
	contextData := commandContext(command)
	if domain.PlanHash(contextData.ReconciliationKey) != a.planHash {
		return false
	}
	_, ok := a.operationIDs[domain.OperationID(contextData.IdempotencyKey)]
	return ok
}

func (i Invocation) valid() bool {
	return i.Principal.Adapter != "" && i.Principal.Subject != "" && i.Principal.AuthenticationMethod != "" &&
		domain.ValidExecutionMode(i.RequestedMode) && i.CorrelationID != ""
}

type PolicyEvaluator interface {
	Decide(context.Context, domain.PolicyRequest) (domain.PolicyDecision, error)
}

type ApprovalStore interface {
	ConsumeOperation(context.Context, domain.ApprovalID, domain.ActorID, domain.GuildID, string, time.Time) (domain.ApprovalGrant, error)
	ConsumeReconciliationPlan(context.Context, domain.ApprovalID, domain.ActorID, domain.GuildID, string, time.Time) (domain.ApprovalGrant, error)
	ConsumeLockdownPlan(context.Context, domain.ApprovalID, domain.ActorID, domain.GuildID, string, time.Time) (domain.ApprovalGrant, error)
}

type ApprovalIssuer interface {
	Issue(context.Context, domain.ApprovalGrant) (domain.ApprovalGrant, error)
}

type AuditSink interface {
	Append(context.Context, domain.AuditEvent) error
}

type SecurityTelemetry interface {
	RecordSecurityEvent(context.Context, domain.SecurityEvent)
	ObserveApprovalLatency(context.Context, time.Duration)
}

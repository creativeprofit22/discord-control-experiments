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
}

func (i Invocation) valid() bool {
	return i.Principal.Adapter != "" && i.Principal.Subject != "" && i.Principal.AuthenticationMethod != "" &&
		domain.ValidExecutionMode(i.RequestedMode) && i.CorrelationID != ""
}

type PolicyEvaluator interface {
	Decide(context.Context, domain.PolicyRequest) (domain.PolicyDecision, error)
}

type ApprovalStore interface {
	Consume(context.Context, domain.ApprovalID, domain.ActorID, string, time.Time) (domain.ApprovalGrant, error)
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

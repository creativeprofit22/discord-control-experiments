package application

import (
	"context"
	"errors"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type ConfirmationSource string

const (
	ConfirmationInteractive       ConfirmationSource = "interactive"
	ConfirmationPolicyPreapproved ConfirmationSource = "policy_preapproved"
)

type ApprovalCoordinator struct {
	issuer ApprovalIssuer
	clock  Clock
	ids    CorrelationIDGenerator
	ttl    time.Duration
}

func NewApprovalCoordinator(issuer ApprovalIssuer, clock Clock, ids CorrelationIDGenerator, maximumTTL time.Duration) (*ApprovalCoordinator, error) {
	if issuer == nil || clock == nil || ids == nil || maximumTTL <= 0 {
		return nil, errors.New("approval coordinator dependencies are required")
	}
	ttl := 5 * time.Minute
	if maximumTTL < ttl {
		ttl = maximumTTL
	}
	return &ApprovalCoordinator{issuer: issuer, clock: clock, ids: ids, ttl: ttl}, nil
}

func (c *ApprovalCoordinator) Issue(ctx context.Context, invocation Invocation, plan domain.OperationPlan, source ConfirmationSource) (domain.ApprovalGrant, error) {
	if !domain.RequiresApproval(plan.DestructiveClass) || invocation.ActorID == "" || plan.OperationHash == "" {
		return domain.ApprovalGrant{}, &domain.ApprovalError{Reason: "operation is not eligible"}
	}
	if source == ConfirmationPolicyPreapproved && !plan.Preapproved {
		return domain.ApprovalGrant{}, &domain.AuthorizationError{Reason: domain.PolicyApprovalRequired}
	}
	if source != ConfirmationInteractive && source != ConfirmationPolicyPreapproved {
		return domain.ApprovalGrant{}, &domain.AuthorizationError{Reason: domain.PolicyApprovalRequired}
	}
	id, err := c.ids.NewCorrelationID()
	if err != nil || id == "" {
		return domain.ApprovalGrant{}, errors.New("generate approval reference")
	}
	now := c.clock.Now()
	grant := domain.ApprovalGrant{ID: domain.ApprovalID(id), ActorID: invocation.ActorID, OperationHash: plan.OperationHash, ApproverID: invocation.ActorID, IssuedAt: now, ExpiresAt: now.Add(c.ttl)}
	return c.issuer.Issue(ctx, grant)
}

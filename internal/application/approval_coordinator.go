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
	ConfirmationInteraction       ConfirmationSource = "interaction"
)

type ApprovalCoordinator struct {
	issuer ApprovalIssuer
	clock  Clock
	ttl    time.Duration
}

func NewApprovalCoordinator(issuer ApprovalIssuer, clock Clock, maximumTTL time.Duration) (*ApprovalCoordinator, error) {
	if issuer == nil || clock == nil || maximumTTL <= 0 {
		return nil, errors.New("approval coordinator dependencies are required")
	}
	ttl := 5 * time.Minute
	if maximumTTL < ttl {
		ttl = maximumTTL
	}
	return &ApprovalCoordinator{issuer: issuer, clock: clock, ttl: ttl}, nil
}

func (c *ApprovalCoordinator) Issue(ctx context.Context, invocation Invocation, plan domain.OperationPlan, source ConfirmationSource) (domain.ApprovalGrant, error) {
	if !domain.RequiresApproval(plan.DestructiveClass) || invocation.ActorID == "" || plan.GuildID == "" || plan.OperationHash == "" {
		return domain.ApprovalGrant{}, &domain.ApprovalError{Reason: "operation is not eligible"}
	}
	if source == ConfirmationPolicyPreapproved && !plan.Preapproved {
		return domain.ApprovalGrant{}, &domain.AuthorizationError{Reason: domain.PolicyApprovalRequired}
	}
	if source != ConfirmationInteractive && source != ConfirmationPolicyPreapproved {
		return domain.ApprovalGrant{}, &domain.AuthorizationError{Reason: domain.PolicyApprovalRequired}
	}
	now := c.clock.Now()
	grant := domain.ApprovalGrant{ActorID: invocation.ActorID, GuildID: plan.GuildID, ScopeKind: domain.ApprovalScopeOperation, ScopeHash: plan.OperationHash, ApproverID: invocation.ActorID, IssuedAt: now, ExpiresAt: now.Add(c.ttl)}
	return c.issuer.Issue(ctx, grant)
}

func (c *ApprovalCoordinator) IssueReconciliationPlan(ctx context.Context, actor domain.ActorID, guild domain.GuildID, planHash domain.PlanHash, source ConfirmationSource, expiresAt time.Time) (domain.ApprovalGrant, error) {
	return c.issuePlan(ctx, actor, guild, string(planHash), domain.ApprovalScopeReconciliationPlan, source, expiresAt)
}

func (c *ApprovalCoordinator) IssueLockdownPlan(ctx context.Context, actor domain.ActorID, guild domain.GuildID, planHash domain.LockdownPlanHash, source ConfirmationSource, expiresAt time.Time) (domain.ApprovalGrant, error) {
	return c.issuePlan(ctx, actor, guild, string(planHash), domain.ApprovalScopeLockdownPlan, source, expiresAt)
}

func (c *ApprovalCoordinator) issuePlan(ctx context.Context, actor domain.ActorID, guild domain.GuildID, planHash string, scope domain.ApprovalScopeKind, source ConfirmationSource, expiresAt time.Time) (domain.ApprovalGrant, error) {
	if actor == "" || guild == "" || len(planHash) != 64 || (source != ConfirmationInteractive && source != ConfirmationInteraction) {
		return domain.ApprovalGrant{}, &domain.ApprovalError{Reason: "plan is not eligible"}
	}
	now := c.clock.Now()
	maximum := now.Add(c.ttl)
	if expiresAt.IsZero() || expiresAt.After(maximum) {
		expiresAt = maximum
	}
	if !expiresAt.After(now) {
		return domain.ApprovalGrant{}, &domain.ApprovalError{Reason: "plan approval expired"}
	}
	return c.issuer.Issue(ctx, domain.ApprovalGrant{ActorID: actor, GuildID: guild, ScopeKind: scope, ScopeHash: planHash, ApproverID: actor, IssuedAt: now, ExpiresAt: expiresAt})
}

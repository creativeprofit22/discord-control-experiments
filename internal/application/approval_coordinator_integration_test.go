package application_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/approval"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type approvalCoordinatorClock struct {
	now time.Time
}

func (c approvalCoordinatorClock) Now() time.Time { return c.now }

func TestApprovalCoordinatorIssuesOperationAndPlanGrantsThroughStore(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0).UTC()
	random := make([]byte, 64)
	for index := range random {
		random[index] = byte(index + 1)
	}
	store, err := approval.NewStoreWithDependencies(10*time.Minute, func() time.Time { return now }, bytes.NewReader(random))
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := application.NewApprovalCoordinator(store, approvalCoordinatorClock{now: now}, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	actor := domain.ActorID("operator")
	guild := domain.GuildID("123456789012345678")
	operationHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	operationGrant, err := coordinator.Issue(ctx, application.Invocation{ActorID: actor}, domain.OperationPlan{
		GuildID:          guild,
		DestructiveClass: domain.DestructiveDelete,
		OperationHash:    operationHash,
	}, application.ConfirmationInteractive)
	if err != nil {
		t.Fatalf("issue operation approval: %v", err)
	}
	if operationGrant.ID == "" {
		t.Fatal("operation approval store returned an empty ID")
	}
	if _, err := store.ConsumeOperation(ctx, operationGrant.ID, actor, guild, operationHash, now); err != nil {
		t.Fatalf("consume operation approval: %v", err)
	}

	planHash := domain.PlanHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	planGrant, err := coordinator.IssueReconciliationPlan(ctx, actor, guild, planHash, application.ConfirmationInteractive, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("issue reconciliation plan approval: %v", err)
	}
	if planGrant.ID == "" || planGrant.ID == operationGrant.ID {
		t.Fatalf("approval store returned invalid plan ID %q", planGrant.ID)
	}
	if _, err := store.ConsumeReconciliationPlan(ctx, planGrant.ID, actor, guild, string(planHash), now); err != nil {
		t.Fatalf("consume reconciliation plan approval: %v", err)
	}
}

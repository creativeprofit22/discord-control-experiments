package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type approvalExecutorFixture struct {
	plan     domain.OperationPlan
	executed bool
	approval domain.ApprovalID
}

func (f *approvalExecutorFixture) PrepareDestructive(context.Context, application.OperatorRequest) (domain.OperationPlan, error) {
	return f.plan, nil
}
func (f *approvalExecutorFixture) Execute(_ context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
	f.executed = true
	f.approval = request.Invocation.ApprovalID
	return application.OperatorResult{Kind: "ok"}, nil
}

type approvalCoordinatorFixture struct{ issued map[string]bool }

func (f *approvalCoordinatorFixture) Issue(_ context.Context, invocation application.Invocation, plan domain.OperationPlan, _ application.ConfirmationSource) (domain.ApprovalGrant, error) {
	if f.issued[plan.OperationHash] {
		return domain.ApprovalGrant{}, &domain.ApprovalError{Reason: "replayed"}
	}
	f.issued[plan.OperationHash] = true
	return domain.ApprovalGrant{ID: "grant", ActorID: invocation.ActorID, OperationHash: plan.OperationHash, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func destructiveFixture(t *testing.T, nonInteractive bool) Command {
	args := []string{"channel", "delete", "--config", "config.json", "--guild", "1", "--channel", "2"}
	if nonInteractive {
		args = append(args, "--non-interactive", "true")
	}
	command, err := Parse(args)
	if err != nil {
		t.Fatal(err)
	}
	return command
}
func invocationFixture() application.Invocation {
	return application.Invocation{Principal: domain.Principal{Adapter: "cli", Subject: "local", AuthenticationMethod: "os-process"}, ActorID: "actor", RequestedMode: domain.ModeApply, CorrelationID: "correlation"}
}

func TestDestructiveApprovalMatrix(t *testing.T) {
	executor := &approvalExecutorFixture{plan: domain.OperationPlan{OperationHash: "full-hash", DestructiveClass: domain.DestructiveDelete}}
	coordinator := &approvalCoordinatorFixture{issued: map[string]bool{}}
	_, err := Dispatch(context.Background(), executor, destructiveFixture(t, false), DispatchOptions{Invocation: invocationFixture(), Approvals: coordinator, Confirm: func(context.Context, domain.OperationPlan) (bool, error) { return true, nil }})
	if err != nil || !executor.executed || executor.approval != "grant" {
		t.Fatalf("executed=%v approval=%q err=%v", executor.executed, executor.approval, err)
	}
}
func TestNonInteractiveCannotBypass(t *testing.T) {
	executor := &approvalExecutorFixture{plan: domain.OperationPlan{OperationHash: "hash", DestructiveClass: domain.DestructiveDelete}}
	_, err := Dispatch(context.Background(), executor, destructiveFixture(t, true), DispatchOptions{Invocation: invocationFixture(), Approvals: &approvalCoordinatorFixture{issued: map[string]bool{}}})
	var denied *domain.AuthorizationError
	if !errors.As(err, &denied) || executor.executed {
		t.Fatalf("executed=%v err=%v", executor.executed, err)
	}
	executor.plan.Preapproved = true
	_, err = Dispatch(context.Background(), executor, destructiveFixture(t, true), DispatchOptions{Invocation: invocationFixture(), Approvals: &approvalCoordinatorFixture{issued: map[string]bool{}}})
	if err != nil || !executor.executed {
		t.Fatalf("preapproved execution err=%v", err)
	}
}
func TestPlanHashRequired(t *testing.T) {
	if _, err := Parse([]string{"apply", "--config", "c", "--plan", "p"}); err == nil {
		t.Fatal("apply accepted without exact plan hash")
	}
}
func TestApprovalReplayRejected(t *testing.T) {
	coordinator := &approvalCoordinatorFixture{issued: map[string]bool{}}
	plan := domain.OperationPlan{OperationHash: "hash", DestructiveClass: domain.DestructiveDelete}
	if _, err := coordinator.Issue(context.Background(), invocationFixture(), plan, application.ConfirmationInteractive); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Issue(context.Background(), invocationFixture(), plan, application.ConfirmationInteractive); err == nil {
		t.Fatal("replayed operation approval issued")
	}
}

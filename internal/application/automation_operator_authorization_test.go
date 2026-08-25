package application_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/ken-kai/discord-control-experiments/internal/policy"
)

type automationAuditRecorder struct {
	mu     sync.Mutex
	events []domain.AuditEvent
}

func (r *automationAuditRecorder) Append(_ context.Context, event domain.AuditEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	return nil
}

func (r *automationAuditRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

type automationOperatorStore struct {
	workflow  domain.AutomationWorkflow
	runs      map[string]domain.AutomationRun
	mutations int
}

func (s *automationOperatorStore) SaveDefinition(context.Context, domain.AutomationWorkflow) (string, error) {
	s.mutations++
	return "", errors.New("unexpected mutation")
}
func (s *automationOperatorStore) LoadDefinition(string) (domain.AutomationWorkflow, error) {
	return s.workflow, nil
}
func (s *automationOperatorStore) SaveSchedule(context.Context, uint64, domain.AutomationSchedule) error {
	s.mutations++
	return errors.New("unexpected mutation")
}
func (s *automationOperatorStore) LoadSchedule(string) (domain.AutomationSchedule, error) {
	return domain.AutomationSchedule{}, errors.New("unexpected schedule load")
}
func (s *automationOperatorStore) EnqueueRun(context.Context, domain.AutomationRun) (bool, error) {
	s.mutations++
	return false, errors.New("unexpected mutation")
}
func (s *automationOperatorStore) LoadRun(id string) (domain.AutomationRun, error) {
	run, ok := s.runs[id]
	if !ok {
		return run, errors.New("run not found")
	}
	return run, nil
}
func (s *automationOperatorStore) CompareAndSwapRun(context.Context, uint64, domain.AutomationRun) error {
	s.mutations++
	return errors.New("unexpected mutation")
}
func (s *automationOperatorStore) CompareAndSwapOwnedRun(context.Context, uint64, domain.AutomationRun, string, string, time.Time) error {
	s.mutations++
	return errors.New("unexpected mutation")
}
func (s *automationOperatorStore) ListRuns() ([]domain.AutomationRun, error) {
	result := make([]domain.AutomationRun, 0, len(s.runs))
	for _, run := range s.runs {
		result = append(result, run)
	}
	return result, nil
}
func (s *automationOperatorStore) DeadLetter(context.Context, domain.AutomationRun) error {
	s.mutations++
	return errors.New("unexpected mutation")
}
func (s *automationOperatorStore) ClaimRun(context.Context, string, string, string, time.Time, time.Duration) (domain.AutomationRun, error) {
	s.mutations++
	return domain.AutomationRun{}, errors.New("unexpected mutation")
}
func (s *automationOperatorStore) Heartbeat(context.Context, string, string, string, time.Time, time.Duration) error {
	s.mutations++
	return errors.New("unexpected mutation")
}

type automationIssuerRecorder struct{ calls int }

func (i *automationIssuerRecorder) Issue(context.Context, domain.ApprovalGrant) (domain.ApprovalGrant, error) {
	i.calls++
	return domain.ApprovalGrant{}, errors.New("unexpected approval")
}

type automationActionsRecorder struct{ calls int }

func (a *automationActionsRecorder) ExecuteAutomationAction(context.Context, application.Invocation, domain.GuildID, domain.AutomationAction, string, string) (application.AutomationActionResult, error) {
	a.calls++
	return application.AutomationActionResult{}, nil
}

func (a *automationActionsRecorder) PreviewAutomationAction(context.Context, application.Invocation, domain.GuildID, domain.AutomationAction) (string, error) {
	a.calls++
	return "", nil
}

func newMessageOperatorAuthorizer(t *testing.T) application.AutomationOperatorAuthorizer {
	t.Helper()
	principal := domain.Principal{Adapter: "cli", Subject: "local-process", AuthenticationMethod: "os-process"}
	policyService, err := policy.NewWithRules(domain.ModeApply, []policy.ActorRule{{ActorID: "human-operator", Principals: []domain.Principal{principal}, Guilds: []domain.GuildID{"101"}, Capabilities: []domain.Capability{domain.CapabilityMessageManage}, Mode: domain.ModeApply}})
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := application.NewPolicyAutomationOperatorAuthorizer(policyService, new(automationAuditRecorder), automationClock{now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	return authorizer
}

func TestAutomationOperatorDenyAllActorCannotReadMutateOrApprove(t *testing.T) {
	ctx := context.Background()
	workflow := automationWorkflow("101")
	hash, err := workflow.Hash()
	if err != nil {
		t.Fatal(err)
	}
	child := []domain.AutomationChildResult{{GuildID: "101", State: domain.AutomationRunFailed}}
	store := &automationOperatorStore{workflow: workflow, runs: map[string]domain.AutomationRun{
		"get":     {ID: "get", WorkflowHash: hash, Children: child},
		"retry":   {ID: "retry", WorkflowHash: hash, Children: child},
		"recover": {ID: "recover", WorkflowHash: hash, State: domain.AutomationRunUncertain, Children: []domain.AutomationChildResult{{GuildID: "101", State: domain.AutomationRunUncertain}}},
		"approve": {ID: "approve", WorkflowHash: hash, State: domain.AutomationRunWaitingApproval, RenderedOperationHash: "reviewed", Children: []domain.AutomationChildResult{{GuildID: "101", State: domain.AutomationRunWaitingApproval}}},
		"dead":    {ID: "dead", WorkflowHash: hash, State: domain.AutomationRunDeadLetter, Children: child},
	}}
	audit := new(automationAuditRecorder)
	authorizer, err := application.NewPolicyAutomationOperatorAuthorizer(policy.New(), audit, automationClock{now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	actions := new(automationActionsRecorder)
	service := &application.AutomationService{Store: store, Actions: actions, Clock: automationClock{now: time.Now()}, ActorID: "automation-actor"}
	operator, err := application.NewAutomationOperatorService(true, store, service, authorizer, []domain.AutomationWorkflow{workflow})
	if err != nil {
		t.Fatal(err)
	}
	issuer := new(automationIssuerRecorder)
	operator.ReviewIssuer, operator.ReviewClock, operator.ReviewTTL = issuer, automationClock{now: time.Now()}, time.Hour
	invocation := automationInvocation("deny-all-cli")

	attempts := []func() error{
		func() error { _, err := operator.List(ctx, invocation); return err },
		func() error { _, err := operator.Get(ctx, invocation, "get"); return err },
		func() error { _, err := operator.Run(ctx, invocation, workflow.ID, "manual"); return err },
		func() error { _, err := operator.Retry(ctx, invocation, "retry"); return err },
		func() error {
			_, err := operator.Recover(ctx, invocation, "recover", "skip", "bounded evidence")
			return err
		},
		func() error { _, err := operator.Approve(ctx, invocation, "approve", "reviewed"); return err },
		func() error { _, err := operator.DeadLetters(ctx, invocation); return err },
	}
	for index, attempt := range attempts {
		var authorization *domain.AuthorizationError
		if err := attempt(); !errors.As(err, &authorization) {
			t.Fatalf("attempt %d error = %v", index, err)
		}
	}
	if store.mutations != 0 || actions.calls != 0 || issuer.calls != 0 {
		t.Fatalf("denial side effects: store=%d Discord=%d approvals=%d", store.mutations, actions.calls, issuer.calls)
	}
	if audit.count() != len(attempts) {
		t.Fatalf("denial audit events = %d, want %d", audit.count(), len(attempts))
	}
}

func TestAutomationOperatorPolicyScopesGuildsActionsAndApprover(t *testing.T) {
	cli := domain.Principal{Adapter: "cli", Subject: "authorized", AuthenticationMethod: "os-process"}
	policyService, err := policy.NewWithRules(domain.ModeApply, []policy.ActorRule{{
		ActorID: "human-operator", Principals: []domain.Principal{cli}, Guilds: []domain.GuildID{"101"},
		Capabilities: []domain.Capability{domain.CapabilityJobRead, domain.CapabilityMessageManage}, Mode: domain.ModeApply,
	}})
	if err != nil {
		t.Fatal(err)
	}
	audit := new(automationAuditRecorder)
	authorizer, err := application.NewPolicyAutomationOperatorAuthorizer(policyService, audit, automationClock{now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	invocation := automationInvocation("human-operator")
	invocation.Principal = cli
	allowed := application.AutomationOperatorAuthorizationRequest{Invocation: invocation, GuildID: "101", Capability: domain.CapabilityMessageManage, Mode: domain.ModeApply, DestructiveClass: domain.DestructiveNone, Operation: "workflow.run"}
	if err := authorizer.AuthorizeAutomationOperator(context.Background(), allowed); err != nil {
		t.Fatalf("authorized action denied: %v", err)
	}
	wrongGuild := allowed
	wrongGuild.GuildID = "102"
	wrongAction := allowed
	wrongAction.Capability = domain.CapabilityRoleManage
	for _, request := range []application.AutomationOperatorAuthorizationRequest{wrongGuild, wrongAction} {
		var authorization *domain.AuthorizationError
		if err := authorizer.AuthorizeAutomationOperator(context.Background(), request); !errors.As(err, &authorization) {
			t.Fatalf("unauthorized request error = %v", err)
		}
	}
	if audit.count() != 2 {
		t.Fatalf("denial audit events = %d, want 2", audit.count())
	}
}

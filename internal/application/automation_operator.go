package application

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type AutomationOperatorService struct {
	Enabled      bool
	Store        AutomationStore
	Service      *AutomationService
	Authorizer   AutomationOperatorAuthorizer
	Workflows    map[string]domain.AutomationWorkflow
	ReviewIssuer ApprovalIssuer
	ReviewClock  AutomationClock
	ReviewTTL    time.Duration
}

type AutomationWorkflowView struct {
	ID, Hash, Trigger string
	Enabled           bool
	Guilds, Steps     int
}

func NewAutomationOperatorService(enabled bool, store AutomationStore, service *AutomationService, authorizer AutomationOperatorAuthorizer, workflows []domain.AutomationWorkflow) (*AutomationOperatorService, error) {
	if store == nil || service == nil || authorizer == nil {
		return nil, errors.New("automation operator dependencies are required")
	}
	result := &AutomationOperatorService{Enabled: enabled, Store: store, Service: service, Authorizer: authorizer, Workflows: map[string]domain.AutomationWorkflow{}}
	for _, workflow := range workflows {
		if _, exists := result.Workflows[workflow.ID]; exists {
			return nil, errors.New("duplicate workflow ID")
		}
		result.Workflows[workflow.ID] = workflow
	}
	return result, nil
}

func (s *AutomationOperatorService) List(ctx context.Context, invocation Invocation) ([]AutomationWorkflowView, error) {
	views := make([]AutomationWorkflowView, 0, len(s.Workflows))
	for _, workflow := range s.Workflows {
		if err := s.authorizeWorkflowRead(ctx, invocation, workflow, "workflow.list"); err != nil {
			return nil, err
		}
		hash, err := workflow.Hash()
		if err != nil {
			return nil, err
		}
		views = append(views, AutomationWorkflowView{ID: workflow.ID, Hash: hash, Trigger: workflow.Trigger.Kind, Enabled: workflow.Enabled, Guilds: len(workflow.Targets.GuildIDs), Steps: len(workflow.Steps)})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	return views, nil
}

func (s *AutomationOperatorService) Get(ctx context.Context, invocation Invocation, runID string) (domain.AutomationRun, error) {
	run, err := s.Store.LoadRun(runID)
	if err != nil {
		return run, err
	}
	if err := s.authorizeRunRead(ctx, invocation, run, "workflow.get"); err != nil {
		return domain.AutomationRun{}, err
	}
	return run, nil
}

func (s *AutomationOperatorService) Validate(ctx context.Context, invocation Invocation, id string) (AutomationWorkflowView, error) {
	workflow, ok := s.Workflows[id]
	if !ok {
		return AutomationWorkflowView{}, errors.New("workflow not found")
	}
	if err := s.authorizeWorkflowRead(ctx, invocation, workflow, "workflow.validate"); err != nil {
		return AutomationWorkflowView{}, err
	}
	if err := s.Service.Validate(workflow); err != nil {
		return AutomationWorkflowView{}, err
	}
	hash, _ := workflow.Hash()
	return AutomationWorkflowView{ID: id, Hash: hash, Trigger: workflow.Trigger.Kind, Enabled: workflow.Enabled, Guilds: len(workflow.Targets.GuildIDs), Steps: len(workflow.Steps)}, nil
}

func (s *AutomationOperatorService) Run(ctx context.Context, invocation Invocation, id, triggerID string) (domain.AutomationRun, error) {
	if !s.Enabled {
		return domain.AutomationRun{}, errors.New("automation is disabled")
	}
	workflow, ok := s.Workflows[id]
	if !ok {
		return domain.AutomationRun{}, errors.New("workflow not found")
	}
	if !workflow.Enabled {
		return domain.AutomationRun{}, errors.New("workflow is disabled")
	}
	if workflow.Trigger.Kind != domain.TriggerManual {
		return domain.AutomationRun{}, errors.New("only manual workflows can be started directly")
	}
	if err := s.authorizeWorkflowActions(ctx, invocation, workflow, "workflow.run"); err != nil {
		return domain.AutomationRun{}, err
	}
	return s.Service.Run(ctx, AutomationRunRequest{Workflow: workflow, TriggerID: triggerID})
}

func (s *AutomationOperatorService) Approve(ctx context.Context, invocation Invocation, runID, reviewed string) (domain.AutomationRun, error) {
	run, err := s.Store.LoadRun(runID)
	if err != nil {
		return run, err
	}
	approver := invocation.ActorID
	if run.State != domain.AutomationRunWaitingApproval || reviewed != run.RenderedOperationHash || approver == "" || approver == s.Service.ActorID || s.ReviewIssuer == nil || s.ReviewClock == nil || s.ReviewTTL <= 0 {
		return run, errors.New("workflow review cannot be approved")
	}
	workflow, err := s.Store.LoadDefinition(run.WorkflowHash)
	if err != nil {
		return run, err
	}
	guild, action, err := pendingAutomationAction(run, workflow)
	if err != nil {
		return run, err
	}
	if err := s.authorizeAction(ctx, invocation, guild, action, "workflow.approve"); err != nil {
		return run, err
	}
	now := s.ReviewClock.Now()
	grant, err := s.ReviewIssuer.Issue(ctx, domain.ApprovalGrant{ActorID: s.Service.ActorID, GuildID: guild, ScopeKind: domain.ApprovalScopeOperation, ScopeHash: reviewed, ApproverID: approver, IssuedAt: now, ExpiresAt: now.Add(s.ReviewTTL)})
	if err != nil {
		return run, err
	}
	return s.Service.Run(ctx, AutomationRunRequest{Workflow: workflow, TriggerID: run.Provenance.TriggerID, ReviewedHash: reviewed, ApprovalID: grant.ID})
}

func (s *AutomationOperatorService) Retry(ctx context.Context, invocation Invocation, runID string) (domain.AutomationRun, error) {
	run, err := s.Store.LoadRun(runID)
	if err != nil {
		return run, err
	}
	workflow, err := s.Store.LoadDefinition(run.WorkflowHash)
	if err != nil {
		return run, err
	}
	if err := s.authorizeWorkflowActions(ctx, invocation, workflow, "workflow.retry"); err != nil {
		return run, err
	}
	return s.Service.Run(ctx, AutomationRunRequest{Workflow: workflow, TriggerID: run.Provenance.TriggerID, RetryFailed: true, Recovery: run.RenderedContent != ""})
}

func (s *AutomationOperatorService) Recover(ctx context.Context, invocation Invocation, runID, decision, evidence string) (domain.AutomationRun, error) {
	run, err := s.Store.LoadRun(runID)
	if err != nil {
		return run, err
	}
	if run.State != domain.AutomationRunUncertain || evidence == "" || len(evidence) > 1024 {
		return run, errors.New("uncertain recovery requires bounded evidence")
	}
	workflow, err := s.Store.LoadDefinition(run.WorkflowHash)
	if err != nil {
		return run, err
	}
	if err := s.authorizeWorkflowActions(ctx, invocation, workflow, "workflow.recover"); err != nil {
		return run, err
	}
	journal, events, err := s.Service.Journals.OpenAutomationJournal(run.ID, run.WorkflowHash)
	if err != nil {
		return run, err
	}
	defer journal.Close()
	if len(events) == 0 || events[len(events)-1].State != "dispatching" {
		return run, errors.New("uncertain journal evidence is missing")
	}
	stepID := events[len(events)-1].StepID
	switch decision {
	case "retry":
		if _, err := journal.Append("operator_retry", stepID, s.Service.Clock.Now()); err != nil {
			return run, err
		}
		next := run
		next.State = domain.AutomationRunQueued
		next.Error = "operator retry evidence " + automationID("evidence", evidence)
		if err := s.Store.CompareAndSwapRun(ctx, run.Revision, next); err != nil {
			return run, err
		}
		return s.Service.Run(ctx, AutomationRunRequest{Workflow: workflow, TriggerID: run.Provenance.TriggerID, Recovery: true})
	case "skip":
		if _, err := journal.Append("operator_skipped", stepID, s.Service.Clock.Now()); err != nil {
			return run, err
		}
		found := false
		for index := range run.Children {
			if run.Children[index].State == domain.AutomationRunUncertain {
				run.Children[index].CompletedStep++
				run.Children[index].State = domain.AutomationRunFailed
				found = true
				break
			}
		}
		if !found {
			return run, errors.New("uncertain child is missing")
		}
		run.State, run.Error = domain.AutomationRunFailed, "operator skip evidence "+automationID("evidence", evidence)
		run.RenderedOperationHash, run.RenderedContent, run.PendingUserID, run.PendingRoleIDs = "", "", "", nil
		if err := s.Store.CompareAndSwapRun(ctx, run.Revision, run); err != nil {
			return run, err
		}
		return s.Store.LoadRun(run.ID)
	default:
		return run, errors.New("recovery decision must be retry or skip")
	}
}

func (s *AutomationOperatorService) DeadLetters(ctx context.Context, invocation Invocation) ([]domain.AutomationRun, error) {
	runs, err := s.Store.ListRuns()
	if err != nil {
		return nil, err
	}
	filtered := runs[:0]
	for _, run := range runs {
		if run.State != domain.AutomationRunDeadLetter {
			continue
		}
		if err := s.authorizeRunRead(ctx, invocation, run, "workflow.dead-letter.list"); err != nil {
			return nil, err
		}
		filtered = append(filtered, run)
	}
	return filtered, nil
}

func (s *AutomationOperatorService) authorizeWorkflowRead(ctx context.Context, invocation Invocation, workflow domain.AutomationWorkflow, operation domain.OperationName) error {
	if len(workflow.Targets.GuildIDs) == 0 {
		return s.Authorizer.AuthorizeAutomationOperator(ctx, AutomationOperatorAuthorizationRequest{Invocation: invocation, Capability: domain.CapabilityJobRead, Mode: domain.ModeReadOnly, DestructiveClass: domain.DestructiveNone, Operation: operation})
	}
	for _, guild := range workflow.Targets.GuildIDs {
		if err := s.Authorizer.AuthorizeAutomationOperator(ctx, AutomationOperatorAuthorizationRequest{Invocation: invocation, GuildID: domain.GuildID(guild), Capability: domain.CapabilityJobRead, Mode: domain.ModeReadOnly, DestructiveClass: domain.DestructiveNone, Operation: operation}); err != nil {
			return err
		}
	}
	return nil
}

func (s *AutomationOperatorService) authorizeRunRead(ctx context.Context, invocation Invocation, run domain.AutomationRun, operation domain.OperationName) error {
	if len(run.Children) == 0 {
		return s.Authorizer.AuthorizeAutomationOperator(ctx, AutomationOperatorAuthorizationRequest{Invocation: invocation, Capability: domain.CapabilityJobRead, Mode: domain.ModeReadOnly, DestructiveClass: domain.DestructiveNone, Operation: operation})
	}
	for _, child := range run.Children {
		if err := s.Authorizer.AuthorizeAutomationOperator(ctx, AutomationOperatorAuthorizationRequest{Invocation: invocation, GuildID: domain.GuildID(child.GuildID), Capability: domain.CapabilityJobRead, Mode: domain.ModeReadOnly, DestructiveClass: domain.DestructiveNone, Operation: operation}); err != nil {
			return err
		}
	}
	return nil
}

func (s *AutomationOperatorService) authorizeWorkflowActions(ctx context.Context, invocation Invocation, workflow domain.AutomationWorkflow, operation domain.OperationName) error {
	if len(workflow.Targets.GuildIDs) == 0 || len(workflow.Steps) == 0 {
		return s.Authorizer.AuthorizeAutomationOperator(ctx, AutomationOperatorAuthorizationRequest{Invocation: invocation, Mode: domain.ModeApply, DestructiveClass: domain.DestructiveNone, Operation: operation})
	}
	for _, guild := range workflow.Targets.GuildIDs {
		for _, step := range workflow.Steps {
			if err := s.authorizeAction(ctx, invocation, domain.GuildID(guild), step.Action, operation); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *AutomationOperatorService) authorizeAction(ctx context.Context, invocation Invocation, guild domain.GuildID, action domain.AutomationAction, operation domain.OperationName) error {
	capability, destructive, err := AutomationActionAuthorization(action)
	if err != nil {
		return s.Authorizer.AuthorizeAutomationOperator(ctx, AutomationOperatorAuthorizationRequest{Invocation: invocation, GuildID: guild, Mode: domain.ModeApply, DestructiveClass: domain.DestructiveNone, Operation: operation})
	}
	return s.Authorizer.AuthorizeAutomationOperator(ctx, AutomationOperatorAuthorizationRequest{Invocation: invocation, GuildID: guild, Capability: capability, Mode: domain.ModeApply, DestructiveClass: destructive, Operation: operation})
}

func pendingAutomationAction(run domain.AutomationRun, workflow domain.AutomationWorkflow) (domain.GuildID, domain.AutomationAction, error) {
	for _, child := range run.Children {
		if child.State == domain.AutomationRunSucceeded {
			continue
		}
		if child.GuildID == "" || child.CompletedStep < 0 || child.CompletedStep >= len(workflow.Steps) {
			break
		}
		return domain.GuildID(child.GuildID), workflow.Steps[child.CompletedStep].Action, nil
	}
	return "", domain.AutomationAction{}, errors.New("workflow review target action is missing")
}

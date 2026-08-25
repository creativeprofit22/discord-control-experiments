package application_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/approval"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/ken-kai/discord-control-experiments/internal/state"
)

type automationClock struct{ now time.Time }

func (c automationClock) Now() time.Time { return c.now }

type allowAutomationOperator struct{}

func (allowAutomationOperator) AuthorizeAutomationOperator(context.Context, application.AutomationOperatorAuthorizationRequest) error {
	return nil
}

func automationInvocation(actor domain.ActorID) application.Invocation {
	return application.Invocation{Principal: domain.Principal{Adapter: "cli", Subject: "local-process", AuthenticationMethod: "os-process"}, ActorID: actor, RequestedMode: domain.ModeApply, CorrelationID: "automation-operator-test"}
}

type scriptedAutomationActions struct {
	mu        sync.Mutex
	calls     map[string]int
	failOnce  string
	uncertain string
	preview   string
	retryable bool
}

func (a *scriptedAutomationActions) ExecuteAutomationAction(_ context.Context, _ application.Invocation, guild domain.GuildID, action domain.AutomationAction, content string, _ string) (application.AutomationActionResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls[string(guild)]++
	if string(guild) == a.uncertain {
		return application.AutomationActionResult{Uncertain: true}, errors.New("lost response")
	}
	if string(guild) == a.failOnce && a.calls[string(guild)] == 1 {
		return application.AutomationActionResult{ProvenUnsent: true, Retryable: a.retryable}, errors.New("scripted failure")
	}
	if action.Kind == domain.ActionCleanup && content != a.preview {
		return application.AutomationActionResult{ProvenUnsent: true, ReviewChanged: true, Rendered: a.preview}, errors.New("cleanup candidate count changed")
	}
	return application.AutomationActionResult{}, nil
}

func (a *scriptedAutomationActions) PreviewAutomationAction(context.Context, application.Invocation, domain.GuildID, domain.AutomationAction) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.preview, nil
}

type fixedSummary struct{ text string }

func (s fixedSummary) Summarize(context.Context, application.SummaryRequest) (application.SummaryResult, error) {
	return application.SummaryResult{Text: s.text, Model: "fixed"}, nil
}

type concurrentJournalStore struct {
	store   *state.AutomationStore
	opened  chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *concurrentJournalStore) OpenAutomationJournal(runID, workflowHash string) (application.AutomationJournal, []domain.AutomationJournalEvent, error) {
	journal, events, err := s.store.OpenAutomationJournal(runID, workflowHash)
	if err != nil {
		return nil, nil, err
	}
	s.opened <- struct{}{}
	<-s.release
	return journal, events, nil
}

func (s *concurrentJournalStore) releaseOpen() { s.once.Do(func() { close(s.release) }) }

func automationWorkflow(guilds ...string) domain.AutomationWorkflow {
	return domain.AutomationWorkflow{Version: 1, ID: "cross-guild", Enabled: true, Trigger: domain.AutomationTrigger{Kind: domain.TriggerManual}, Targets: domain.AutomationTargets{GuildIDs: guilds}, Retry: domain.AutomationRetry{MaxAttempts: 2, Initial: "1s", Maximum: "5s"}, Steps: []domain.AutomationStep{{Action: domain.AutomationAction{Kind: domain.ActionMessage, ChannelID: "999", Content: "hello"}}}}
}

func automationService(t *testing.T, workflow domain.AutomationWorkflow, actions application.AutomationActionExecutor) *application.AutomationService {
	t.Helper()
	paths, err := state.ResolvePaths(t.TempDir(), "state")
	if err != nil {
		t.Fatal(err)
	}
	store := state.NewAutomationStore(paths)
	catalog := application.DefaultAutomationCatalog()
	for _, guild := range workflow.Targets.GuildIDs {
		catalog.Guilds[guild] = application.AutomationGuildBinding{Channels: map[string]struct{}{"999": {}}, Roles: map[string]struct{}{}}
	}
	return &application.AutomationService{Store: store, Journals: store, Actions: actions, Summarizer: fixedSummary{text: "summary"}, Clock: automationClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, Catalog: catalog, Principal: domain.Principal{Adapter: "automation", Subject: "local-scheduler", AuthenticationMethod: "local-process"}}
}

func TestManualRunRejectsGloballyDisabledAutomation(t *testing.T) {
	workflow := automationWorkflow("101")
	actions := &scriptedAutomationActions{calls: map[string]int{}}
	service := automationService(t, workflow, actions)
	operator, err := application.NewAutomationOperatorService(false, service.Store, service, allowAutomationOperator{}, []domain.AutomationWorkflow{workflow})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operator.Run(context.Background(), automationInvocation("human-operator"), workflow.ID, "disabled-global"); err == nil {
		t.Fatal("globally disabled automation started a manual run")
	}
	runs, err := service.Store.ListRuns()
	if err != nil || len(runs) != 0 || len(actions.calls) != 0 {
		t.Fatalf("disabled automation persisted or dispatched: runs=%d calls=%v err=%v", len(runs), actions.calls, err)
	}
}

func TestManualRunRejectsDisabledWorkflow(t *testing.T) {
	workflow := automationWorkflow("101")
	workflow.Enabled = false
	actions := &scriptedAutomationActions{calls: map[string]int{}}
	service := automationService(t, workflow, actions)
	operator, err := application.NewAutomationOperatorService(true, service.Store, service, allowAutomationOperator{}, []domain.AutomationWorkflow{workflow})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operator.Run(context.Background(), automationInvocation("human-operator"), workflow.ID, "disabled-workflow"); err == nil {
		t.Fatal("disabled workflow started a manual run")
	}
	runs, err := service.Store.ListRuns()
	if err != nil || len(runs) != 0 || len(actions.calls) != 0 {
		t.Fatalf("disabled workflow persisted or dispatched: runs=%d calls=%v err=%v", len(runs), actions.calls, err)
	}
}

func TestConcurrentManualRunsHaveSingleLeaseOwner(t *testing.T) {
	workflow := automationWorkflow("101")
	actions := &scriptedAutomationActions{calls: map[string]int{}}
	service := automationService(t, workflow, actions)
	store := service.Store.(*state.AutomationStore)
	journals := &concurrentJournalStore{store: store, opened: make(chan struct{}, 2), release: make(chan struct{})}
	service.Journals = journals
	operator, err := application.NewAutomationOperatorService(true, store, service, allowAutomationOperator{}, []domain.AutomationWorkflow{workflow})
	if err != nil {
		t.Fatal(err)
	}

	results := make(chan error, 2)
	run := func() {
		projection, runErr := operator.Run(context.Background(), automationInvocation("human-operator"), workflow.ID, "same-trigger")
		if runErr == nil && projection.State != domain.AutomationRunSucceeded && projection.State != domain.AutomationRunRunning {
			runErr = errors.New("unexpected concurrent run state: " + projection.State)
		}
		results <- runErr
	}
	go run()
	<-journals.opened
	go run()
	received := 0
	select {
	case <-journals.opened:
		journals.releaseOpen()
	case err := <-results:
		received++
		journals.releaseOpen()
		if err != nil {
			t.Fatal(err)
		}
	}
	for ; received < 2; received++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if actions.calls["101"] != 1 {
		t.Fatalf("Discord actions = %d", actions.calls["101"])
	}
	projection, err := operator.Run(context.Background(), automationInvocation("human-operator"), workflow.ID, "same-trigger")
	if err != nil || projection.State != domain.AutomationRunSucceeded {
		t.Fatalf("duplicate projection = %#v, %v", projection, err)
	}
	journal, events, err := store.OpenAutomationJournal(projection.ID, projection.WorkflowHash)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if len(events) != 2 || events[0].State != "dispatching" || events[1].State != "completed" {
		t.Fatalf("journal events = %#v", events)
	}
}

func TestAutomationCrossGuildPolicyIsolationAndResume(t *testing.T) {
	workflow := automationWorkflow("101", "102", "103")
	actions := &scriptedAutomationActions{calls: map[string]int{}, failOnce: "102"}
	service := automationService(t, workflow, actions)
	run, err := service.Run(context.Background(), application.AutomationRunRequest{Workflow: workflow, TriggerID: "manual-1"})
	if err != nil || run.State != domain.AutomationRunFailed {
		t.Fatalf("first run = %#v, %v", run, err)
	}
	run, err = service.Run(context.Background(), application.AutomationRunRequest{Workflow: workflow, TriggerID: "manual-1", RetryFailed: true})
	if err != nil || run.State != domain.AutomationRunSucceeded {
		t.Fatalf("resumed run = %#v, %v", run, err)
	}
	if actions.calls["101"] != 1 || actions.calls["102"] != 2 || actions.calls["103"] != 1 {
		t.Fatalf("calls = %#v", actions.calls)
	}
}

func TestAutomationApprovalPersistsRenderedHash(t *testing.T) {
	workflow := automationWorkflow("101")
	workflow.Steps[0].Action.RequireReview = true
	actions := &scriptedAutomationActions{calls: map[string]int{}}
	service := automationService(t, workflow, actions)
	run, err := service.Run(context.Background(), application.AutomationRunRequest{Workflow: workflow, TriggerID: "manual-approval"})
	if err != nil || run.State != domain.AutomationRunWaitingApproval || run.RenderedOperationHash == "" || len(actions.calls) != 0 {
		t.Fatalf("approval wait = %#v, %v", run, err)
	}
	now := service.Clock.Now()
	approvalStore, err := approval.NewStoreWithDependencies(time.Hour, func() time.Time { return now }, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	service.ActorID, service.Approvals = "automation-actor", approvalStore
	operator, err := application.NewAutomationOperatorService(true, service.Store, service, newMessageOperatorAuthorizer(t), []domain.AutomationWorkflow{workflow})
	if err != nil {
		t.Fatal(err)
	}
	operator.ReviewIssuer, operator.ReviewClock, operator.ReviewTTL = approvalStore, service.Clock, time.Hour
	if _, err := operator.Approve(context.Background(), automationInvocation(service.ActorID), run.ID, run.RenderedOperationHash); err == nil {
		t.Fatal("automation actor self-approved review")
	}
	run, err = operator.Approve(context.Background(), automationInvocation("human-operator"), run.ID, run.RenderedOperationHash)
	if err != nil || run.State != domain.AutomationRunSucceeded || actions.calls["101"] != 1 {
		t.Fatalf("approved run = %#v, %v", run, err)
	}
}

func TestAutomationIntegrationUncertainMutationDoesNotReplay(t *testing.T) {
	workflow := automationWorkflow("101")
	actions := &scriptedAutomationActions{calls: map[string]int{}, uncertain: "101"}
	service := automationService(t, workflow, actions)
	run, err := service.Run(context.Background(), application.AutomationRunRequest{Workflow: workflow, TriggerID: "uncertain"})
	if err != nil || run.State != domain.AutomationRunUncertain {
		t.Fatalf("uncertain = %#v, %v", run, err)
	}
	_, _ = service.Run(context.Background(), application.AutomationRunRequest{Workflow: workflow, TriggerID: "uncertain", RetryFailed: true})
	if actions.calls["101"] != 1 {
		t.Fatalf("uncertain replay calls = %d", actions.calls["101"])
	}
	operator, err := application.NewAutomationOperatorService(true, service.Store, service, allowAutomationOperator{}, []domain.AutomationWorkflow{workflow})
	if err != nil {
		t.Fatal(err)
	}
	run, err = operator.Recover(context.Background(), automationInvocation("human-operator"), run.ID, "retry", "Discord audit confirms no message exists")
	if err != nil || run.State != domain.AutomationRunUncertain || actions.calls["101"] != 2 {
		t.Fatalf("evidence retry = %#v calls=%d err=%v", run, actions.calls["101"], err)
	}
	run, err = operator.Recover(context.Background(), automationInvocation("human-operator"), run.ID, "skip", "operator accepts possible prior effect")
	if err != nil || run.State != domain.AutomationRunFailed || actions.calls["101"] != 2 {
		t.Fatalf("evidence skip = %#v calls=%d err=%v", run, actions.calls["101"], err)
	}
}

func TestAutomationDeadLetterAfterRetryExhaustion(t *testing.T) {
	workflow := automationWorkflow("101")
	workflow.Retry.MaxAttempts = 1
	actions := &scriptedAutomationActions{calls: map[string]int{}, failOnce: "101", retryable: true}
	service := automationService(t, workflow, actions)
	run, err := service.Run(context.Background(), application.AutomationRunRequest{Workflow: workflow, TriggerID: "dead-letter"})
	if err != nil || run.State != domain.AutomationRunDeadLetter {
		t.Fatalf("dead letter = %#v err=%v", run, err)
	}
	operator, _ := application.NewAutomationOperatorService(true, service.Store, service, allowAutomationOperator{}, []domain.AutomationWorkflow{workflow})
	dead, err := operator.DeadLetters(context.Background(), automationInvocation("human-operator"))
	if err != nil || len(dead) != 1 || dead[0].ID != run.ID {
		t.Fatalf("dead letters = %#v err=%v", dead, err)
	}
}

func TestAutomationCleanupPreviewApprovalAndDriftStop(t *testing.T) {
	workflow := automationWorkflow("101")
	workflow.Steps[0].Action = domain.AutomationAction{Kind: domain.ActionCleanup, ChannelID: "999", Age: "24h", Limit: 10, RequireReview: true}
	actions := &scriptedAutomationActions{calls: map[string]int{}, preview: "cleanup-candidates:2"}
	service := automationService(t, workflow, actions)
	service.Catalog.CleanupChannels["101:999"] = struct{}{}
	run, err := service.Run(context.Background(), application.AutomationRunRequest{Workflow: workflow, TriggerID: "cleanup"})
	if err != nil || run.State != domain.AutomationRunWaitingApproval || run.RenderedContent != "cleanup-candidates:2" || actions.calls["101"] != 0 {
		t.Fatalf("cleanup preview = %#v calls=%d err=%v", run, actions.calls["101"], err)
	}
	actions.preview = "cleanup-candidates:3"
	run, err = service.Run(context.Background(), application.AutomationRunRequest{Workflow: workflow, TriggerID: "cleanup", ReviewedHash: run.RenderedOperationHash, ApprovalID: "fresh"})
	if err != nil || run.State != domain.AutomationRunWaitingApproval || run.RenderedContent != "cleanup-candidates:3" || actions.calls["101"] != 1 {
		t.Fatalf("cleanup drift = %#v calls=%d err=%v", run, actions.calls["101"], err)
	}
}

func TestAISummaryRequiresReviewAndProvenance(t *testing.T) {
	workflow := automationWorkflow("101")
	workflow.Trigger = domain.AutomationTrigger{Kind: domain.TriggerNews, Source: "feed", Limit: 2}
	workflow.Steps[0].Transforms = []domain.AutomationTransform{{Kind: domain.TransformSummarize, MaxBytes: 200}}
	service := automationService(t, workflow, &scriptedAutomationActions{calls: map[string]int{}})
	service.Catalog.Sources["feed"] = struct{}{}
	if err := service.Validate(workflow); err == nil {
		t.Fatal("AI publication without review accepted")
	}
	workflow.Steps[0].Action.RequireReview = true
	run, err := service.Run(context.Background(), application.AutomationRunRequest{Workflow: workflow, TriggerID: "feed-1", Item: &application.SourceItem{ID: "item-1", Title: "release", Text: "untrusted text", Link: "https://example.com/release", Published: time.Now()}})
	if err != nil || run.State != domain.AutomationRunWaitingApproval || run.Provenance.SourceID != "item-1" {
		t.Fatalf("summary run = %#v, %v", run, err)
	}
}

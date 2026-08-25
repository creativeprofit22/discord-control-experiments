package jobs

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/ken-kai/discord-control-experiments/internal/state"
)

type schedulerTestClock struct{ now time.Time }

func (c *schedulerTestClock) Now() time.Time { return c.now }
func (c *schedulerTestClock) NewTicker(time.Duration) SchedulerTicker {
	return &schedulerTestTicker{channel: make(chan time.Time)}
}

type schedulerTestTicker struct{ channel chan time.Time }

func (t *schedulerTestTicker) C() <-chan time.Time { return t.channel }
func (*schedulerTestTicker) Stop()                 {}

type schedulerActions struct {
	mu    sync.Mutex
	calls int
}

func (a *schedulerActions) ExecuteAutomationAction(context.Context, application.Invocation, domain.GuildID, domain.AutomationAction, string, string) (application.AutomationActionResult, error) {
	a.mu.Lock()
	a.calls++
	a.mu.Unlock()
	return application.AutomationActionResult{}, nil
}

type schedulerSource struct {
	mu       sync.Mutex
	failures int
	item     application.SourceItem
}

func (s *schedulerSource) Fetch(context.Context, string, string, int) (application.SourceBatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failures > 0 {
		s.failures--
		return application.SourceBatch{}, errors.New("scripted source failure")
	}
	return application.SourceBatch{Items: []application.SourceItem{s.item}, Cursor: `{"seen":["item-1"]}`}, nil
}

type pagingMembers struct {
	mu    sync.Mutex
	pages map[string][]domain.Member
	after []string
}

func (p *pagingMembers) ListAutomationMembers(_ context.Context, _ domain.GuildID, after string, _ int) ([]domain.Member, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.after = append(p.after, after)
	return append([]domain.Member(nil), p.pages[after]...), nil
}

type schedulerTelemetry struct {
	mu     sync.Mutex
	events []string
}

func (t *schedulerTelemetry) ObserveWorkflow(_ context.Context, observation application.WorkflowObservation) {
	t.mu.Lock()
	t.events = append(t.events, observation.Event)
	t.mu.Unlock()
}

func schedulerFixture(t *testing.T, workflow domain.AutomationWorkflow, source application.AutomationSource) (*Scheduler, *schedulerActions, *state.AutomationStore, *schedulerTestClock) {
	t.Helper()
	paths, err := state.ResolvePaths(t.TempDir(), "state")
	if err != nil {
		t.Fatal(err)
	}
	store := state.NewAutomationStore(paths)
	clock := &schedulerTestClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	actions := &schedulerActions{}
	catalog := application.DefaultAutomationCatalog()
	catalog.Guilds["101"] = application.AutomationGuildBinding{Channels: map[string]struct{}{"999": {}}, Roles: map[string]struct{}{}}
	catalog.Sources["feed"] = struct{}{}
	service := &application.AutomationService{Store: store, Journals: store, Actions: actions, Clock: clock, Catalog: catalog, Principal: domain.Principal{Adapter: "automation", Subject: "local-scheduler", AuthenticationMethod: "local-process"}}
	scheduler, err := NewScheduler(store, service, source, nil, &schedulerTelemetry{}, clock, []domain.AutomationWorkflow{workflow}, time.Second, time.Minute, 2)
	if err != nil {
		t.Fatal(err)
	}
	return scheduler, actions, store, clock
}

func scheduledWorkflow() domain.AutomationWorkflow {
	return domain.AutomationWorkflow{Version: 1, ID: "once-post", Enabled: true, Trigger: domain.AutomationTrigger{Kind: domain.TriggerOnce, At: "2026-01-01T00:00:00Z"}, Targets: domain.AutomationTargets{GuildIDs: []string{"101"}}, Retry: domain.AutomationRetry{MaxAttempts: 2, Initial: "1s", Maximum: "5s"}, Steps: []domain.AutomationStep{{Action: domain.AutomationAction{Kind: domain.ActionMessage, ChannelID: "999", Content: "hello"}}}}
}

func TestAutomationIntegrationRestartAndDuplicateWorkers(t *testing.T) {
	workflow := scheduledWorkflow()
	scheduler, actions, store, clock := schedulerFixture(t, workflow, nil)
	var wait sync.WaitGroup
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() { defer wait.Done(); _ = scheduler.tick(context.Background()) }()
	}
	wait.Wait()
	if actions.calls != 1 {
		t.Fatalf("Discord actions = %d", actions.calls)
	}
	schedule, err := store.LoadSchedule(workflow.ID)
	if err != nil || !schedule.NextRun.After(clock.now) {
		t.Fatalf("schedule = %#v, %v", schedule, err)
	}
	restarted, err := NewScheduler(store, scheduler.service, nil, nil, nil, clock, []domain.AutomationWorkflow{workflow}, time.Second, time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.tick(context.Background()); err != nil || actions.calls != 1 {
		t.Fatalf("restart duplicated action: calls=%d err=%v", actions.calls, err)
	}
}

func TestAutomationIntegrationMemberBaselinePagination(t *testing.T) {
	workflow := scheduledWorkflow()
	workflow.ID = "members"
	workflow.Trigger = domain.AutomationTrigger{Kind: domain.TriggerMemberScan, ScanEvery: "5m", Limit: 2}
	workflow.Steps[0].Action.Kind = domain.ActionWelcome
	scheduler, actions, _, clock := schedulerFixture(t, workflow, nil)
	members := &pagingMembers{pages: map[string][]domain.Member{"": {{User: domain.User{ID: "1"}}, {User: domain.User{ID: "2"}}}, "2": {{User: domain.User{ID: "3"}}}, "3": {{User: domain.User{ID: "4"}}}}}
	scheduler.members = members
	if err := scheduler.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(5 * time.Minute)
	if err := scheduler.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if actions.calls != 0 {
		t.Fatalf("baseline sent %d welcomes", actions.calls)
	}
	clock.now = clock.now.Add(5 * time.Minute)
	if err := scheduler.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if actions.calls != 1 {
		t.Fatalf("new-member welcomes = %d", actions.calls)
	}
	if strings.Join(members.after, ",") != ",2,3" {
		t.Fatalf("member cursors = %#v", members.after)
	}
}

func TestAutomationIntegrationSourceFailurePreservesCursor(t *testing.T) {
	workflow := scheduledWorkflow()
	workflow.ID = "news"
	workflow.Trigger = domain.AutomationTrigger{Kind: domain.TriggerNews, Source: "feed", Limit: 10}
	source := &schedulerSource{failures: 1, item: application.SourceItem{ID: "item-1", Title: "release", Link: "https://example.com/release"}}
	scheduler, actions, store, _ := schedulerFixture(t, workflow, source)
	if err := scheduler.tick(context.Background()); err == nil {
		t.Fatal("source failure was hidden")
	}
	schedule, _ := store.LoadSchedule(workflow.ID)
	if schedule.Cursor != "" || actions.calls != 0 {
		t.Fatalf("failed cursor = %#v, calls=%d", schedule, actions.calls)
	}
	if err := scheduler.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	schedule, _ = store.LoadSchedule(workflow.ID)
	if schedule.Cursor == "" || actions.calls != 1 {
		t.Fatalf("completed cursor = %#v, calls=%d", schedule, actions.calls)
	}
}

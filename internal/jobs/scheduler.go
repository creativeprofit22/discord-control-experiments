package jobs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type SchedulerTicker interface {
	C() <-chan time.Time
	Stop()
}
type SchedulerClock interface {
	Now() time.Time
	NewTicker(time.Duration) SchedulerTicker
}

type Scheduler struct {
	store       application.AutomationStore
	service     *application.AutomationService
	sources     application.AutomationSource
	members     application.AutomationMemberScanner
	telemetry   application.WorkflowTelemetry
	clock       SchedulerClock
	workflows   []domain.AutomationWorkflow
	poll, lease time.Duration
	workers     int
	workerID    string
	cancel      context.CancelFunc
	done        chan struct{}
	mu          sync.Mutex
	tickMu      sync.Mutex
}

func NewScheduler(store application.AutomationStore, service *application.AutomationService, sources application.AutomationSource, members application.AutomationMemberScanner, telemetry application.WorkflowTelemetry, clock SchedulerClock, workflows []domain.AutomationWorkflow, poll, lease time.Duration, workers int) (*Scheduler, error) {
	if store == nil || service == nil || clock == nil || poll < time.Second || lease < 10*time.Second || workers < 1 || workers > 32 {
		return nil, errors.New("invalid scheduler dependencies or limits")
	}
	for _, workflow := range workflows {
		if err := service.Validate(workflow); err != nil {
			return nil, fmt.Errorf("validate workflow %s: %w", workflow.ID, err)
		}
	}
	identifier := make([]byte, 16)
	if _, err := rand.Read(identifier); err != nil {
		return nil, errors.New("generate scheduler worker identity")
	}
	return &Scheduler{store: store, service: service, sources: sources, members: members, telemetry: telemetry, clock: clock, workflows: append([]domain.AutomationWorkflow(nil), workflows...), poll: poll, lease: lease, workers: workers, workerID: hex.EncodeToString(identifier)}, nil
}

func (s *Scheduler) Name() domain.Adapter { return "automation" }
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return errors.New("scheduler already started")
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel, s.done = cancel, make(chan struct{})
	go s.loop(runCtx)
	return nil
}
func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.cancel = nil
	s.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Scheduler) loop(ctx context.Context) {
	defer close(s.done)
	ticker := s.clock.NewTicker(s.poll)
	defer ticker.Stop()
	_ = s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			_ = s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) error {
	s.tickMu.Lock()
	defer s.tickMu.Unlock()
	resumeErr := s.resumeRuns(ctx)
	semaphore := make(chan struct{}, s.workers)
	var wait sync.WaitGroup
	errorsChannel := make(chan error, len(s.workflows))
	for _, workflow := range s.workflows {
		workflow := workflow
		if workflow.Trigger.Kind == domain.TriggerManual || !workflow.Enabled {
			continue
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
				if err := s.tickWorkflow(ctx, workflow); err != nil {
					errorsChannel <- err
				}
			case <-ctx.Done():
				errorsChannel <- ctx.Err()
			}
		}()
	}
	wait.Wait()
	close(errorsChannel)
	result := resumeErr
	for err := range errorsChannel {
		result = errors.Join(result, err)
	}
	return result
}

func (s *Scheduler) resumeRuns(ctx context.Context) error {
	runs, err := s.store.ListRuns()
	if err != nil {
		return err
	}
	s.observe(ctx, "", "queue_depth", "observed")
	var result error
	for _, run := range runs {
		now := s.clock.Now()
		eligible := run.State == domain.AutomationRunQueued || (run.State == domain.AutomationRunRetryWaiting && !run.NextAttempt.After(now)) || ((run.State == domain.AutomationRunLeased || run.State == domain.AutomationRunRunning) && !run.Lease.Expires.After(now))
		if !eligible {
			continue
		}
		workflow, loadErr := s.store.LoadDefinition(run.WorkflowHash)
		if loadErr != nil {
			result = errors.Join(result, loadErr)
			continue
		}
		request := s.runRequest(workflow, run.Provenance.TriggerID)
		request.Recovery = run.RenderedContent != ""
		resumed, runErr := s.service.Run(ctx, request)
		if runErr != nil {
			result = errors.Join(result, runErr)
			continue
		}
		if resumed.State == domain.AutomationRunUncertain {
			s.observe(ctx, workflow.ID, "partial_failure", "uncertain")
		}
	}
	return result
}

func (s *Scheduler) tickWorkflow(ctx context.Context, workflow domain.AutomationWorkflow) error {
	hash, err := workflow.Hash()
	if err != nil {
		return err
	}
	now := s.clock.Now().UTC()
	schedule, err := s.store.LoadSchedule(workflow.ID)
	if err != nil {
		schedule = domain.AutomationSchedule{WorkflowID: workflow.ID, WorkflowHash: hash, NextRun: initialAutomationRun(workflow.Trigger, now)}
		if err := s.store.SaveSchedule(ctx, 0, schedule); err != nil {
			if loaded, loadErr := s.store.LoadSchedule(workflow.ID); loadErr == nil {
				schedule = loaded
			} else {
				return err
			}
		} else {
			schedule, _ = s.store.LoadSchedule(workflow.ID)
		}
	}
	if schedule.WorkflowHash != hash {
		return errors.New("scheduled workflow definition changed without operator reconciliation")
	}
	if schedule.NextRun.After(now) {
		return nil
	}
	if s.telemetry != nil {
		s.telemetry.ObserveWorkflow(ctx, application.WorkflowObservation{WorkflowID: workflow.ID, Event: "schedule_due", Lag: now.Sub(schedule.NextRun)})
	}
	switch workflow.Trigger.Kind {
	case domain.TriggerNews:
		if s.sources == nil {
			return errors.New("news workflow source is unavailable")
		}
		batch, fetchErr := s.sources.Fetch(ctx, workflow.Trigger.Source, schedule.Cursor, workflow.Trigger.Limit)
		if fetchErr != nil {
			s.observe(ctx, workflow.ID, "source_failure", "failed")
			return fetchErr
		}
		for _, item := range batch.Items {
			request := s.runRequest(workflow, "source:"+item.ID)
			request.Item = &item
			run, runErr := s.service.Run(ctx, request)
			if runErr != nil || run.State != domain.AutomationRunSucceeded {
				return errors.Join(runErr, errors.New("news item publication incomplete"))
			}
		}
		if batch.NotModified || len(batch.Items) == 0 {
			s.observe(ctx, workflow.ID, "deduplicated", "no_new_items")
		}
		schedule.Cursor = batch.Cursor
	case domain.TriggerMemberScan:
		if err := s.scanMembers(ctx, workflow, &schedule); err != nil {
			return err
		}
	default:
		triggerID := "scheduled:" + strconv.FormatInt(schedule.NextRun.Unix(), 10)
		run, runErr := s.service.Run(ctx, s.runRequest(workflow, triggerID))
		if runErr != nil {
			return runErr
		}
		if run.State == domain.AutomationRunUncertain {
			return errors.New("scheduled mutation outcome is uncertain")
		}
	}
	schedule.NextRun = nextAutomationRun(workflow.Trigger, schedule.NextRun, now)
	return s.store.SaveSchedule(ctx, schedule.Revision, schedule)
}

type memberScanCursor struct {
	After      string `json:"after,omitempty"`
	Baselining bool   `json:"baselining"`
}

func (s *Scheduler) scanMembers(ctx context.Context, workflow domain.AutomationWorkflow, schedule *domain.AutomationSchedule) error {
	if s.members == nil {
		return errors.New("member scanner is unavailable")
	}
	cursors := map[string]memberScanCursor{}
	if schedule.Cursor == "" {
		for _, guild := range workflow.Targets.GuildIDs {
			cursors[guild] = memberScanCursor{Baselining: true}
		}
	} else {
		decoder := json.NewDecoder(strings.NewReader(schedule.Cursor))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&cursors); err != nil {
			return errors.New("member cursor is malformed")
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return errors.New("member cursor is malformed")
		}
	}
	for _, guild := range workflow.Targets.GuildIDs {
		cursor, ok := cursors[guild]
		if !ok {
			return errors.New("member cursor guild binding is missing")
		}
		members, err := s.members.ListAutomationMembers(ctx, domain.GuildID(guild), cursor.After, workflow.Trigger.Limit)
		if err != nil {
			return err
		}
		sort.Slice(members, func(i, j int) bool { return snowflakeLess(string(members[i].User.ID), string(members[j].User.ID)) })
		for _, member := range members {
			id := string(member.User.ID)
			if cursor.After != "" && !snowflakeLess(cursor.After, id) {
				continue
			}
			if !cursor.Baselining {
				request := s.runRequest(workflow, "member:"+guild+":"+id)
				request.MemberID = id
				request.MemberRoleIDs = make([]string, len(member.Roles))
				for index, role := range member.Roles {
					request.MemberRoleIDs[index] = string(role)
				}
				run, runErr := s.service.Run(ctx, request)
				if runErr != nil || run.State != domain.AutomationRunSucceeded {
					return errors.Join(runErr, errors.New("member workflow incomplete"))
				}
			}
			cursor.After = id
		}
		if cursor.Baselining && len(members) < workflow.Trigger.Limit {
			cursor.Baselining = false
		}
		cursors[guild] = cursor
	}
	encoded, err := json.Marshal(cursors)
	if err != nil {
		return err
	}
	schedule.Cursor = string(encoded)
	return nil
}

func snowflakeLess(left, right string) bool {
	if len(left) != len(right) {
		return len(left) < len(right)
	}
	return left < right
}

func initialAutomationRun(trigger domain.AutomationTrigger, now time.Time) time.Time {
	if trigger.Kind == domain.TriggerOnce {
		parsed, _ := time.Parse(time.RFC3339, trigger.At)
		return parsed.UTC()
	}
	if trigger.Kind == domain.TriggerCalendar {
		return nextCalendarRun(trigger, now.Add(-24*time.Hour), now)
	}
	return now
}
func nextAutomationRun(trigger domain.AutomationTrigger, previous, now time.Time) time.Time {
	switch trigger.Kind {
	case domain.TriggerOnce:
		return automationNever()
	case domain.TriggerInterval:
		duration, _ := time.ParseDuration(trigger.Every)
		next := previous.Add(duration)
		for !next.After(now) {
			next = next.Add(duration)
		}
		return next
	case domain.TriggerCalendar:
		return nextCalendarRun(trigger, previous, now)
	case domain.TriggerNews:
		return now.Add(time.Minute)
	case domain.TriggerMemberScan:
		duration, _ := time.ParseDuration(trigger.ScanEvery)
		return now.Add(duration)
	default:
		return automationNever()
	}
}

func automationNever() time.Time { return time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC) }
func nextCalendarRun(trigger domain.AutomationTrigger, previous, now time.Time) time.Time {
	clock, _ := time.Parse("15:04", trigger.Time)
	allowed := map[string]bool{}
	for _, day := range trigger.Weekdays {
		allowed[day] = true
	}
	for offset := 0; offset < 8; offset++ {
		day := now.AddDate(0, 0, offset)
		candidate := time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, time.UTC)
		if candidate.After(previous) && candidate.After(now) && (len(allowed) == 0 || allowed[strings.ToLower(candidate.Weekday().String())]) {
			return candidate
		}
	}
	return now.Add(7 * 24 * time.Hour)
}
func (s *Scheduler) runRequest(workflow domain.AutomationWorkflow, triggerID string) application.AutomationRunRequest {
	return application.AutomationRunRequest{Workflow: workflow, TriggerID: triggerID, LeaseOwner: "scheduler-" + s.workerID, LeaseToken: schedulerLeaseToken(s.workerID, workflow.ID, triggerID), LeaseDuration: s.lease}
}

func schedulerLeaseToken(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:16])
}

func (s *Scheduler) observe(ctx context.Context, workflowID, event, outcome string) {
	if s.telemetry != nil {
		s.telemetry.ObserveWorkflow(ctx, application.WorkflowObservation{WorkflowID: workflowID, Event: event, Outcome: outcome})
	}
}

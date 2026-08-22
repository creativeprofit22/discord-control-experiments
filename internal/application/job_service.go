package application

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type JobStatus string

const (
	JobPending         JobStatus = "pending"
	JobRunning         JobStatus = "running"
	JobCompleted       JobStatus = "completed"
	JobFailed          JobStatus = "failed"
	JobPartial         JobStatus = "partial"
	JobCancelRequested JobStatus = "cancel_requested"
)

type JobRecord struct {
	RunID          domain.RunID    `json:"runId"`
	GuildID        domain.GuildID  `json:"guildId"`
	PlanHashPrefix string          `json:"planHashPrefix"`
	Status         JobStatus       `json:"status"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	CompletedSteps int             `json:"completedSteps"`
	TotalSteps     int             `json:"totalSteps"`
	FailureClass   string          `json:"failureClass,omitempty"`
	Recoverable    bool            `json:"recoverable"`
	PlanHash       domain.PlanHash `json:"-"`
}

type JobStore interface {
	ListJobRecords(context.Context) ([]JobRecord, error)
	GetJobRecord(context.Context, domain.RunID) (JobRecord, error)
	SaveJobCancellation(context.Context, domain.RunID, time.Time) error
}
type JobRecovery interface {
	Resume(context.Context, ResumeRequest) (domain.ApplyResult, error)
}

type JobService struct {
	store    JobStore
	recovery JobRecovery
	clock    Clock
	mu       sync.Mutex
	live     map[domain.RunID]context.CancelFunc
}

func NewJobService(store JobStore, recovery JobRecovery, clock Clock) (*JobService, error) {
	if store == nil || recovery == nil || clock == nil {
		return nil, errors.New("job service dependencies are required")
	}
	return &JobService{store: store, recovery: recovery, clock: clock, live: map[domain.RunID]context.CancelFunc{}}, nil
}
func (s *JobService) List(ctx context.Context) ([]JobRecord, error) {
	records, err := s.store.ListJobRecords(ctx)
	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return records[i].RunID < records[j].RunID
		}
		return records[i].CreatedAt.Before(records[j].CreatedAt)
	})
	return records, err
}
func (s *JobService) Get(ctx context.Context, runID domain.RunID) (JobRecord, error) {
	if runID == "" {
		return JobRecord{}, &domain.ValidationError{Field: "run_id", Rule: "required"}
	}
	return s.store.GetJobRecord(ctx, runID)
}
func (s *JobService) Cancel(ctx context.Context, runID domain.RunID) (JobRecord, error) {
	if runID == "" {
		return JobRecord{}, &domain.ValidationError{Field: "run_id", Rule: "required"}
	}
	if err := s.store.SaveJobCancellation(ctx, runID, s.clock.Now()); err != nil {
		return JobRecord{}, err
	}
	s.mu.Lock()
	cancel := s.live[runID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return s.store.GetJobRecord(ctx, runID)
}
func (s *JobService) RegisterLive(runID domain.RunID, cancel context.CancelFunc) (unregister func()) {
	if runID == "" || cancel == nil {
		return func() {}
	}
	s.mu.Lock()
	s.live[runID] = cancel
	s.mu.Unlock()
	return func() { s.mu.Lock(); delete(s.live, runID); s.mu.Unlock() }
}
func (s *JobService) Resume(ctx context.Context, request ResumeRequest) (domain.ApplyResult, error) {
	record, err := s.store.GetJobRecord(ctx, request.RunID)
	if err != nil {
		return domain.ApplyResult{}, err
	}
	if request.GuildID != record.GuildID || request.PlanHash != record.PlanHash {
		return domain.ApplyResult{}, &domain.StalePlanError{Reason: "job guild or plan hash mismatch"}
	}
	return s.recovery.Resume(ctx, request)
}

func PlanHashPrefix(hash domain.PlanHash) string {
	value := string(hash)
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

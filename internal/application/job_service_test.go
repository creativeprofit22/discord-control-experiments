package application

import (
	"context"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type jobStoreFixture struct {
	record   JobRecord
	canceled bool
}

func (s *jobStoreFixture) ListJobRecords(context.Context) ([]JobRecord, error) {
	return []JobRecord{s.record}, nil
}
func (s *jobStoreFixture) GetJobRecord(context.Context, domain.RunID) (JobRecord, error) {
	if s.canceled {
		s.record.Status = JobCancelRequested
	}
	return s.record, nil
}
func (s *jobStoreFixture) SaveJobCancellation(context.Context, domain.RunID, time.Time) error {
	s.canceled = true
	return nil
}

type jobRecoveryFixture struct{ calls int }

func (r *jobRecoveryFixture) Resume(context.Context, ResumeRequest) (domain.ApplyResult, error) {
	r.calls++
	return domain.ApplyResult{RunID: "run"}, nil
}

func TestJobServiceContract(t *testing.T) {
	store := &jobStoreFixture{record: JobRecord{RunID: "run", GuildID: "1", PlanHash: "hash", PlanHashPrefix: "hash", Recoverable: true}}
	recovery := &jobRecoveryFixture{}
	service, err := NewJobService(store, recovery, reconciliationClock{now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	liveCanceled := false
	unregister := service.RegisterLive("run", func() { liveCanceled = true })
	defer unregister()
	record, err := service.Cancel(context.Background(), "run")
	if err != nil || !liveCanceled || record.Status != JobCancelRequested {
		t.Fatalf("record=%#v canceled=%v err=%v", record, liveCanceled, err)
	}
	_, err = service.Resume(context.Background(), ResumeRequest{GuildID: "1", RunID: "run", PlanHash: "different"})
	if err == nil || recovery.calls != 0 {
		t.Fatalf("mismatch resumed: calls=%d err=%v", recovery.calls, err)
	}
	_, err = service.Resume(context.Background(), ResumeRequest{GuildID: "1", RunID: "run", PlanHash: "hash"})
	if err != nil || recovery.calls != 1 {
		t.Fatalf("resume calls=%d err=%v", recovery.calls, err)
	}
}

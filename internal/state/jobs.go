package state

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const maxJobRecords = 1000

type cancellationRecord struct {
	RunID       domain.RunID `json:"run_id"`
	RequestedAt time.Time    `json:"requested_at"`
}

func (s *Store) ListJobRecords(ctx context.Context) ([]application.JobRecord, error) {
	guildRoot := filepath.Join(s.paths.Root(), "guilds")
	entries, err := os.ReadDir(guildRoot)
	if os.IsNotExist(err) {
		return []application.JobRecord{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list job state: %w", err)
	}
	records := []application.JobRecord{}
	for _, guildEntry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !guildEntry.IsDir() || !decimalID(guildEntry.Name()) {
			continue
		}
		guildID := domain.GuildID(guildEntry.Name())
		directory := filepath.Join(guildRoot, guildEntry.Name())
		files, readErr := os.ReadDir(directory)
		if readErr != nil {
			return nil, fmt.Errorf("list guild jobs: %w", readErr)
		}
		for _, file := range files {
			name := file.Name()
			if file.Type()&os.ModeSymlink != 0 || file.IsDir() || !strings.HasPrefix(name, "plan-") || !strings.HasSuffix(name, ".json") {
				continue
			}
			runID := domain.RunID(strings.TrimSuffix(strings.TrimPrefix(name, "plan-"), ".json"))
			if !safeComponent(string(runID)) {
				continue
			}
			record, recordErr := s.jobRecord(guildID, runID)
			if recordErr != nil {
				return nil, recordErr
			}
			records = append(records, record)
			if len(records) > maxJobRecords {
				return nil, fmt.Errorf("job record limit exceeded")
			}
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].RunID < records[j].RunID })
	return records, nil
}

func (s *Store) GetJobRecord(ctx context.Context, runID domain.RunID) (application.JobRecord, error) {
	if !safeComponent(string(runID)) {
		return application.JobRecord{}, &domain.ValidationError{Field: "run_id", Rule: "invalid"}
	}
	records, err := s.ListJobRecords(ctx)
	if err != nil {
		return application.JobRecord{}, err
	}
	var match *application.JobRecord
	for index := range records {
		if records[index].RunID == runID {
			if match != nil {
				return application.JobRecord{}, fmt.Errorf("duplicate run ID")
			}
			copy := records[index]
			match = &copy
		}
	}
	if match == nil {
		return application.JobRecord{}, os.ErrNotExist
	}
	return *match, nil
}

func (s *Store) SaveJobCancellation(ctx context.Context, runID domain.RunID, requestedAt time.Time) error {
	record, err := s.GetJobRecord(ctx, runID)
	if err != nil {
		return err
	}
	directory, err := s.paths.GuildDirectory(record.GuildID)
	if err != nil {
		return err
	}
	target := filepath.Join(directory, "cancel-"+string(runID)+".json")
	if _, err := os.Lstat(target); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	payload, err := json.Marshal(cancellationRecord{RunID: runID, RequestedAt: requestedAt.UTC()})
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".cancel-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(name)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, target); err != nil {
		if _, statErr := os.Stat(target); statErr == nil {
			return nil
		}
		return err
	}
	committed = true
	return syncDirectory(directory)
}

func (s *Store) jobRecord(guildID domain.GuildID, runID domain.RunID) (application.JobRecord, error) {
	plan, err := s.LoadPlan(guildID, runID)
	if err != nil {
		return application.JobRecord{}, err
	}
	record := application.JobRecord{RunID: runID, GuildID: guildID, PlanHash: plan.Hash, PlanHashPrefix: application.PlanHashPrefix(plan.Hash), Status: application.JobPending, CreatedAt: plan.CreatedAt.UTC(), UpdatedAt: plan.CreatedAt.UTC(), TotalSteps: len(plan.Operations), Recoverable: true}
	journalPath, err := s.paths.JournalPath(guildID, runID)
	if err != nil {
		return application.JobRecord{}, err
	}
	if _, statErr := os.Stat(journalPath); statErr == nil {
		events, replayErr := ReplayJournal(journalPath, guildID, plan.Hash)
		if replayErr != nil {
			return application.JobRecord{}, replayErr
		}
		for _, event := range events {
			if event.OccurredAt.After(record.UpdatedAt) {
				record.UpdatedAt = event.OccurredAt.UTC()
			}
			switch event.Type {
			case domain.JournalRunStarted:
				record.Status = application.JobRunning
			case domain.JournalStepComplete:
				record.CompletedSteps++
			case domain.JournalRunComplete:
				record.Status, record.Recoverable = application.JobCompleted, false
			case domain.JournalRunFailed:
				record.Status, record.FailureClass = application.JobFailed, sanitizedFailureClass(event.ErrorCode)
			}
		}
	} else if !os.IsNotExist(statErr) {
		return application.JobRecord{}, statErr
	}
	cancelName := "cancel-" + string(runID) + ".json"
	cancelFile, readErr := os.OpenInRoot(filepath.Dir(journalPath), cancelName)
	if readErr == nil {
		data, limitErr := io.ReadAll(io.LimitReader(cancelFile, 4097))
		closeErr := cancelFile.Close()
		if limitErr != nil || closeErr != nil {
			return application.JobRecord{}, fmt.Errorf("read cancellation record")
		}
		var cancellation cancellationRecord
		if len(data) > 4096 || json.Unmarshal(data, &cancellation) != nil || cancellation.RunID != runID {
			return application.JobRecord{}, fmt.Errorf("invalid cancellation record")
		}
		record.Status = application.JobCancelRequested
		if cancellation.RequestedAt.After(record.UpdatedAt) {
			record.UpdatedAt = cancellation.RequestedAt.UTC()
		}
	} else if !os.IsNotExist(readErr) {
		return application.JobRecord{}, readErr
	}
	if record.Status == application.JobRunning && record.CompletedSteps > 0 {
		record.Status = application.JobPartial
	}
	return record, nil
}

func sanitizedFailureClass(value string) string {
	if value == "" {
		return "unknown"
	}
	if len(value) > 64 {
		return "unknown"
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return "unknown"
		}
	}
	return value
}

var _ application.JobStore = (*Store)(nil)

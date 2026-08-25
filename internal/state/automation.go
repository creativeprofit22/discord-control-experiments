package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const maxAutomationProjectionBytes = 8 << 20

var ErrAutomationConflict = errors.New("automation projection revision conflict")

type AutomationStore struct{ paths Paths }

func NewAutomationStore(paths Paths) *AutomationStore { return &AutomationStore{paths: paths} }

func (s *AutomationStore) withLock(ctx context.Context, operation func() error) error {
	path, err := s.paths.AutomationLockPath()
	if err != nil {
		return err
	}
	// simplification: one host-wide file lock serializes transitions; use a transactional distributed store before multi-host execution.
	lock, err := acquireFileLock(ctx, s.paths.root, path)
	if err != nil {
		return fmt.Errorf("acquire automation lock: %w", err)
	}
	defer releaseFileLock(lock)
	return operation()
}

func (s *AutomationStore) SaveDefinition(ctx context.Context, workflow domain.AutomationWorkflow) (string, error) {
	hash, err := workflow.Hash()
	if err != nil {
		return "", err
	}
	path, err := s.paths.AutomationPath("definition", hash, ".json")
	if err != nil {
		return "", err
	}
	err = s.withLock(ctx, func() error {
		data, readErr := readRootFile(s.paths.root, path)
		if readErr == nil {
			var existing domain.AutomationWorkflow
			if decodeAutomationJSON(data, &existing) != nil {
				return errors.New("stored automation definition is malformed")
			}
			existingHash, hashErr := existing.Hash()
			if hashErr != nil || existingHash != hash {
				return errors.New("stored automation definition hash mismatch")
			}
			return nil
		}
		if !os.IsNotExist(readErr) {
			return readErr
		}
		return atomicAutomationJSON(path, workflow)
	})
	return hash, err
}

func (s *AutomationStore) LoadDefinition(hash string) (domain.AutomationWorkflow, error) {
	path, err := s.paths.AutomationPath("definition", hash, ".json")
	if err != nil {
		return domain.AutomationWorkflow{}, err
	}
	var workflow domain.AutomationWorkflow
	if err := readAutomationJSON(path, &workflow); err != nil {
		return workflow, err
	}
	actual, err := workflow.Hash()
	if err != nil || actual != hash {
		return workflow, errors.New("automation definition hash mismatch")
	}
	return workflow, nil
}

func (s *AutomationStore) SaveSchedule(ctx context.Context, expected uint64, schedule domain.AutomationSchedule) error {
	if !safeComponent(schedule.WorkflowID) || !safeComponent(schedule.WorkflowHash) {
		return errors.New("invalid automation schedule identity")
	}
	path, err := s.paths.AutomationPath("schedule", schedule.WorkflowID, ".json")
	if err != nil {
		return err
	}
	return s.withLock(ctx, func() error {
		current := domain.AutomationSchedule{}
		err := readAutomationJSON(path, &current)
		if os.IsNotExist(err) {
			if expected != 0 {
				return ErrAutomationConflict
			}
		} else if err != nil {
			return err
		} else if current.Revision != expected {
			return ErrAutomationConflict
		}
		schedule.Revision = expected + 1
		return atomicAutomationJSON(path, schedule)
	})
}

func (s *AutomationStore) LoadSchedule(workflowID string) (domain.AutomationSchedule, error) {
	path, err := s.paths.AutomationPath("schedule", workflowID, ".json")
	if err != nil {
		return domain.AutomationSchedule{}, err
	}
	var schedule domain.AutomationSchedule
	return schedule, readAutomationJSON(path, &schedule)
}

func (s *AutomationStore) EnqueueRun(ctx context.Context, run domain.AutomationRun) (bool, error) {
	if !safeComponent(run.ID) || !safeComponent(run.WorkflowHash) || !safeComponent(run.IdempotencyKey) {
		return false, errors.New("invalid automation run identity")
	}
	path, err := s.paths.AutomationPath("run", run.ID, ".json")
	if err != nil {
		return false, err
	}
	created := false
	err = s.withLock(ctx, func() error {
		var existing domain.AutomationRun
		if readErr := readAutomationJSON(path, &existing); readErr == nil {
			if existing.IdempotencyKey != run.IdempotencyKey || existing.WorkflowHash != run.WorkflowHash {
				return errors.New("automation run identity collision")
			}
			return nil
		} else if !os.IsNotExist(readErr) {
			return readErr
		}
		run.Revision, run.State = 1, domain.AutomationRunQueued
		created = true
		return atomicAutomationJSON(path, run)
	})
	return created, err
}

func (s *AutomationStore) LoadRun(runID string) (domain.AutomationRun, error) {
	path, err := s.paths.AutomationPath("run", runID, ".json")
	if err != nil {
		return domain.AutomationRun{}, err
	}
	var run domain.AutomationRun
	return run, readAutomationJSON(path, &run)
}

func (s *AutomationStore) CompareAndSwapRun(ctx context.Context, expected uint64, next domain.AutomationRun) error {
	path, err := s.paths.AutomationPath("run", next.ID, ".json")
	if err != nil {
		return err
	}
	return s.withLock(ctx, func() error {
		var current domain.AutomationRun
		if err := readAutomationJSON(path, &current); err != nil {
			return err
		}
		if current.Revision != expected {
			return ErrAutomationConflict
		}
		if current.State != next.State && !domain.ValidAutomationTransition(current.State, next.State) {
			return errors.New("illegal automation run transition")
		}
		if current.WorkflowHash != next.WorkflowHash || current.IdempotencyKey != next.IdempotencyKey {
			return errors.New("immutable automation run binding changed")
		}
		next.Revision = expected + 1
		if len(next.Error) > 1024 {
			next.Error = next.Error[:1024]
		}
		return atomicAutomationJSON(path, next)
	})
}

func (s *AutomationStore) CompareAndSwapOwnedRun(ctx context.Context, expected uint64, next domain.AutomationRun, owner, token string, now time.Time) error {
	path, err := s.paths.AutomationPath("run", next.ID, ".json")
	if err != nil {
		return err
	}
	return s.withLock(ctx, func() error {
		var current domain.AutomationRun
		if err := readAutomationJSON(path, &current); err != nil {
			return err
		}
		if current.Revision != expected {
			return ErrAutomationConflict
		}
		if current.Lease.Owner != owner || current.Lease.Token != token || !current.Lease.Expires.After(now) {
			return errors.New("automation lease ownership lost")
		}
		if current.State != next.State && !domain.ValidAutomationTransition(current.State, next.State) {
			return errors.New("illegal automation run transition")
		}
		if current.WorkflowHash != next.WorkflowHash || current.IdempotencyKey != next.IdempotencyKey {
			return errors.New("immutable automation run binding changed")
		}
		next.Revision = expected + 1
		if len(next.Error) > 1024 {
			next.Error = next.Error[:1024]
		}
		return atomicAutomationJSON(path, next)
	})
}

func (s *AutomationStore) ClaimRun(ctx context.Context, runID, owner, token string, now time.Time, duration time.Duration) (domain.AutomationRun, error) {
	if !safeComponent(owner) || !safeComponent(token) || duration <= 0 {
		return domain.AutomationRun{}, errors.New("invalid automation lease")
	}
	var claimed domain.AutomationRun
	path, err := s.paths.AutomationPath("run", runID, ".json")
	if err != nil {
		return claimed, err
	}
	err = s.withLock(ctx, func() error {
		var run domain.AutomationRun
		if err := readAutomationJSON(path, &run); err != nil {
			return err
		}
		if (run.State == domain.AutomationRunLeased || run.State == domain.AutomationRunRunning) && run.Lease.Expires.After(now) {
			return errors.New("automation run already leased")
		}
		if run.State != domain.AutomationRunQueued && run.State != domain.AutomationRunLeased && run.State != domain.AutomationRunRunning {
			return errors.New("automation run is not claimable")
		}
		run.Revision++
		run.State = domain.AutomationRunLeased
		run.Lease = domain.AutomationLease{Owner: owner, Token: token, Expires: now.Add(duration)}
		claimed = run
		return atomicAutomationJSON(path, run)
	})
	return claimed, err
}

func (s *AutomationStore) Heartbeat(ctx context.Context, runID, owner, token string, now time.Time, duration time.Duration) error {
	path, err := s.paths.AutomationPath("run", runID, ".json")
	if err != nil {
		return err
	}
	return s.withLock(ctx, func() error {
		var run domain.AutomationRun
		if err := readAutomationJSON(path, &run); err != nil {
			return err
		}
		if run.Lease.Owner != owner || run.Lease.Token != token || !run.Lease.Expires.After(now) {
			return errors.New("automation lease ownership lost")
		}
		run.Revision++
		run.Lease.Expires = now.Add(duration)
		return atomicAutomationJSON(path, run)
	})
}

func (s *AutomationStore) ListRuns() ([]domain.AutomationRun, error) {
	directory, err := s.paths.AutomationDirectory()
	if err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(filepath.Join(directory, "run-*.json"))
	if err != nil {
		return nil, err
	}
	runs := make([]domain.AutomationRun, 0, len(matches))
	for _, path := range matches {
		var run domain.AutomationRun
		if err := readAutomationJSON(path, &run); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].ID < runs[j].ID })
	return runs, nil
}

func (s *AutomationStore) DeadLetter(ctx context.Context, run domain.AutomationRun) error {
	if run.State != domain.AutomationRunDeadLetter {
		return errors.New("dead letter must be terminal")
	}
	path, err := s.paths.AutomationPath("dead", run.ID, ".json")
	if err != nil {
		return err
	}
	return s.withLock(ctx, func() error { return atomicAutomationJSON(path, run) })
}

func readAutomationJSON(path string, target any) error {
	data, err := os.ReadFile(path) // #nosec G304 -- callers construct contained paths through Paths.
	if err != nil {
		return err
	}
	if len(data) > maxAutomationProjectionBytes {
		return errors.New("automation projection exceeds size limit")
	}
	return decodeAutomationJSON(data, target)
}

func decodeAutomationJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode automation projection: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("automation projection contains trailing data")
	}
	return nil
}

func atomicAutomationJSON(path string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(encoded) > maxAutomationProjectionBytes {
		return errors.New("automation projection exceeds size limit")
	}
	if strings.Contains(string(encoded), "\n") {
		return errors.New("automation projection contains newline")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".automation-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if _, err := temporary.Write(encoded); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if err := temporary.Sync(); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := replaceAutomationFile(name, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

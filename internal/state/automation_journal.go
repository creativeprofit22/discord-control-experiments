package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const maxAutomationJournalBytes = 32 << 20

type AutomationJournal struct {
	file         *os.File
	runID        string
	workflowHash string
	sequence     uint64
	previousHash string
}

func OpenAutomationJournal(paths Paths, runID, workflowHash string) (*AutomationJournal, []domain.AutomationJournalEvent, error) {
	if !safeComponent(runID) || !safeComponent(workflowHash) {
		return nil, nil, errors.New("invalid automation journal binding")
	}
	path, err := paths.AutomationPath("journal", runID, ".jsonl")
	if err != nil {
		return nil, nil, err
	}
	events, repairOffset, err := replayAutomationJournal(path, runID, workflowHash)
	if err != nil {
		return nil, nil, err
	}
	if repairOffset >= 0 {
		repairFile, err := openRootFile(paths.root, path, os.O_WRONLY, 0o600)
		if err != nil {
			return nil, nil, err
		}
		if err := repairFile.Truncate(repairOffset); err != nil {
			return nil, nil, errors.Join(err, repairFile.Close())
		}
		if err := repairFile.Sync(); err != nil {
			return nil, nil, errors.Join(err, repairFile.Close())
		}
		if err := repairFile.Close(); err != nil {
			return nil, nil, err
		}
	}
	file, err := openRootFile(paths.root, path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	journal := &AutomationJournal{file: file, runID: runID, workflowHash: workflowHash}
	if len(events) > 0 {
		journal.sequence = events[len(events)-1].Sequence
		journal.previousHash = events[len(events)-1].Hash
	}
	return journal, events, nil
}

func (j *AutomationJournal) Append(state, stepID string, at time.Time) (domain.AutomationJournalEvent, error) {
	if j == nil || j.file == nil {
		return domain.AutomationJournalEvent{}, errors.New("automation journal is closed")
	}
	event := domain.AutomationJournalEvent{Sequence: j.sequence + 1, RunID: j.runID, WorkflowHash: j.workflowHash, State: state, StepID: stepID, At: at.UTC(), PreviousHash: j.previousHash}
	hash, err := hashAutomationJournalEvent(event)
	if err != nil {
		return event, err
	}
	event.Hash = hash
	encoded, err := json.Marshal(event)
	if err != nil {
		return event, err
	}
	encoded = append(encoded, '\n')
	if _, err := j.file.Write(encoded); err != nil {
		return event, err
	}
	if err := j.file.Sync(); err != nil {
		return event, err
	}
	j.sequence, j.previousHash = event.Sequence, event.Hash
	return event, nil
}

func (j *AutomationJournal) AppendDispatching(stepID string, at time.Time) (domain.AutomationJournalEvent, error) {
	if !safeComponent(stepID) {
		return domain.AutomationJournalEvent{}, errors.New("invalid automation step ID")
	}
	return j.Append("dispatching", stepID, at)
}

func (j *AutomationJournal) Close() error {
	if j == nil || j.file == nil {
		return nil
	}
	err := j.file.Close()
	j.file = nil
	return err
}

func ReplayAutomationJournal(path, runID, workflowHash string) ([]domain.AutomationJournalEvent, error) {
	events, _, err := replayAutomationJournal(path, runID, workflowHash)
	return events, err
}

func replayAutomationJournal(path, runID, workflowHash string) ([]domain.AutomationJournalEvent, int64, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is produced by contained Paths.
	if os.IsNotExist(err) {
		return nil, -1, nil
	}
	if err != nil {
		return nil, -1, err
	}
	if len(data) > maxAutomationJournalBytes {
		return nil, -1, errors.New("automation journal exceeds size limit")
	}
	lines := bytes.Split(data, []byte{'\n'})
	events := make([]domain.AutomationJournalEvent, 0, len(lines))
	previous := ""
	for index, line := range lines {
		if len(line) == 0 {
			continue
		}
		var event domain.AutomationJournalEvent
		if err := decodeAutomationJSON(line, &event); err != nil {
			if index == len(lines)-1 && data[len(data)-1] != '\n' {
				return events, int64(len(data) - len(line)), nil
			}
			return nil, -1, fmt.Errorf("automation journal record %d: %w", index+1, err)
		}
		if event.Sequence != uint64(len(events)+1) || event.RunID != runID || event.WorkflowHash != workflowHash || event.PreviousHash != previous {
			return nil, -1, errors.New("automation journal sequence or binding mismatch")
		}
		expected, err := hashAutomationJournalEvent(event)
		if err != nil || expected != event.Hash {
			return nil, -1, errors.New("automation journal hash mismatch")
		}
		previous = event.Hash
		events = append(events, event)
	}
	return events, -1, nil
}

func hashAutomationJournalEvent(event domain.AutomationJournalEvent) (string, error) {
	event.Hash = ""
	encoded, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

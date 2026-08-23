package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const maxLockdownPlanBytes = 8 << 20

type LockdownStore struct{ paths Paths }

func NewLockdownStore(paths Paths) *LockdownStore { return &LockdownStore{paths: paths} }

func (s *LockdownStore) SavePlan(plan domain.LockdownPlan) error {
	if err := plan.VerifyHash(); err != nil {
		return err
	}
	lock, err := AcquireGuildLock(s.paths, plan.GuildID)
	if err != nil {
		return err
	}
	defer lock.Close()
	path, err := s.paths.LockdownPlanPath(plan.GuildID, plan.Hash)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("lockdown plan already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	encoded, err := json.Marshal(plan)
	if err != nil || len(encoded) > maxLockdownPlanBytes {
		if err != nil {
			return err
		}
		return fmt.Errorf("lockdown plan exceeds size limit")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".lockdown-plan-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer temporary.Close()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(encoded); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func (s *LockdownStore) LoadPlan(guildID domain.GuildID, hash domain.LockdownPlanHash) (domain.LockdownPlan, error) {
	path, err := s.paths.LockdownPlanPath(guildID, hash)
	if err != nil {
		return domain.LockdownPlan{}, err
	}
	// #nosec G304 -- path is produced by contained Paths.LockdownPlanPath.
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.LockdownPlan{}, err
	}
	if len(data) > maxLockdownPlanBytes {
		return domain.LockdownPlan{}, fmt.Errorf("lockdown plan exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var plan domain.LockdownPlan
	if err := decoder.Decode(&plan); err != nil {
		return plan, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return plan, fmt.Errorf("lockdown plan contains trailing data")
	}
	if plan.GuildID != guildID || plan.Hash != hash {
		return plan, fmt.Errorf("lockdown plan binding mismatch")
	}
	if err := plan.VerifyHash(); err != nil {
		return plan, err
	}
	return plan, nil
}

func (s *LockdownStore) Append(guildID domain.GuildID, planHash domain.LockdownPlanHash, event domain.LockdownJournalEvent) (domain.LockdownJournalEvent, error) {
	lock, err := AcquireGuildLock(s.paths, guildID)
	if err != nil {
		return domain.LockdownJournalEvent{}, err
	}
	defer lock.Close()
	path, err := s.paths.LockdownJournalPath(guildID, planHash)
	if err != nil {
		return domain.LockdownJournalEvent{}, err
	}
	events, repairOffset, err := replayLockdownJournal(path, guildID, planHash)
	if err != nil {
		return domain.LockdownJournalEvent{}, err
	}
	if repairOffset >= 0 {
		if err := repairJournal(path, repairOffset); err != nil {
			return domain.LockdownJournalEvent{}, err
		}
	}
	if event.OccurredAt.IsZero() {
		return domain.LockdownJournalEvent{}, fmt.Errorf("lockdown event time is required")
	}
	event.GuildID, event.PlanHash, event.Sequence, event.PreviousHash, event.RecordHash = guildID, planHash, uint64(len(events)+1), "", ""
	if len(events) > 0 {
		event.PreviousHash = events[len(events)-1].RecordHash
	}
	hash, err := lockdownRecordHash(event)
	if err != nil {
		return event, err
	}
	event.RecordHash = hash
	encoded, err := json.Marshal(event)
	if err != nil {
		return event, err
	}
	if len(encoded) > MaxJournalRecordBytes {
		return event, fmt.Errorf("lockdown journal record exceeds size limit")
	}
	// #nosec G304 -- path is produced by contained Paths.LockdownJournalPath.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return event, err
	}
	defer file.Close()
	if err := checkPrivateFile(file); err != nil {
		return event, err
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return event, err
	}
	if err := file.Sync(); err != nil {
		return event, err
	}
	return event, nil
}

func (s *LockdownStore) Replay(guildID domain.GuildID, planHash domain.LockdownPlanHash) ([]domain.LockdownJournalEvent, error) {
	lock, err := AcquireGuildLock(s.paths, guildID)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	path, err := s.paths.LockdownJournalPath(guildID, planHash)
	if err != nil {
		return nil, err
	}
	events, _, err := replayLockdownJournal(path, guildID, planHash)
	return events, err
}

func replayLockdownJournal(path string, guildID domain.GuildID, planHash domain.LockdownPlanHash) ([]domain.LockdownJournalEvent, int64, error) {
	// #nosec G304 -- path is produced by contained Paths.LockdownJournalPath.
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []domain.LockdownJournalEvent{}, -1, nil
	}
	if err != nil {
		return nil, -1, err
	}
	if len(data) > MaxJournalBytes {
		return nil, -1, fmt.Errorf("lockdown journal exceeds size limit")
	}
	terminated := bytes.HasSuffix(data, []byte{'\n'})
	lines := bytes.Split(data, []byte{'\n'})
	events := make([]domain.LockdownJournalEvent, 0, len(lines))
	previous := ""
	for index, line := range lines {
		last := index == len(lines)-1
		if last && !terminated {
			if len(line) > MaxJournalRecordBytes {
				return nil, -1, fmt.Errorf("lockdown record exceeds size limit")
			}
			if event, decodeErr := decodeLockdownRecord(line); decodeErr == nil {
				if err := validateLockdownRecord(event, guildID, planHash, uint64(len(events)+1), previous, index+1); err != nil {
					return nil, -1, err
				}
			}
			return events, int64(len(data) - len(line)), nil
		}
		if len(line) == 0 {
			if last {
				continue
			}
			return nil, -1, fmt.Errorf("lockdown journal contains empty record")
		}
		event, err := decodeLockdownRecord(line)
		if err != nil {
			return nil, -1, fmt.Errorf("decode lockdown record %d: %w", index+1, err)
		}
		if err := validateLockdownRecord(event, guildID, planHash, uint64(len(events)+1), previous, index+1); err != nil {
			return nil, -1, err
		}
		events, previous = append(events, event), event.RecordHash
	}
	return events, -1, nil
}

func decodeLockdownRecord(line []byte) (domain.LockdownJournalEvent, error) {
	var event domain.LockdownJournalEvent
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return event, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return event, fmt.Errorf("lockdown record contains trailing data")
	}
	return event, nil
}

func validateLockdownRecord(event domain.LockdownJournalEvent, guildID domain.GuildID, planHash domain.LockdownPlanHash, sequence uint64, previous string, record int) error {
	if event.GuildID != guildID || event.PlanHash != planHash {
		return fmt.Errorf("lockdown journal binding mismatch at record %d", record)
	}
	if event.Sequence != sequence || event.PreviousHash != previous || event.OccurredAt.IsZero() {
		return fmt.Errorf("lockdown journal sequence mismatch at record %d", record)
	}
	if event.Type != domain.LockdownPlanPersisted && event.Type != domain.LockdownOperationStarted && event.Type != domain.LockdownOperationCompleted && event.Type != domain.LockdownOperationFailed {
		return fmt.Errorf("lockdown journal type invalid at record %d", record)
	}
	if event.Type != domain.LockdownPlanPersisted && event.OperationID == "" {
		return fmt.Errorf("lockdown operation ID required at record %d", record)
	}
	recorded := event.RecordHash
	event.RecordHash = ""
	expected, err := lockdownRecordHash(event)
	if err != nil {
		return err
	}
	if recorded == "" || recorded != expected {
		return fmt.Errorf("lockdown journal hash mismatch at record %d", record)
	}
	return nil
}

func lockdownRecordHash(event domain.LockdownJournalEvent) (string, error) {
	event.RecordHash = ""
	encoded, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

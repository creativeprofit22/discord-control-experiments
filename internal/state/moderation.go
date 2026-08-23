package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type ModerationStore struct{ paths Paths }

func NewModerationStore(paths Paths) *ModerationStore { return &ModerationStore{paths: paths} }

func (s *ModerationStore) Append(guildID domain.GuildID, event domain.ModerationJournalEvent) (domain.ModerationJournalEvent, error) {
	lock, err := AcquireGuildLock(s.paths, guildID)
	if err != nil {
		return domain.ModerationJournalEvent{}, err
	}
	defer lock.Close()
	path, err := s.paths.ModerationJournalPath(guildID)
	if err != nil {
		return domain.ModerationJournalEvent{}, err
	}
	return appendModerationRecord(path, guildID, event)
}

func (s *ModerationStore) Replay(guildID domain.GuildID) ([]domain.ModerationJournalEvent, error) {
	lock, err := AcquireGuildLock(s.paths, guildID)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	path, err := s.paths.ModerationJournalPath(guildID)
	if err != nil {
		return nil, err
	}
	events, _, err := replayModerationJournal(path, guildID)
	return events, err
}

func (s *ModerationStore) Compact(guildID domain.GuildID, retention time.Duration, now time.Time) error {
	if retention <= 0 || now.IsZero() {
		return fmt.Errorf("retention and current time are required")
	}
	lock, err := AcquireGuildLock(s.paths, guildID)
	if err != nil {
		return err
	}
	defer lock.Close()
	path, err := s.paths.ModerationJournalPath(guildID)
	if err != nil {
		return err
	}
	events, _, err := replayModerationJournal(path, guildID)
	if err != nil {
		return err
	}
	compacted := compactModerationEvents(events, retention, now)
	return replaceModerationJournal(path, guildID, compacted)
}

func compactModerationEvents(events []domain.ModerationJournalEvent, retention time.Duration, now time.Time) []domain.ModerationJournalEvent {
	cases := map[domain.CaseID]domain.Case{}
	latestAction := map[domain.ModerationActionID]uint64{}
	for _, event := range events {
		if event.Case != nil {
			cases[event.Case.ID] = *event.Case
		}
		if event.Action != nil {
			latestAction[event.Action.ID] = event.Sequence
		}
	}
	cutoff := now.Add(-retention)
	result := make([]domain.ModerationJournalEvent, 0, len(events))
	for _, event := range events {
		originalSequence := event.Sequence
		event.Sequence, event.PreviousHash, event.RecordHash = 0, "", ""
		switch {
		case event.Evidence != nil && (event.Evidence.ExpiresAt != nil && !event.Evidence.ExpiresAt.After(now) || event.OccurredAt.Before(cutoff)):
			continue
		case event.Action != nil:
			if latestAction[event.Action.ID] != originalSequence {
				continue
			}
			caseValue, closed := cases[event.Action.CaseID]
			if closed && caseValue.Status == domain.CaseClosed && event.OccurredAt.Before(cutoff) {
				result = append(result, domain.ModerationJournalEvent{GuildID: event.GuildID, Type: domain.ModerationRetentionTombstone, OccurredAt: now, Tombstone: &domain.ModerationTombstone{CaseID: event.Action.CaseID, ActionID: event.Action.ID, ActionKind: event.Action.Kind, IdempotencyKey: event.Action.IdempotencyKey, IntentHash: actionIntentHash(*event.Action), Result: event.Action.Result, CorrelationID: event.Action.CorrelationID, RetainedAt: now}})
				continue
			}
		}
		result = append(result, event)
	}
	return result
}

func replaceModerationJournal(path string, guildID domain.GuildID, events []domain.ModerationJournalEvent) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".moderation-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer temporary.Close()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	previousHash := ""
	for index, event := range events {
		event.GuildID, event.Sequence, event.PreviousHash, event.RecordHash = guildID, uint64(index+1), previousHash, ""
		hash, hashErr := moderationRecordHash(event)
		if hashErr != nil {
			return hashErr
		}
		event.RecordHash, previousHash = hash, hash
		encoded, encodeErr := json.Marshal(event)
		if encodeErr != nil || len(encoded) > MaxJournalRecordBytes {
			if encodeErr != nil {
				return encodeErr
			}
			return fmt.Errorf("compacted moderation record exceeds size limit")
		}
		if _, err := temporary.Write(append(encoded, '\n')); err != nil {
			return err
		}
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	previous := path + ".previous"
	_ = os.Remove(previous)
	if _, statErr := os.Stat(path); statErr == nil {
		if err := os.Rename(path, previous); err != nil {
			return err
		}
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Rename(previous, path)
		return err
	}
	if runtime.GOOS != "windows" {
		if err := syncDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func (s *ModerationStore) AppealExport(guildID domain.GuildID, subject domain.UserID, now time.Time) (domain.AppealExport, error) {
	events, err := s.Replay(guildID)
	if err != nil {
		return domain.AppealExport{}, err
	}
	cases := map[domain.CaseID]domain.Case{}
	actions := map[domain.CaseID]map[domain.ModerationActionID]domain.ModerationAction{}
	evidence := map[domain.CaseID][]domain.EvidenceReference{}
	for _, event := range events {
		if event.Case != nil && event.Case.SubjectUserID == subject {
			cases[event.Case.ID] = *event.Case
		}
		if event.Action != nil && event.Action.SubjectUserID == subject {
			if actions[event.Action.CaseID] == nil {
				actions[event.Action.CaseID] = map[domain.ModerationActionID]domain.ModerationAction{}
			}
			actions[event.Action.CaseID][event.Action.ID] = *event.Action
		}
		if event.Evidence != nil && event.Evidence.SubjectUserID == subject && (event.Evidence.ExpiresAt == nil || event.Evidence.ExpiresAt.After(now)) {
			evidence[event.Evidence.CaseID] = append(evidence[event.Evidence.CaseID], *event.Evidence)
		}
	}
	result := domain.AppealExport{SchemaVersion: "discord-control/appeal-export-v1", GuildID: guildID, SubjectUserID: subject, GeneratedAt: now, Cases: []domain.AppealCase{}}
	for _, value := range cases {
		appealCase := domain.AppealCase{ID: value.ID, ReasonCode: value.ReasonCode, Status: value.Status, CreatedAt: value.CreatedAt, ClosedAt: value.ClosedAt, Actions: []domain.AppealAction{}, Evidence: evidence[value.ID]}
		for _, action := range actions[value.ID] {
			appealCase.Actions = append(appealCase.Actions, domain.AppealAction{Kind: action.Kind, Status: action.Status, ReasonCode: action.ReasonCode, CreatedAt: action.CreatedAt, ExpiresAt: action.ExpiresAt})
		}
		sort.Slice(appealCase.Actions, func(i, j int) bool {
			if !appealCase.Actions[i].CreatedAt.Equal(appealCase.Actions[j].CreatedAt) {
				return appealCase.Actions[i].CreatedAt.Before(appealCase.Actions[j].CreatedAt)
			}
			return appealCase.Actions[i].Kind < appealCase.Actions[j].Kind
		})
		sort.Slice(appealCase.Evidence, func(i, j int) bool { return appealCase.Evidence[i].ID < appealCase.Evidence[j].ID })
		result.Cases = append(result.Cases, appealCase)
	}
	sort.Slice(result.Cases, func(i, j int) bool { return result.Cases[i].ID < result.Cases[j].ID })
	return result, nil
}

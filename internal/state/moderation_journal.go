package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func appendModerationRecord(path string, guildID domain.GuildID, event domain.ModerationJournalEvent) (domain.ModerationJournalEvent, error) {
	events, repairOffset, err := replayModerationJournal(path, guildID)
	if err != nil {
		return domain.ModerationJournalEvent{}, err
	}
	if repairOffset >= 0 {
		if err := repairJournal(path, repairOffset); err != nil {
			return domain.ModerationJournalEvent{}, err
		}
	}
	if err := validateModerationPayload(event, guildID); err != nil {
		return domain.ModerationJournalEvent{}, err
	}
	// simplification: append replays the bounded guild journal; add a durable side index if configured caps grow beyond 64 MiB.
	if err := validateModerationIdentity(events, event); err != nil {
		return domain.ModerationJournalEvent{}, err
	}
	event.GuildID, event.Sequence, event.PreviousHash, event.RecordHash = guildID, uint64(len(events)+1), "", ""
	if len(events) > 0 {
		event.PreviousHash = events[len(events)-1].RecordHash
	}
	hash, err := moderationRecordHash(event)
	if err != nil {
		return domain.ModerationJournalEvent{}, err
	}
	event.RecordHash = hash
	encoded, err := json.Marshal(event)
	if err != nil {
		return domain.ModerationJournalEvent{}, err
	}
	if len(encoded) > MaxJournalRecordBytes {
		return domain.ModerationJournalEvent{}, fmt.Errorf("moderation journal record exceeds size limit")
	}
	// #nosec G304 -- path is produced by contained Paths.ModerationJournalPath.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return domain.ModerationJournalEvent{}, fmt.Errorf("open moderation journal: %w", err)
	}
	defer file.Close()
	if err := checkPrivateFile(file); err != nil {
		return domain.ModerationJournalEvent{}, err
	}
	line := append(encoded, '\n')
	written, err := file.Write(line)
	if err != nil || written != len(line) {
		if err == nil {
			err = io.ErrShortWrite
		}
		return domain.ModerationJournalEvent{}, fmt.Errorf("append moderation journal: %w", err)
	}
	if err := file.Sync(); err != nil {
		return domain.ModerationJournalEvent{}, fmt.Errorf("sync moderation journal: %w", err)
	}
	return event, nil
}

func replayModerationJournal(path string, guildID domain.GuildID) ([]domain.ModerationJournalEvent, int64, error) {
	// #nosec G304 -- path is produced by contained Paths.ModerationJournalPath.
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []domain.ModerationJournalEvent{}, -1, nil
	}
	if err != nil {
		return nil, -1, fmt.Errorf("read moderation journal: %w", err)
	}
	if len(data) > MaxJournalBytes {
		return nil, -1, fmt.Errorf("moderation journal exceeds size limit")
	}
	terminated := bytes.HasSuffix(data, []byte{'\n'})
	lines := bytes.Split(data, []byte{'\n'})
	events := make([]domain.ModerationJournalEvent, 0, len(lines))
	previousHash := ""
	for index, line := range lines {
		last := index == len(lines)-1
		if last && !terminated {
			if len(line) > MaxJournalRecordBytes {
				return nil, -1, fmt.Errorf("moderation journal record exceeds size limit")
			}
			if event, decodeErr := decodeModerationRecord(line); decodeErr == nil {
				if err := validateModerationRecord(event, guildID, uint64(len(events)+1), previousHash, index+1); err != nil {
					return nil, -1, err
				}
			}
			return events, int64(len(data) - len(line)), nil
		}
		if len(line) == 0 {
			if last {
				continue
			}
			return nil, -1, fmt.Errorf("moderation journal contains an empty record")
		}
		if len(line) > MaxJournalRecordBytes {
			return nil, -1, fmt.Errorf("moderation journal record exceeds size limit")
		}
		event, err := decodeModerationRecord(line)
		if err != nil {
			return nil, -1, fmt.Errorf("decode moderation record %d: %w", index+1, err)
		}
		if err := validateModerationRecord(event, guildID, uint64(len(events)+1), previousHash, index+1); err != nil {
			return nil, -1, err
		}
		if err := validateModerationIdentity(events, event); err != nil {
			return nil, -1, fmt.Errorf("moderation record %d: %w", index+1, err)
		}
		events, previousHash = append(events, event), event.RecordHash
	}
	return events, -1, nil
}

func decodeModerationRecord(line []byte) (domain.ModerationJournalEvent, error) {
	var event domain.ModerationJournalEvent
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return event, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return event, fmt.Errorf("moderation record contains trailing data")
	}
	return event, nil
}

func validateModerationRecord(event domain.ModerationJournalEvent, guildID domain.GuildID, sequence uint64, previousHash string, record int) error {
	if event.GuildID != guildID {
		return fmt.Errorf("moderation journal guild mismatch at record %d", record)
	}
	if event.Sequence != sequence || event.PreviousHash != previousHash {
		return fmt.Errorf("moderation journal sequence mismatch at record %d", record)
	}
	if err := validateModerationPayload(event, guildID); err != nil {
		return fmt.Errorf("moderation journal payload at record %d: %w", record, err)
	}
	recorded := event.RecordHash
	event.RecordHash = ""
	expected, err := moderationRecordHash(event)
	if err != nil {
		return err
	}
	if recorded == "" || recorded != expected {
		return fmt.Errorf("moderation journal hash mismatch at record %d", record)
	}
	return nil
}

func validateModerationPayload(event domain.ModerationJournalEvent, guildID domain.GuildID) error {
	count := 0
	if event.Case != nil {
		count++
		if event.Case.GuildID != guildID {
			return fmt.Errorf("case guild mismatch")
		}
		if err := event.Case.Validate(); err != nil {
			return err
		}
	}
	if event.Evidence != nil {
		count++
		if event.Evidence.GuildID != guildID {
			return fmt.Errorf("evidence guild mismatch")
		}
		if err := event.Evidence.Validate(); err != nil {
			return err
		}
	}
	if event.Action != nil {
		count++
		if event.Action.GuildID != guildID {
			return fmt.Errorf("action guild mismatch")
		}
		if err := event.Action.Validate(); err != nil {
			return err
		}
	}
	if event.Tombstone != nil {
		count++
		if event.Tombstone.CaseID == "" || event.Tombstone.CorrelationID == "" || event.Tombstone.RetainedAt.IsZero() || event.Tombstone.ActionID != "" && (event.Tombstone.IdempotencyKey == "" || event.Tombstone.IntentHash == "") {
			return fmt.Errorf("invalid retention tombstone")
		}
	}
	if event.OccurredAt.IsZero() || count != 1 {
		return fmt.Errorf("moderation event requires one typed payload")
	}
	expected := map[domain.ModerationJournalEventType]bool{domain.ModerationCaseUpsert: event.Case != nil, domain.ModerationEvidenceAttach: event.Evidence != nil, domain.ModerationActionAppend: event.Action != nil, domain.ModerationRetentionTombstone: event.Tombstone != nil}
	if !expected[event.Type] {
		return fmt.Errorf("moderation event type does not match payload")
	}
	return nil
}

func validateModerationIdentity(events []domain.ModerationJournalEvent, candidate domain.ModerationJournalEvent) error {
	for _, event := range events {
		if candidate.Action != nil && event.Tombstone != nil && candidate.Action.IdempotencyKey == event.Tombstone.IdempotencyKey {
			if actionIntentHash(*candidate.Action) != event.Tombstone.IntentHash {
				return fmt.Errorf("duplicate idempotency key has different action payload")
			}
			return fmt.Errorf("duplicate idempotency key")
		}
		if candidate.Evidence != nil && event.Evidence != nil && candidate.Evidence.ID == event.Evidence.ID {
			if !equalWithoutJournal(candidate.Evidence, event.Evidence) {
				return fmt.Errorf("duplicate evidence ID has different payload")
			}
			return fmt.Errorf("duplicate evidence ID")
		}
		if candidate.Action != nil && event.Action != nil {
			if candidate.Action.ID == event.Action.ID {
				if !equalActionIntent(*candidate.Action, *event.Action) {
					return fmt.Errorf("duplicate action ID has different payload")
				}
				continue
			}
			if candidate.Action.IdempotencyKey == event.Action.IdempotencyKey {
				if !equalActionIntent(*candidate.Action, *event.Action) {
					return fmt.Errorf("duplicate idempotency key has different action payload")
				}
				return fmt.Errorf("duplicate idempotency key")
			}
		}
	}
	return nil
}

func equalWithoutJournal(left, right any) bool {
	l, _ := json.Marshal(left)
	r, _ := json.Marshal(right)
	return bytes.Equal(l, r)
}

func actionIntentHash(action domain.ModerationAction) string {
	action.ID, action.Status, action.Result, action.CompletedAt = "", "", "", nil
	encoded, _ := json.Marshal(action)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func equalActionIntent(left, right domain.ModerationAction) bool {
	left.ID, left.Status, left.Result, left.CompletedAt = "", "", "", nil
	right.ID, right.Status, right.Result, right.CompletedAt = "", "", "", nil
	return equalWithoutJournal(left, right)
}

func moderationRecordHash(event domain.ModerationJournalEvent) (string, error) {
	event.RecordHash = ""
	encoded, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

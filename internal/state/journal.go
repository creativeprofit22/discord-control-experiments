package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const MaxJournalRecordBytes = 64 << 10
const MaxJournalBytes = 64 << 20

type Journal struct {
	mu       sync.Mutex
	path     string
	guildID  domain.GuildID
	planHash domain.PlanHash
	sequence uint64
	lastHash string
}

func OpenJournal(path string, guildID domain.GuildID, planHash domain.PlanHash) (*Journal, []domain.JournalEvent, error) {
	if guildID == "" || planHash == "" {
		return nil, nil, fmt.Errorf("journal guild and plan hash are required")
	}
	events, repairOffset, err := replayJournal(path, guildID, planHash)
	if err != nil {
		return nil, nil, err
	}
	if repairOffset >= 0 {
		if err := repairJournal(path, repairOffset); err != nil {
			return nil, nil, err
		}
	}
	journal := &Journal{path: path, guildID: guildID, planHash: planHash}
	if len(events) > 0 {
		journal.sequence = events[len(events)-1].Sequence
		journal.lastHash = events[len(events)-1].RecordHash
	}
	return journal, events, nil
}

func (j *Journal) Append(event domain.JournalEvent) (domain.JournalEvent, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if event.GuildID != "" && event.GuildID != j.guildID {
		return domain.JournalEvent{}, fmt.Errorf("journal guild mismatch")
	}
	if event.PlanHash != "" && event.PlanHash != j.planHash {
		return domain.JournalEvent{}, fmt.Errorf("journal plan mismatch")
	}
	event.GuildID, event.PlanHash = j.guildID, j.planHash
	event.Sequence, event.PreviousHash, event.RecordHash = j.sequence+1, j.lastHash, ""
	hash, err := journalRecordHash(event)
	if err != nil {
		return domain.JournalEvent{}, err
	}
	event.RecordHash = hash
	encoded, err := json.Marshal(event)
	if err != nil {
		return domain.JournalEvent{}, fmt.Errorf("encode journal event: %w", err)
	}
	if len(encoded) > MaxJournalRecordBytes {
		return domain.JournalEvent{}, fmt.Errorf("journal event exceeds size limit")
	}
	file, err := os.OpenFile(j.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return domain.JournalEvent{}, fmt.Errorf("open journal: %w", err)
	}
	defer file.Close()
	if err := checkPrivateFile(file); err != nil {
		return domain.JournalEvent{}, err
	}
	line := append(encoded, '\n')
	written, err := file.Write(line)
	if err != nil || written != len(line) {
		if err == nil {
			err = fmt.Errorf("short write")
		}
		return domain.JournalEvent{}, fmt.Errorf("append journal: %w", err)
	}
	if err := file.Sync(); err != nil {
		return domain.JournalEvent{}, fmt.Errorf("sync journal: %w", err)
	}
	j.sequence, j.lastHash = event.Sequence, event.RecordHash
	return event, nil
}

func ReplayJournal(path string, guildID domain.GuildID, planHash domain.PlanHash) ([]domain.JournalEvent, error) {
	events, _, err := replayJournal(path, guildID, planHash)
	return events, err
}

func replayJournal(path string, guildID domain.GuildID, planHash domain.PlanHash) ([]domain.JournalEvent, int64, error) {
	// #nosec G304 -- callers obtain path from Paths.JournalPath, which validates components and containment.
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []domain.JournalEvent{}, -1, nil
	}
	if err != nil {
		return nil, -1, fmt.Errorf("read journal: %w", err)
	}
	if len(data) > MaxJournalBytes {
		return nil, -1, fmt.Errorf("journal exceeds size limit")
	}
	terminated := bytes.HasSuffix(data, []byte{'\n'})
	lines := bytes.Split(data, []byte{'\n'})
	events := make([]domain.JournalEvent, 0, len(lines))
	previousHash := ""
	for index, line := range lines {
		last := index == len(lines)-1
		if last && !terminated {
			if len(line) > MaxJournalRecordBytes {
				return nil, -1, fmt.Errorf("journal record exceeds size limit")
			}
			event, decodeErr := decodeJournalRecord(line)
			if decodeErr == nil {
				if _, err := validateJournalEvent(event, guildID, planHash, uint64(len(events)+1), previousHash, index+1); err != nil {
					return nil, -1, err
				}
			}
			return events, int64(len(data) - len(line)), nil
		}
		if len(line) == 0 {
			if last {
				continue
			}
			return nil, -1, fmt.Errorf("journal contains an empty record")
		}
		if len(line) > MaxJournalRecordBytes {
			return nil, -1, fmt.Errorf("journal record exceeds size limit")
		}
		event, err := decodeJournalRecord(line)
		if err != nil {
			return nil, -1, fmt.Errorf("decode journal record %d: %w", index+1, err)
		}
		event, err = validateJournalEvent(event, guildID, planHash, uint64(len(events)+1), previousHash, index+1)
		if err != nil {
			return nil, -1, err
		}
		events = append(events, event)
		previousHash = event.RecordHash
	}
	return events, -1, nil
}

func decodeJournalRecord(line []byte) (domain.JournalEvent, error) {
	var event domain.JournalEvent
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return domain.JournalEvent{}, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return domain.JournalEvent{}, fmt.Errorf("journal record contains multiple JSON values")
		}
		return domain.JournalEvent{}, fmt.Errorf("journal record contains trailing data: %w", err)
	}
	return event, nil
}

func validateJournalEvent(event domain.JournalEvent, guildID domain.GuildID, planHash domain.PlanHash, sequence uint64, previousHash string, record int) (domain.JournalEvent, error) {
	if event.GuildID != guildID || event.PlanHash != planHash {
		return domain.JournalEvent{}, fmt.Errorf("journal binding mismatch at record %d", record)
	}
	if event.Sequence != sequence || event.PreviousHash != previousHash {
		return domain.JournalEvent{}, fmt.Errorf("journal sequence mismatch at record %d", record)
	}
	recordedHash := event.RecordHash
	event.RecordHash = ""
	expectedHash, err := journalRecordHash(event)
	if err != nil {
		return domain.JournalEvent{}, err
	}
	event.RecordHash = recordedHash
	if recordedHash == "" || recordedHash != expectedHash {
		return domain.JournalEvent{}, fmt.Errorf("journal hash mismatch at record %d", record)
	}
	return event, nil
}

func repairJournal(path string, offset int64) error {
	// #nosec G304 -- callers obtain path from Paths.JournalPath, which validates components and containment.
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open journal for crash-tail repair: %w", err)
	}
	defer file.Close()
	if err := checkPrivateFile(file); err != nil {
		return err
	}
	if err := file.Truncate(offset); err != nil {
		return fmt.Errorf("truncate journal crash tail: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync repaired journal: %w", err)
	}
	return nil
}

func journalRecordHash(event domain.JournalEvent) (string, error) {
	event.RecordHash = ""
	encoded, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
func checkPrivateFile(file *os.File) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("state file permissions are too broad")
	}
	return nil
}

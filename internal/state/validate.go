package state

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func ValidateStateTree(root string) error {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("state root is not a regular directory")
	}
	rootHandle, err := os.OpenRoot(absolute)
	if err != nil {
		return err
	}
	defer rootHandle.Close()
	count := 0
	return filepath.WalkDir(absolute, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("state tree contains a symbolic link")
		}
		if entry.IsDir() {
			return nil
		}
		count++
		if count > MaxBackupEntries {
			return errors.New("state tree contains too many files")
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > MaxBackupFileBytes {
			return errors.New("state tree contains an unsupported file")
		}
		relative, _ := filepath.Rel(absolute, filename)
		if strings.HasSuffix(relative, ".json") {
			return validateStateJSON(rootHandle, filepath.ToSlash(relative))
		}
		if strings.HasSuffix(relative, ".jsonl") {
			return validateStateJournal(rootHandle, relative, filename)
		}
		if strings.HasSuffix(relative, ".lock") {
			return nil
		}
		return fmt.Errorf("unsupported state file %q", filepath.ToSlash(relative))
	})
}

func validateStateJSON(root *os.Root, relative string) error {
	data, err := root.ReadFile(relative)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var value any
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("malformed state JSON: %w", err)
	}
	if err := decoder.Decode(&value); !errors.Is(err, io.EOF) {
		return errors.New("state JSON contains trailing or multiple values")
	}
	return nil
}

func validateStateJournal(root *os.Root, relative, filename string) error {
	first, err := firstJournalRecord(root, filepath.ToSlash(relative))
	if err != nil {
		return err
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	base := filepath.Base(filename)
	if len(parts) == 3 && parts[0] == "guilds" {
		guild := domain.GuildID(parts[1])
		switch {
		case base == "moderation.jsonl":
			var event domain.ModerationJournalEvent
			if json.Unmarshal(first, &event) != nil {
				return errors.New("malformed moderation journal")
			}
			_, _, err = replayModerationJournal(filename, guild)
			return err
		case strings.HasPrefix(base, "lockdown-journal-"):
			var event domain.LockdownJournalEvent
			if json.Unmarshal(first, &event) != nil {
				return errors.New("malformed lockdown journal")
			}
			_, _, err = replayLockdownJournal(filename, guild, event.PlanHash)
			return err
		case strings.HasPrefix(base, "journal-"):
			var event domain.JournalEvent
			if json.Unmarshal(first, &event) != nil {
				return errors.New("malformed reconciliation journal")
			}
			_, _, err = replayJournal(filename, guild, event.PlanHash)
			return err
		}
	}
	if len(parts) == 2 && parts[0] == "automation" && strings.HasPrefix(base, "journal-") {
		var event domain.AutomationJournalEvent
		if json.Unmarshal(first, &event) != nil {
			return errors.New("malformed automation journal")
		}
		_, _, err = replayAutomationJournal(filename, event.RunID, event.WorkflowHash)
		return err
	}
	return fmt.Errorf("unsupported state journal %q", filepath.ToSlash(relative))
}

func firstJournalRecord(root *os.Root, relative string) ([]byte, error) {
	file, err := root.Open(relative)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), MaxJournalRecordBytes+1)
	if !scanner.Scan() || len(scanner.Bytes()) == 0 {
		return nil, errors.New("state journal is empty or malformed")
	}
	return bytes.Clone(scanner.Bytes()), scanner.Err()
}

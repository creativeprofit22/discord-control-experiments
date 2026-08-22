package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const maxPlanBytes = 8 << 20

type Store struct {
	paths    Paths
	bindings *BindingStore
}

func NewStore(paths Paths) *Store { return &Store{paths: paths, bindings: NewBindingStore(paths)} }
func (s *Store) LoadBindings(guildID domain.GuildID) ([]domain.ResourceBinding, error) {
	return s.bindings.Load(guildID)
}
func (s *Store) SaveBindings(guildID domain.GuildID, bindings []domain.ResourceBinding) error {
	return s.bindings.Save(guildID, bindings)
}
func (s *Store) AcquireGuildLock(guildID domain.GuildID) (io.Closer, error) {
	return AcquireGuildLock(s.paths, guildID)
}
func (s *Store) OpenJournal(guildID domain.GuildID, runID domain.RunID, planHash domain.PlanHash) (application.ReconciliationJournal, []domain.JournalEvent, error) {
	path, err := s.paths.JournalPath(guildID, runID)
	if err != nil {
		return nil, nil, err
	}
	return OpenJournal(path, guildID, planHash)
}
func (s *Store) planPath(guildID domain.GuildID, runID domain.RunID) (string, error) {
	if !safeComponent(string(runID)) {
		return "", fmt.Errorf("invalid run ID")
	}
	directory, err := s.paths.GuildDirectory(guildID)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "plan-"+string(runID)+".json"), nil
}
func (s *Store) SavePlan(guildID domain.GuildID, runID domain.RunID, plan domain.Plan) error {
	path, err := s.planPath(guildID, runID)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("encode plan: %w", err)
	}
	if len(encoded) > maxPlanBytes {
		return fmt.Errorf("plan exceeds size limit")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".plan-*.tmp")
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
	if runtime.GOOS == "windows" {
		_ = os.Remove(path)
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
func (s *Store) LoadPlan(guildID domain.GuildID, runID domain.RunID) (domain.Plan, error) {
	path, err := s.planPath(guildID, runID)
	if err != nil {
		return domain.Plan{}, err
	}
	// #nosec G304 -- planPath validates guild/run components and contains the path beneath the configured state root.
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.Plan{}, fmt.Errorf("read saved plan: %w", err)
	}
	if len(data) > maxPlanBytes {
		return domain.Plan{}, fmt.Errorf("saved plan exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var plan domain.Plan
	if err := decoder.Decode(&plan); err != nil {
		return domain.Plan{}, fmt.Errorf("decode saved plan: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return domain.Plan{}, fmt.Errorf("saved plan contains trailing data")
	}
	return plan, nil
}

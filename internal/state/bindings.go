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
	"sort"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const bindingsSchemaV1 = "discord-control/bindings-v1"
const maxBindingsBytes = 4 << 20

type bindingDocument struct {
	SchemaVersion string                   `json:"schema_version"`
	GuildID       domain.GuildID           `json:"guild_id"`
	Bindings      []domain.ResourceBinding `json:"bindings"`
}

type BindingStore struct{ paths Paths }

func NewBindingStore(paths Paths) *BindingStore { return &BindingStore{paths: paths} }

func (s *BindingStore) Load(guildID domain.GuildID) ([]domain.ResourceBinding, error) {
	path, err := s.paths.BindingsPath(guildID)
	if err != nil {
		return nil, err
	}
	// #nosec G304 -- BindingsPath validates the numeric guild ID and contains the path beneath the configured state root.
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []domain.ResourceBinding{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read bindings: %w", err)
	}
	if len(data) > maxBindingsBytes {
		return nil, fmt.Errorf("bindings exceed size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document bindingDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode bindings: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("bindings contain trailing data")
	}
	if document.SchemaVersion != bindingsSchemaV1 || document.GuildID != guildID {
		return nil, fmt.Errorf("bindings guild or schema mismatch")
	}
	if err := validateBindings(document.Bindings); err != nil {
		return nil, err
	}
	return document.Bindings, nil
}

func (s *BindingStore) Save(guildID domain.GuildID, bindings []domain.ResourceBinding) error {
	if err := validateBindings(bindings); err != nil {
		return err
	}
	path, err := s.paths.BindingsPath(guildID)
	if err != nil {
		return err
	}
	if info, statErr := os.Lstat(path); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("bindings path is a symbolic link")
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	ordered := append([]domain.ResourceBinding(nil), bindings...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Ref.Kind != ordered[j].Ref.Kind {
			return ordered[i].Ref.Kind < ordered[j].Ref.Kind
		}
		return ordered[i].Ref.LogicalID < ordered[j].Ref.LogicalID
	})
	encoded, err := json.Marshal(bindingDocument{SchemaVersion: bindingsSchemaV1, GuildID: guildID, Bindings: ordered})
	if err != nil {
		return fmt.Errorf("encode bindings: %w", err)
	}
	if len(encoded) > maxBindingsBytes {
		return fmt.Errorf("bindings exceed size limit")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".bindings-*.tmp")
	if err != nil {
		return fmt.Errorf("create bindings temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		return errors.Join(fmt.Errorf("set bindings permissions: %w", err), temporary.Close())
	}
	if _, err := temporary.Write(encoded); err != nil {
		return errors.Join(fmt.Errorf("write bindings: %w", err), temporary.Close())
	}
	if err := temporary.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync bindings: %w", err), temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close bindings: %w", err)
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(path)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace bindings: %w", err)
	}
	return syncDirectory(filepath.Dir(path))
}

func validateBindings(bindings []domain.ResourceBinding) error {
	refs := map[domain.StableResourceRef]bool{}
	ids := map[domain.Snowflake]bool{}
	for _, binding := range bindings {
		if binding.Ref.Kind == "" || !safeComponent(binding.Ref.LogicalID) || !decimalID(string(binding.RemoteID)) || binding.Fingerprint == "" {
			return fmt.Errorf("invalid ownership binding")
		}
		if refs[binding.Ref] || ids[binding.RemoteID] {
			return fmt.Errorf("duplicate ownership binding")
		}
		refs[binding.Ref], ids[binding.RemoteID] = true, true
	}
	return nil
}
func syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	// #nosec G304 -- path is the directory of a state file already contained by Paths.
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

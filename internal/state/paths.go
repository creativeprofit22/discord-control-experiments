package state

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type Paths struct{ root string }

func ResolvePaths(configDirectory, configuredStateDirectory string) (Paths, error) {
	if configDirectory == "" {
		return Paths{}, fmt.Errorf("configuration directory is required")
	}
	base, err := filepath.Abs(configDirectory)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve configuration directory: %w", err)
	}
	root := configuredStateDirectory
	if root == "" {
		root = "state"
	}
	if !filepath.IsAbs(root) {
		root = filepath.Join(base, root)
	}
	root, err = filepath.Abs(filepath.Clean(root))
	if err != nil {
		return Paths{}, fmt.Errorf("resolve state directory: %w", err)
	}
	if !contained(base, root) {
		return Paths{}, fmt.Errorf("state directory must remain beneath configuration directory")
	}
	if err := rejectSymlinkPath(base, root); err != nil {
		return Paths{}, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Paths{}, fmt.Errorf("create state directory: %w", err)
	}
	if err := checkPrivateDirectory(root); err != nil {
		return Paths{}, err
	}
	return Paths{root: root}, nil
}

func (p Paths) Root() string { return p.root }
func (p Paths) GuildDirectory(guildID domain.GuildID) (string, error) {
	if !decimalID(string(guildID)) {
		return "", fmt.Errorf("invalid guild ID")
	}
	path := filepath.Join(p.root, "guilds", string(guildID))
	if !contained(p.root, path) {
		return "", fmt.Errorf("guild path escaped state directory")
	}
	if err := rejectSymlinkPath(p.root, path); err != nil {
		return "", err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("create guild state directory: %w", err)
	}
	return path, checkPrivateDirectory(path)
}
func (p Paths) JournalPath(guildID domain.GuildID, runID domain.RunID) (string, error) {
	if !safeComponent(string(runID)) {
		return "", fmt.Errorf("invalid run ID")
	}
	directory, err := p.GuildDirectory(guildID)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "journal-"+string(runID)+".jsonl"), nil
}
func (p Paths) ModerationJournalPath(guildID domain.GuildID) (string, error) {
	directory, err := p.GuildDirectory(guildID)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "moderation.jsonl"), nil
}
func (p Paths) LockdownPlanPath(guildID domain.GuildID, hash domain.LockdownPlanHash) (string, error) {
	if !safeComponent(string(hash)) {
		return "", fmt.Errorf("invalid lockdown plan hash")
	}
	directory, err := p.GuildDirectory(guildID)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "lockdown-plan-"+string(hash)+".json"), nil
}
func (p Paths) LockdownJournalPath(guildID domain.GuildID, hash domain.LockdownPlanHash) (string, error) {
	if !safeComponent(string(hash)) {
		return "", fmt.Errorf("invalid lockdown plan hash")
	}
	directory, err := p.GuildDirectory(guildID)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "lockdown-journal-"+string(hash)+".jsonl"), nil
}
func (p Paths) BindingsPath(guildID domain.GuildID) (string, error) {
	directory, err := p.GuildDirectory(guildID)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "bindings.json"), nil
}
func (p Paths) LockPath(guildID domain.GuildID) (string, error) {
	directory, err := p.GuildDirectory(guildID)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "apply.lock"), nil
}

func (p Paths) AutomationDirectory() (string, error) {
	path := filepath.Join(p.root, "automation")
	if !contained(p.root, path) {
		return "", fmt.Errorf("automation path escaped state directory")
	}
	if err := rejectSymlinkPath(p.root, path); err != nil {
		return "", err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("create automation state directory: %w", err)
	}
	return path, checkPrivateDirectory(path)
}

func (p Paths) AutomationPath(kind, id, extension string) (string, error) {
	if !safeComponent(kind) || !safeComponent(id) || (extension != ".json" && extension != ".jsonl") {
		return "", fmt.Errorf("invalid automation path component")
	}
	directory, err := p.AutomationDirectory()
	if err != nil {
		return "", err
	}
	path := filepath.Join(directory, kind+"-"+id+extension)
	if !contained(directory, path) {
		return "", fmt.Errorf("automation path escaped state directory")
	}
	return path, nil
}

func (p Paths) AutomationLockPath() (string, error) {
	directory, err := p.AutomationDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "transitions.lock"), nil
}

func contained(base, target string) bool {
	relative, err := filepath.Rel(base, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
func decimalID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value[0] != '0'
}
func safeComponent(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
func rejectSymlinkPath(base, target string) error {
	current := base
	relative, err := filepath.Rel(base, target)
	if err != nil {
		return err
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return fmt.Errorf("inspect state path: %w", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("state path contains a symbolic link")
		}
	}
	return nil
}
func checkPrivateDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("state directory permissions are too broad")
	}
	return nil
}

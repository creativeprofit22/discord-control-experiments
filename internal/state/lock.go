package state

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type GuildLock struct {
	mu     sync.Mutex
	path   string
	file   *os.File
	closed bool
}

func AcquireGuildLock(paths Paths, guildID domain.GuildID) (*GuildLock, error) {
	path, err := paths.LockPath(guildID)
	if err != nil {
		return nil, err
	}
	// #nosec G304 -- LockPath validates the numeric guild ID and contains the path beneath the configured state root.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if os.IsExist(err) {
		return nil, fmt.Errorf("an apply is already active for guild %s", guildID)
	}
	if err != nil {
		return nil, fmt.Errorf("acquire guild lock: %w", err)
	}
	if _, err := file.WriteString(strconv.Itoa(os.Getpid()) + "\n"); err != nil {
		return nil, errors.Join(fmt.Errorf("write guild lock: %w", err), cleanupGuildLock(file, path))
	}
	if err := file.Sync(); err != nil {
		return nil, errors.Join(fmt.Errorf("sync guild lock: %w", err), cleanupGuildLock(file, path))
	}
	return &GuildLock{path: path, file: file}, nil
}

func cleanupGuildLock(file *os.File, path string) error {
	closeErr := file.Close()
	removeErr := os.Remove(path)
	if os.IsNotExist(removeErr) {
		removeErr = nil
	}
	return errors.Join(closeErr, removeErr)
}

func (lock *GuildLock) Close() error {
	lock.mu.Lock()
	defer lock.mu.Unlock()
	if lock.closed {
		return nil
	}
	lock.closed = true
	closeErr := lock.file.Close()
	removeErr := os.Remove(lock.path)
	if closeErr != nil {
		return closeErr
	}
	if removeErr != nil && !os.IsNotExist(removeErr) {
		return removeErr
	}
	return nil
}

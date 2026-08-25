//go:build windows

package state

import (
	"context"
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func acquireFileLock(ctx context.Context, root, path string) (*os.File, error) {
	file, err := openRootFile(root, path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	overlapped := new(windows.Overlapped)
	for {
		err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			_ = file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func releaseFileLock(file *os.File) error {
	overlapped := new(windows.Overlapped)
	return errors.Join(windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped), file.Close())
}

//go:build !windows

package state

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func acquireFileLock(ctx context.Context, root, path string) (*os.File, error) {
	file, err := openRootFile(root, path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
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
	return errors.Join(unix.Flock(int(file.Fd()), unix.LOCK_UN), file.Close())
}

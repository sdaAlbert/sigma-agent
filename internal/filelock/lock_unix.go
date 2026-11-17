//go:build !windows

package filelock

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func lockFile(file *os.File, wait bool) (func() error, error) {
	mode := unix.LOCK_EX
	if !wait {
		mode |= unix.LOCK_NB
	}
	if err := unix.Flock(int(file.Fd()), mode); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("acquire file lock: %w", err)
	}
	return func() error { return unix.Flock(int(file.Fd()), unix.LOCK_UN) }, nil
}

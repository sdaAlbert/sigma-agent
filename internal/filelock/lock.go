package filelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var ErrLocked = errors.New("lock is already held")

// Lock is an operating-system-backed exclusive file lock. The lock is released
// automatically when the process exits or the file handle closes.
type Lock struct {
	file   *os.File
	unlock func() error
	once   sync.Once
	err    error
}

func Acquire(path string, wait bool) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	unlock, err := lockFile(file, wait)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &Lock{file: file, unlock: unlock}, nil
}

func (l *Lock) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		if l.unlock != nil {
			l.err = l.unlock()
		}
		if err := l.file.Close(); l.err == nil {
			l.err = err
		}
	})
	return l.err
}

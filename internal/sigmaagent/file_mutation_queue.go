package sigmaagent

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type queuedPathLock struct {
	token chan struct{}
	refs  int
}

// fileMutationQueue permits independent files to be edited concurrently while
// serializing every mutation to the same workspace path.
type fileMutationQueue struct {
	mu    sync.Mutex
	locks map[string]*queuedPathLock
}

func newFileMutationQueue() *fileMutationQueue {
	return &fileMutationQueue{locks: make(map[string]*queuedPathLock)}
}

func (q *fileMutationQueue) acquire(ctx context.Context, path string) (func(), error) {
	key := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}

	q.mu.Lock()
	lock := q.locks[key]
	if lock == nil {
		lock = &queuedPathLock{token: make(chan struct{}, 1)}
		q.locks[key] = lock
	}
	lock.refs++
	q.mu.Unlock()

	select {
	case lock.token <- struct{}{}:
		return func() {
			<-lock.token
			q.releaseReference(key, lock)
		}, nil
	case <-ctx.Done():
		q.releaseReference(key, lock)
		return nil, ctx.Err()
	}
}

func (q *fileMutationQueue) releaseReference(key string, lock *queuedPathLock) {
	q.mu.Lock()
	defer q.mu.Unlock()
	lock.refs--
	if lock.refs == 0 {
		delete(q.locks, key)
	}
}

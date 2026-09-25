package cache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gofrs/flock"

	"github.com/git-bug/git-bug/repository"
)

// writeLockFile is the file the write lock is taken on, inside local storage.
//
// It replaces the process-lifetime "lock" file upstream took at cache open,
// which made concurrency a matter of who got there first: an open termui held
// the store for as long as it was open, and every other command failed. Here
// readers take nothing at all, and a writer holds this lock only across one
// read-modify-commit (d35de2e).
const writeLockFile = "write.lock"

const (
	writeLockTimeout = 5 * time.Second
	writeLockRetry   = 20 * time.Millisecond
)

// inProcessWriteLock stands in for the file lock when local storage has no real
// path behind it — the in-memory repositories used in tests. Such a store
// cannot be shared with another process, so a process-wide mutex is not a
// weaker guarantee there, it is the same one.
var inProcessWriteLock sync.Mutex

// lockWrite takes the store's write lock and returns the function releasing it.
//
// The lock is advisory and only binds processes that go through this package,
// which every writer does: the CLI, the TUI, the web UI and the bridge all sit
// on the cache. Writing refs/work-issues/* from outside git-work — a stray
// `git update-ref`, say — is outside what this can protect.
func lockWrite(repo repository.RepoStorage) (func(), error) {
	root := repo.LocalStorage().Root()

	fi, err := os.Stat(root)
	if err != nil || !fi.IsDir() {
		inProcessWriteLock.Lock()
		return inProcessWriteLock.Unlock, nil
	}

	path := filepath.Join(root, writeLockFile)
	lock := flock.New(path)

	ctx, cancel := context.WithTimeout(context.Background(), writeLockTimeout)
	defer cancel()

	locked, err := lock.TryLockContext(ctx, writeLockRetry)
	if err != nil && ctx.Err() == nil {
		return nil, err
	}
	if !locked {
		return nil, fmt.Errorf("timed out after %s waiting for the write lock%s; "+
			"another git-work process is writing to this repository",
			writeLockTimeout, describeLockHolder(path))
	}

	// Record the holder for the message above. The lock lives in the kernel,
	// not in this content, so a stale pid here is only ever a stale diagnostic:
	// there is no stale-lock cleanup to get wrong, because the kernel releases
	// the lock when the process dies, crash included.
	_ = os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644)

	return func() { _ = lock.Unlock() }, nil
}

// describeLockHolder reads the pid the holder left behind, for the error
// message only. Any failure means we simply do not name it.
func describeLockHolder(path string) string {
	buf, err := os.ReadFile(path)
	if err != nil || len(buf) == 0 {
		return ""
	}
	pid, err := strconv.Atoi(string(buf))
	if err != nil {
		return ""
	}
	return fmt.Sprintf(" (held by pid %d)", pid)
}

// LockWrite is lockWrite for the one writer outside this package,
// the store migration (bf6f392),
// which rewrites whole entities under the namespaces the cache owns
// and so holds the same lock a cache write does.
func LockWrite(repo repository.RepoStorage) (func(), error) {
	return lockWrite(repo)
}

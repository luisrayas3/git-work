package cache

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	// watchDebounce collapses the burst of filesystem events a single write
	// produces — git writes a ref by creating a temporary file and renaming it
	// — into one reconciliation.
	watchDebounce = 100 * time.Millisecond

	// watchPollInterval is the backstop. fsnotify can drop events under load,
	// and does not work at all on some filesystems; without a poll, a missed
	// event means a view that never updates again. With one, the worst case is
	// this much latency.
	watchPollInterval = 10 * time.Second
)

// Watch reports changes made by other processes to the registered observers,
// until the returned function is called.
//
// Entity refs are watched directly. The reconciliation itself is the same
// ref->hash diff the cache does when it loads (d591cb3), so the event only
// decides *when* to look, never *what* changed — which is what makes a missed
// event survivable and a spurious one harmless.
func (c *RepoCache) Watch() (func(), error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// LocalStorage is rooted at <repo>/.git/<namespace>, so its parent is the
	// git directory. The pristine repository interface exposes no path of its
	// own, and adding one is not ours to do.
	gitDir := filepath.Dir(c.repo.LocalStorage().Root())

	// The git directory itself covers packed-refs, which is where `git gc` and
	// `git pack-refs` move loose refs — a mass "deletion" as far as the ref
	// directories are concerned.
	paths := []string{gitDir, filepath.Join(gitDir, "refs")}
	for _, subcache := range c.subcaches {
		paths = append(paths, filepath.Join(gitDir, "refs", subcache.GetNamespace()))
	}

	for _, path := range paths {
		// a namespace with no entities yet has no directory; the watch on
		// refs/ catches its creation
		if _, err := os.Stat(path); err != nil {
			continue
		}
		// a path we cannot watch is covered by the poll below
		_ = watcher.Add(path)
	}

	done := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() {
			close(done)
			_ = watcher.Close()
		})
	}

	go c.watchLoop(watcher, done)

	return stop, nil
}

func (c *RepoCache) watchLoop(watcher *fsnotify.Watcher, done <-chan struct{}) {
	// A nil channel blocks forever, which is what we want from the debounce
	// timer while nothing is pending.
	var pending <-chan time.Time

	poll := time.NewTicker(watchPollInterval)
	defer poll.Stop()

	for {
		select {
		case <-done:
			return

		case _, ok := <-watcher.Events:
			if !ok {
				return
			}
			pending = time.After(watchDebounce)

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			// The poll still covers us, so a watcher that fails is a
			// degradation in latency rather than in correctness.
			_ = err

		case <-pending:
			pending = nil
			c.refreshSubcaches()

		case <-poll.C:
			if pending == nil {
				c.refreshSubcaches()
			}
		}
	}
}

func (c *RepoCache) refreshSubcaches() {
	for _, subcache := range c.subcaches {
		// A refresh failing here is not the caller's to handle — there is no
		// caller. The next tick tries again against the same refs.
		_ = subcache.Refresh()
	}
}

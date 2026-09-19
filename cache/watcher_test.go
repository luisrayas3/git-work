package cache

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/repository"
)

// syncObserver is the test observer from repo_cache_test.go, made safe to read
// from the test while the watcher's goroutine writes to it.
type syncObserver struct {
	mu      sync.Mutex
	updated []entity.Id
}

func (o *syncObserver) EntityEvent(event EntityEventType, _ string, _ string, id entity.Id) {
	if event != EntityEventUpdated {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.updated = append(o.updated, id)
}

func (o *syncObserver) sawUpdate(id entity.Id) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, seen := range o.updated {
		if seen == id {
			return true
		}
	}
	return false
}

// TestWatchSeesAnotherProcessWrite covers 63c68d1: an open view learns about a
// write it did not make, without a daemon and without being restarted.
func TestWatchSeesAnotherProcessWrite(t *testing.T) {
	repo := repository.CreateGoGitTestRepo(t, false)

	writer, err := NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	defer writer.Close()

	author, err := writer.Identities().New("René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	require.NoError(t, writer.SetUserIdentity(author))

	b, _, err := writer.Bugs().New("before", "message")
	require.NoError(t, err)

	// a second process, holding the store open — a TUI, say
	viewer, err := NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	defer viewer.Close()

	obs := &syncObserver{}
	viewer.registerAllObservers(defaultRepoName, obs)

	stop, err := viewer.Watch()
	require.NoError(t, err)
	defer stop()

	excerpt, err := viewer.Bugs().ResolveExcerpt(b.Id())
	require.NoError(t, err)
	require.Equal(t, "before", excerpt.Title)

	// the write the viewer knows nothing about
	toEdit, err := writer.Bugs().Resolve(b.Id())
	require.NoError(t, err)
	_, err = toEdit.SetTitle("after")
	require.NoError(t, err)
	require.NoError(t, toEdit.Commit())

	require.Eventually(t, func() bool {
		excerpt, err := viewer.Bugs().ResolveExcerpt(b.Id())
		return err == nil && excerpt.Title == "after"
	}, 5*time.Second, 20*time.Millisecond, "the watching cache never saw the new title")

	require.True(t, obs.sawUpdate(b.Id()), "observers were not told about the change")
}

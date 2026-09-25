package migrate

import (
	"fmt"

	"github.com/git-bug/git-bug/repository"
	"github.com/git-bug/git-bug/util/lamport"
)

// scheduledClocks is a repository whose clocks hand out the times they are told to.
//
// dag.Entity.Commit takes both lamport times from the repository,
// one Increment per pack for the edit clock
// and one more for the create clock on the first pack,
// and nothing else in the pristine packages lets a caller choose them.
// Witnessing the real clock to one below the wanted time
// only works while the wanted times never repeat,
// and two clones can legally produce the same time;
// handing the time out directly reproduces the original packs exactly.
// The real clock is witnessed with every time handed out,
// so it ends where a replay of the same history would leave it.
type scheduledClocks struct {
	repository.ClockedRepo
	next map[string]lamport.Time
}

// schedule sets what the next Increment of one clock returns.
func (s *scheduledClocks) schedule(name string, t lamport.Time) {
	if s.next == nil {
		s.next = make(map[string]lamport.Time)
	}
	s.next[name] = t
}

func (s *scheduledClocks) Increment(name string) (lamport.Time, error) {
	t, ok := s.next[name]
	if !ok {
		return 0, fmt.Errorf("migrate: no time scheduled for clock %s", name)
	}
	delete(s.next, name)
	return t, s.ClockedRepo.Witness(name, t)
}

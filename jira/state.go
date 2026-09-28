package jira

import (
	"encoding/json"
	"errors"
	"os"
	"path"
	"time"

	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/repository"
	"github.com/git-bug/git-bug/util/lamport"
)

// statePath is the run state, local and disposable (JS8): losing it costs a
// slower run, never a wrong one.
const statePath = "jira/state.json"

// State is what one clone remembers between runs.
type State struct {
	Site    string                     `json:"site"`
	Project string                     `json:"project"`
	Cursor  time.Time                  `json:"cursor"`            // Jira's updated, UTC (JS20)
	Seen    map[entity.Id]lamport.Time `json:"seen,omitempty"`    // edit lamport after the last sync (JS20)
	Failed  map[string]time.Time       `json:"failed,omitempty"`  // Jira id -> updated of a hit that failed (JS20)
	Refused map[entity.Id]Refusal      `json:"refused,omitempty"` // creates Jira answered and did not make (JS15)
}

// Refusal is a create attempt Jira refused: not in doubt, and not tried
// again while the issue is unchanged. Losing it costs a Settle's wait.
type Refusal struct {
	At      time.Time    `json:"at"` // the attempt's jira-create
	Lamport lamport.Time `json:"lamport"`
	Reason  string       `json:"reason"`
}

// LoadState reads the state file; a missing one is an empty state.
func LoadState(fs repository.LocalStorage) (*State, error) {
	st := &State{}
	f, err := fs.Open(statePath)
	if errors.Is(err, os.ErrNotExist) {
		return st.init(), nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(st); err != nil {
		// disposable: a damaged file restarts from the markers
		return (&State{}).init(), nil
	}
	return st.init(), nil
}

func (s *State) init() *State {
	if s.Refused == nil {
		s.Refused = map[entity.Id]Refusal{}
	}
	if s.Seen == nil {
		s.Seen = map[entity.Id]lamport.Time{}
	}
	if s.Failed == nil {
		s.Failed = map[string]time.Time{}
	}
	return s
}

// Bind resets the state when it belongs to another site or project (JS20).
func (s *State) Bind(site, project string) {
	if s.Site != site || s.Project != project {
		*s = State{Site: site, Project: project}
		s.init()
	}
}

// Save writes the state file through a temporary file and a rename.
func (s *State) Save(fs repository.LocalStorage) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := fs.MkdirAll(path.Dir(statePath), 0o755); err != nil {
		return err
	}
	tmp := statePath + ".tmp"
	f, err := fs.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return fs.Rename(tmp, statePath)
}

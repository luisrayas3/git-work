package migrate

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/repository"
	"github.com/git-bug/git-bug/util/lamport"
)

// pack is one commit of the old store: the operations it carries
// and the lamport times it was written with.
//
// entities/bug reads an entity as one flat list of operations,
// and the commit boundaries are what the replay has to reproduce,
// one target commit per original,
// so the packs are read again here from the git objects,
// the way entity/dag reads them,
// and matched to the typed operations by id:
// an operation's id is derived from its bytes in the pack blob,
// so hashing the raw element gives the id entities/bug computed.
type pack struct {
	commit     repository.Hash
	createTime lamport.Time
	editTime   lamport.Time
	opIds      []entity.Id
}

// readPacks reads the commits of one old entity, oldest first.
func readPacks(repo repository.RepoData, ref string) ([]pack, error) {
	commits, err := repo.ListCommits(ref)
	if err != nil {
		return nil, err
	}

	packs := make([]pack, 0, len(commits))
	for _, hash := range commits {
		commit, err := repo.ReadCommit(hash)
		if err != nil {
			return nil, err
		}
		if len(commit.Parents) > 1 {
			// A merge commit is what the dag writes when two clones diverged;
			// this store has none, and replaying one would need the dag's own
			// topological order to be meaningful. Refuse rather than guess.
			return nil, fmt.Errorf("%s: commit %s is a merge; this migration replays linear histories only", ref, hash)
		}
		p, err := readPack(repo, commit)
		if err != nil {
			return nil, fmt.Errorf("%s: commit %s: %w", ref, hash, err)
		}
		packs = append(packs, p)
	}
	return packs, nil
}

func readPack(repo repository.RepoData, commit repository.Commit) (pack, error) {
	p := pack{commit: commit.Hash}

	entries, err := repo.ReadTree(commit.TreeHash)
	if err != nil {
		return p, err
	}

	for _, entry := range entries {
		switch {
		case entry.Name == "ops":
			r, err := repo.ReadData(entry.Hash)
			if err != nil {
				return p, err
			}
			data, err := io.ReadAll(r)
			_ = r.Close()
			if err != nil {
				return p, err
			}
			var aux struct {
				Operations []json.RawMessage `json:"ops"`
			}
			if err := json.Unmarshal(data, &aux); err != nil {
				return p, err
			}
			for _, raw := range aux.Operations {
				p.opIds = append(p.opIds, entity.DeriveId(raw))
			}

		case strings.HasPrefix(entry.Name, "create-clock-"):
			v, err := strconv.ParseUint(strings.TrimPrefix(entry.Name, "create-clock-"), 10, 64)
			if err != nil {
				return p, err
			}
			p.createTime = lamport.Time(v)

		case strings.HasPrefix(entry.Name, "edit-clock-"):
			v, err := strconv.ParseUint(strings.TrimPrefix(entry.Name, "edit-clock-"), 10, 64)
			if err != nil {
				return p, err
			}
			p.editTime = lamport.Time(v)
		}
	}

	if len(p.opIds) == 0 {
		return p, fmt.Errorf("no operations in the pack")
	}
	if p.editTime == 0 {
		return p, fmt.Errorf("no edit time on the pack")
	}
	return p, nil
}

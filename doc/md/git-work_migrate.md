## git-work migrate

Migrate the store once from git-bug's format to the owned model

### Synopsis

Migrate the store once from git-bug's format to the owned model (bf6f392).

Every issue under refs/issues/* is replayed under refs/work-issues/*,
one commit per original commit, with its id, its comment ids and its
lamport times unchanged, and the label taxonomy becomes fields;
the identities are copied to refs/work-users/*. The old refs are kept.

The schema has to be in place first: git work schema import schema.yaml.
The command refuses when refs/work-issues/* holds anything.

git work pull does not carry git-bug's store (6274f33).
With --from REMOTE, the command first fetches refs/issues/* and
refs/identities/* from that remote and merges them into the local refs,
then migrates as it would without it.
The merge only creates or fast-forwards: a local history that diverged
from the remote's is an error. --dry-run fetches and merges them too,
since fetching is not migrating; it still writes nothing under
refs/work-issues/*.

doc/design/store-migration.md is the design.

```
git-work migrate [--dry-run] [--from REMOTE] [flags]
```

### Options

```
      --dry-run       Print what would be written and write nothing but the identity copy and the --from fetch
      --from string   Fetch and merge git-bug's refs/issues/* and refs/identities/* from this remote first
  -h, --help          help for migrate
```

### SEE ALSO

* [git-work](git-work.md)	 - A project tracker embedded in Git


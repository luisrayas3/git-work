#!/bin/sh
# //misc/migrate:bugs-to-issues.sh
#
# One-shot migration for the fork-foundations renames: the entity namespace
# refs/bugs/* -> refs/issues/* (be69e67), and the local storage directory
# .git/git-bug -> .git/git-work that came with it. Run it once per clone, from
# anywhere inside the repository, with `make migrate/issues-namespace`.
#
# This is safe to run because an entity's id does not depend on the namespace:
# the id is the create operation's id, derived from that operation's serialized
# bytes alone (//entity/dag:operation.go IdOperation), and the ref name is not
# part of any object. So the migration renames refs and drops derived local
# state; no object is rewritten and no id changes.
#
# Delete this script, and its Makefile target, once every clone has run it.

set -eu

old_storage=".git/git-bug"
storage=".git/git-work"   # //commands/execenv:env.go gitWorkNamespace

cd "$(git rev-parse --show-toplevel)"

# 0. the local storage directory moved with the root command rename. it holds
#    only derived state — cache, clocks, indexes, selection, lock — but moving
#    it rather than dropping it preserves the lamport clocks, which would
#    otherwise have to be re-witnessed from every entity.
if [ -d "$old_storage" ] && [ ! -d "$storage" ]; then
	mv "$old_storage" "$storage"
	echo "moved $old_storage to $storage"
fi

# 1. a held lock means termui, webui or another command is open on the store,
#    and would write through the old namespace behind our back.
if [ -e "$storage/lock" ]; then
	pid="$(cat "$storage/lock" 2>/dev/null || echo '?')"
	if kill -0 "$pid" 2>/dev/null; then
		echo "refusing to migrate: the store is locked by pid $pid." >&2
		echo "quit termui/webui and try again." >&2
		exit 1
	fi
	echo "removing a stale lock (pid $pid is not running)"
	rm -f "$storage/lock"
fi

local_bugs="$(git for-each-ref --format='%(refname)' 'refs/bugs/')"
remote_bugs="$(git for-each-ref --format='%(refname)' 'refs/remotes/*/bugs/*')"

if [ -z "$local_bugs" ] && [ -z "$remote_bugs" ]; then
	echo "nothing to migrate: no refs/bugs/* and no remote-tracking bugs refs."
	exit 0
fi

# 2. back up before touching anything. this is the undo: restoring is
#    copying refs/backup/bugs-premigration/* back over refs/bugs/*.
#    ref names cannot contain whitespace, so word splitting is safe here, and
#    a for loop keeps `exit` meaningful — inside a `... | while read` it would
#    only leave the subshell.
count=0
for ref in $local_bugs; do
	id="${ref#refs/bugs/}"
	git update-ref "refs/backup/bugs-premigration/$id" "$(git rev-parse "$ref")"
	count=$((count + 1))
done
[ "$count" -eq 0 ] || echo "backed up $count refs to refs/backup/bugs-premigration/"

# 3. local refs: create the new name at the same hash, then drop the old one.
for ref in $local_bugs; do
	id="${ref#refs/bugs/}"
	hash="$(git rev-parse "$ref")"
	if git rev-parse --verify --quiet "refs/issues/$id" >/dev/null; then
		existing="$(git rev-parse "refs/issues/$id")"
		if [ "$existing" != "$hash" ]; then
			echo "refusing to migrate $id: refs/issues/$id already exists at a different hash." >&2
			exit 1
		fi
	else
		git update-ref "refs/issues/$id" "$hash"
	fi
	git update-ref -d "$ref"
done

# 4. remote-tracking refs. these are a cache of the remote and could simply be
#    re-fetched, but renaming them keeps `git work pull` from re-importing
#    everything on the next run.
for ref in $remote_bugs; do
	rest="${ref#refs/remotes/}"
	remote="${rest%%/bugs/*}"
	id="${rest#*/bugs/}"
	git update-ref "refs/remotes/$remote/issues/$id" "$(git rev-parse "$ref")"
	git update-ref -d "$ref"
done

# 5. derived local state, all of it named after the old namespace. the cache
#    and index rebuild on the next command; the lamport clocks are re-witnessed
#    from the entities themselves (//entity/dag:clock.go ReadAllClocksNoCheck).
rm -f "$storage/cache/bugs" "$storage/clocks/bugs-create" "$storage/clocks/bugs-edit"
rm -rf "$storage/indexes/bugs"

echo
echo "local migration done. refs/issues/* now holds the tracker."
echo
echo "origin still has the old namespace. publishing the rename is destructive"
echo "and remote, so it is left to you — run, in this order:"
echo
echo "    git push origin 'refs/issues/*:refs/issues/*'"
echo "    git ls-remote origin 'refs/bugs/*' | cut -f2 | xargs git push origin -d"
echo
echo "between the two commands the tracker exists twice on origin, which is"
echo "harmless. afterwards, delete the backup refs:"
echo
echo "    git for-each-ref --format='%(refname)' refs/backup/bugs-premigration/ | xargs -n 1 git update-ref -d"

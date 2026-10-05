## git-work jira sync

Sync the store with the Jira project, both ways

### Synopsis

Converge the store and the bound Jira project, one issue at a time: a field
edited on one side reaches the other; a field edited on both takes Jira's value
and says so in a note on the issue; a new issue on either side appears on the
other. A run interrupted anywhere is finished by the next one.

Output is one JSON object per line: the schema changes derived from Jira, one
line per issue the run touched, left pending, skipped or failed on, then a
summary; an issue with nothing to do is only counted, as unchanged. Notes on
what the mapping leaves out go to stderr in a run that changed the schema;
git work jira schema prints them all. With IDs (id
prefixes or aliases, a Jira key included) only those issues are synced, with no
search. --full searches the whole project and marks issues deleted in Jira or
moved out of it as gone; more than 10 at once are held unless --accept-deletes.
A Jira issue created by an export names its local issue; one whose issue this
clone has not pulled is skipped, until --adopt DURATION says an issue that old
is lost for good and imports it (7d for a week; 0 for all). Two local copies of
one Jira issue are consolidated into the one that reached Jira first.

The exit status is 1 when an issue failed, the run stopped, or deletes were held.
sync never pushes: run git work push to publish.

```
git-work jira sync [ID...] [flags]
```

### Examples

```
git work jira sync --dry-run
git work jira sync
git work jira sync PROJ-12
git work jira sync --full
git work jira sync --full --adopt 7d
```

### Options

```
      --dry-run          Read both sides and write neither; print the plan
      --full             Search the whole project, and mark deleted or moved issues gone
      --accept-deletes   With --full, mark gone however many issues are missing
      --adopt string     Import a Jira issue created from an issue this clone lacks, once it is this old (7d, 12h, 0)
  -f, --format string    Select the output formatting style. Valid values are [json,text] (default "json")
  -h, --help             help for sync
```

### SEE ALSO

* [git-work jira](git-work_jira.md)	 - Sync with a Jira Cloud project


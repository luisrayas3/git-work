## git-work jira schema

Print the schema the sync derives from the Jira project

### Synopsis

Print the live schema with the Jira project's issue types, fields and values
adopted or added, each carrying its Jira id as an alias. Nothing is written;
warnings and errors on what does not map go to stderr, and with --verbose the
info notes too.

The first mapping is reviewed as this file, then imported; after that every
sync derives and imports it itself.

```
git-work jira schema [flags]
```

### Examples

```
git work jira schema > jira.yaml
$EDITOR jira.yaml
git work schema import jira.yaml --dry-run
git work schema import jira.yaml
```

### Options

```
  -f, --format string   Select the output formatting style. Valid values are [yaml,json] (default "yaml")
  -h, --help            help for schema
  -v, --verbose         Print the info notes too
```

### SEE ALSO

* [git-work jira](git-work_jira.md)	 - Sync with a Jira Cloud project


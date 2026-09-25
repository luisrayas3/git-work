## git-work schema

Show the schema

### Synopsis

Print the live schema: every type and field entity, compiled, in the document
a human edits. This is an alias of export.

The three built-in fields — title, type and archived — exist in code on every
type and are not printed unless an entity overrides one of them.

```
git-work schema [flags]
```

### Examples

```
git work schema
git work schema --format json | jq '.types.task.fields | keys'
```

### Options

```
  -f, --format string   Select the output formatting style. Valid values are [yaml,json] (default "yaml")
  -h, --help            help for schema
```

### SEE ALSO

* [git-work](git-work.md)	 - A project tracker embedded in Git
* [git-work schema archive](git-work_schema_archive.md)	 - Archive a type or a field
* [git-work schema export](git-work_schema_export.md)	 - Print the live schema as a document
* [git-work schema import](git-work_schema_import.md)	 - Apply a schema document to the store
* [git-work schema init](git-work_schema_init.md)	 - Create a preset's types and fields
* [git-work schema log](git-work_schema_log.md)	 - Print the schema's history
* [git-work schema rm](git-work_schema_rm.md)	 - Remove a type or a field from the local repository


## git-work schema export

Print the live schema as a document

### Synopsis

Print every unarchived type and field entity as the document a human edits,
YAML by default and JSON for an agent. --include-archive puts back the archived
types, fields and values, each marked archived: true.

The output is deterministic — types in order, then their fields, then each
field's values — and carries no ordinals, because list position is the order.
Importing what export printed emits no operation, with or without the
archived, which is the round trip: an import leaves what a file omits as it
stands.

```
git-work schema export [flags]
```

### Examples

```
git work schema export > schema.yaml
$EDITOR schema.yaml
git work schema import schema.yaml
```

### Options

```
  -f, --format string     Select the output formatting style. Valid values are [yaml,json] (default "yaml")
  -h, --help              help for export
      --include-archive   include the archived types, fields and values, marked archived: true
```

### SEE ALSO

* [git-work schema](git-work_schema.md)	 - Show the schema


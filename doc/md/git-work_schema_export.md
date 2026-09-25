## git-work schema export

Print the live schema as a document

### Synopsis

Print every type and field entity as the document a human edits, YAML by
default and JSON for an agent.

The output is deterministic — types in order, then their fields, then each
field's values — and carries no ordinals, because list position is the order.
Importing what export printed emits no operation, which is the round trip.

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
  -f, --format string   Select the output formatting style. Valid values are [yaml,json] (default "yaml")
  -h, --help            help for export
```

### SEE ALSO

* [git-work schema](git-work_schema.md)	 - Show the schema


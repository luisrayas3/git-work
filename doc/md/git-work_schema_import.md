## git-work schema import

Apply a schema document to the store

### Synopsis

Read a schema document, YAML or JSON, from a file or standard input, and write
what differs from the store: one commit per entity, nothing at all where the
document and the store already agree.

The whole document is validated before anything is written. An import is an
upsert: a type or a field the document does not mention is left alone, so a
partial file from anywhere can never archive anyone's work. --prune archives
what the document does not mention, which is the only way to remove.

The ids of the entities it creates are printed, one per line, and nothing else.

```
git-work schema import FILE|- [flags]
```

### Examples

```
git work schema import schema.yaml
git work schema import - --dry-run < schema.yaml
```

### Options

```
      --prune     Archive the types and fields the document does not mention
      --dry-run   Print the changes that would be committed, and write nothing
  -h, --help      help for import
```

### SEE ALSO

* [git-work schema](git-work_schema.md)	 - Show the schema


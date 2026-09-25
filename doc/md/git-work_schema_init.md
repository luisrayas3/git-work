## git-work schema init

Create a preset's types and fields

### Synopsis

Instantiate an embedded preset as config entities: one entity per type and
one per (type, field). With no argument, the jira preset.

It refuses when a field entity already exists, archived or not, because a
preset is a starting point and not a merge; to change a schema that is already
there, export it, edit it and import it.

The ids of the entities it creates are printed, one per line, and nothing else.

```
git-work schema init [PRESET] [flags]
```

### Examples

```
git work schema init
git work schema init jira
```

### Options

```
      --dry-run   Print the changes that would be committed, and write nothing
  -h, --help      help for init
```

### SEE ALSO

* [git-work schema](git-work_schema.md)	 - Show the schema


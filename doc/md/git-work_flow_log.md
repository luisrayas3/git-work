## git-work flow log

Print the flows' history

### Synopsis

Print the history of one flow, or of every flow: every operation, oldest
first and one JSON object per line, so it says who changed a flow, when, and
to what.

--format text prints one line per operation instead.

```
git-work flow log [NAME] [flags]
```

### Options

```
  -f, --format string   Select the output formatting style. Valid values are [json,text] (default "json")
  -h, --help            help for log
```

### SEE ALSO

* [git-work flow](git-work_flow.md)	 - List the flows


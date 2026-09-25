## git-work flow run

Run a flow

### Synopsis

Run a flow and print what it returned.

NAME is a flow in the store, or "-" to run a script from standard input
without importing it: one function, the same shape import takes.

KWARGS is a JSON object of the flow's arguments, given as the argument or on
standard input as "-" (not when the script is). Defaults in the signature fill
what the object omits, an unknown key is an error naming the arguments, and an
argument with no default that nobody named is an error too. `git work flow`
lists them.

A flow that returns a value prints it as JSON; one that returns nothing prints
nothing.

A flow that calls a view draws it here and blocks until you quit it, so a
saved view is a flow that calls one. That needs a terminal: without one, the
view call is what fails, and a flow that draws nothing runs as it always did.

```
git-work flow run NAME|- [KWARGS|-] [flags]
```

### Examples

```
git work flow run board
git work flow run board '{"iteration":"2026-Q4-S3"}'
echo '{"iteration":"current"}' | git work flow run board -
git work flow run - '{"status":"done"}' < scratch.star
```

### Options

```
      --gui    Draw what the flow renders in the browser
  -h, --help   help for run
```

### SEE ALSO

* [git-work flow](git-work_flow.md)	 - List the flows


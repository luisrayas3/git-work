## git-work view

Draw the issues

### Synopsis

Draw the issues: a list, a board, a gantt chart, or one issue.

A view takes one JSON object of keyword arguments and nothing else. Every kind
but `show` takes a `query`, the jq program its issues come from, so a kanban
with no flow at all is one command:

  git work view board '{"query":"map(select(.fields.status != \"done\"))","columns":"status"}'

The view draws in the terminal and blocks until you quit it. Edits made in it
are written through the same path a command writes through.

### Options

```
  -h, --help   help for view
```

### SEE ALSO

* [git-work](git-work.md)	 - A project tracker embedded in Git
* [git-work view board](git-work_view_board.md)	 - Draw a board
* [git-work view gantt](git-work_view_gantt.md)	 - Draw a gantt
* [git-work view list](git-work_view_list.md)	 - Draw a list
* [git-work view show](git-work_view_show.md)	 - Draw a show


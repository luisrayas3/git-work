## git-work webui

Launch the web UI

### Synopsis

Launch the web UI.

Available git config:
  git-bug.webui.open [bool]: control the automatic opening of the web UI in the default browser


```
git-work webui [flags]
```

### Options

```
      --bind string    Network address to bind to (default to 127.0.0.1) (default "127.0.0.1")
  -p, --port int       Port to listen on (default to random available port)
      --open           Automatically open the web UI in the default browser
      --no-open        Prevent the automatic opening of the web UI in the default browser
      --read-only      Whether to run the web UI in read-only mode
      --dev            Enable development mode (enables --log-errors, GraphQL playground, relaxed WebSocket origin check)
      --log-errors     Whether to log errors
  -q, --query string   The query to open in the web UI bug list
  -h, --help           help for webui
```

### SEE ALSO

* [git-work](git-work.md)	 - A project tracker embedded in Git


## git-work issue comment edit

Edit an existing comment on an issue

### Synopsis

Replace the body of one comment, given as the argument or on standard input.
COMMENT_ID is the comment's own id, as get and comment new print it.

With --base, the edit is refused, and nothing written, if the comment's
message_hash, as get prints it, is no longer HASH: the comment changed since
it was read. Get it again and redo the edit on its current text.

```
git-work issue comment edit COMMENT_ID BODY|- [flags]
```

### Options

```
      --base HASH   Refuse the edit unless the comment's message_hash is still HASH
  -h, --help        help for edit
```

### SEE ALSO

* [git-work issue comment](git-work_issue_comment.md)	 - Write an issue's comments


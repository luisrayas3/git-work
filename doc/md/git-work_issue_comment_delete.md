## git-work issue comment delete

Delete a comment from an issue

### Synopsis

Delete one comment, everywhere it is published: every clone on its next pull,
and Jira on its next sync. Nothing is printed.
COMMENT_ID is the comment's own id, as get and comment new print it.

A delete is final: there is no undelete. The text is not destroyed in git;
the issue's log still holds it. The description, the first comment,
can not be deleted; rewrite it with comment edit.

```
git-work issue comment delete COMMENT_ID [flags]
```

### Options

```
  -h, --help   help for delete
```

### SEE ALSO

* [git-work issue comment](git-work_issue_comment.md)	 - Write an issue's comments


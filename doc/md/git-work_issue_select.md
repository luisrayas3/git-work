## git-work issue select

Select an issue for implicit use in future commands

### Synopsis

Select an issue for implicit use in future commands.

This command allows you to omit any issue ID argument, for example:
  git work issue show
instead of
  git work issue show 2f153ca

The complementary command is "git work issue deselect" performing the opposite operation.


```
git-work issue select ISSUE_ID [flags]
```

### Examples

```
git work issue select 2f15
git work issue comment
git work issue status

```

### Options

```
  -h, --help   help for select
```

### SEE ALSO

* [git-work issue](git-work_issue.md)	 - List issues


## git-work user

List identities, one line each

### Synopsis

List the identities this repository knows about, one line per identity:
the id and the display name.

This is the human form of `git work user list`, which prints the same
identities as JSON and is the form to script against; --format json prints
that here too.

```
git-work user [flags]
```

### Options

```
  -f, --format string   Select the output formatting style. Valid values are [text,json] (default "text")
  -h, --help            help for user
```

### SEE ALSO

* [git-work](git-work.md)	 - A project tracker embedded in Git
* [git-work user adopt](git-work_user_adopt.md)	 - Adopt an existing identity as your own
* [git-work user list](git-work_user_list.md)	 - List identities, JSON out
* [git-work user me](git-work_user_me.md)	 - Display the identity you write as
* [git-work user new](git-work_user_new.md)	 - Create a new identity


## git-work jira

Sync with a Jira Cloud project

### Synopsis

Keep this store and one Jira Cloud project converged, in both directions,
Jira winning a field both sides edited.

The clone is bound by git config: git-work.jira.url (https://<site>.atlassian.net),
git-work.jira.project (the project key) and git-work.jira.email. The API token
is JIRA_API_TOKEN, else git's credential helpers.

```
git-work jira [flags]
```

### Options

```
  -h, --help   help for jira
```

### SEE ALSO

* [git-work](git-work.md)	 - A project tracker embedded in Git
* [git-work jira schema](git-work_jira_schema.md)	 - Print the schema the sync derives from the Jira project
* [git-work jira sync](git-work_jira_sync.md)	 - Sync the store with the Jira project, both ways


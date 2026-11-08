# The Jira sandbox

What a Jira Cloud site must hold to test the Jira sync against a real Jira,
and how the sync is pointed at it.
The tests in the repository need no site:
they run against the fake Jira, `jira/jiratest`,
whose two canned sites (`CompanySite` and `TeamSite` in `jira/jiratest/fixtures.go`)
model a company-managed and a team-managed project.
A fact seen on a live site is the strongest mark [the API reference](reference/jira-api.md) gives,
and the way to confirm one it marks unconfirmed.

Creating a site needs an Atlassian account.

## The site

A free Jira Cloud site with two projects:
one shaped like a company's Jira, the realistic case,
and one configured differently enough
that a mapping working for both cannot be an accident.

### A company-managed project

- **Issue types covering the hierarchy**: Epic, Story, Task, Bug and Sub-task,
  so that Epic → Story → Sub-task exists as `parent` relations,
  and Story, Task and Bug share a level, so that a type change by edit can be tried.
- **A custom workflow** with a status in each category (to do, in progress, done),
  and **a status whose name is not its category's**,
  such as "Ready for QA" in progress or "Won't Do" in done:
  a status's meaning is its category, and a site where names and categories coincide
  cannot show a mistake that reads the name.
- **Two workflows**, so that issue types of one level differ in workflow,
  ideally two with the same set of statuses,
  which is the case the [type-change rule](reference/jira-api.md#changing-an-issue-type-by-edit) cannot see.
- **Transitions that are not global**: a status reachable only from one other status,
  so that a transition unavailable from where the issue sits can be met.
- **A required field on one type** with no default (such as priority on Bug),
  and a transition that requires a resolution.
- **Priorities**, the site's own set rather than only the default five.
- **A board with sprints**: a closed one, an active one and a future one.
- **Custom fields of several kinds**: a number for story points, a date, a single select.
- **A handful of issues** exercising all of it:
  an epic with children, a sub-task, a link between two issues (blocks / is blocked by),
  assignees, comments, a resolved issue, an issue moved in from another project.

### A team-managed project

- Its own issue types, with an Epic and a Subtask level.
- Project-scoped custom fields and statuses, so that one name exists twice on the site.
- A status set unlike the first project's.

## Binding and credentials

A clone is bound to the site as [the Jira sync](design/jira.md#binding-and-credentials) says:
`git-work.jira.url` (`https://<site>.atlassian.net`,
or `https://api.atlassian.com/ex/jira/<cloudId>` for a scoped token),
`git-work.jira.project` and `git-work.jira.email` in git config,
and an API token from `JIRA_API_TOKEN` or git's credential helpers:
never in the repository, in git config or on a command line.
The client authenticates with Basic email and token (`jira/jiraapi`).

## What to capture

`git work jira schema -v` against the site shows what discovery reads and what Derive makes of it.
To compare the site with the fake, the responses worth keeping are
`GET /rest/api/3/field`, `GET /rest/api/3/project/<KEY>`,
`GET /rest/api/3/project/<KEY>/statuses`, the createmeta of each issue type,
`GET /rest/api/3/priority/search`, `GET /rest/api/3/issueLinkType`,
and a few issues read with `GET /rest/api/3/issue/<KEY>` and their comments.
A capture committed to the repository is sanitised first:
no `emailAddress`, `displayName`, `avatarUrls` or account ids in any user object,
and nothing that names a company.
The shape is what matters, not the contents.

What a live site confirms goes into [the API reference](reference/jira-api.md),
the fact marked as seen on a live site,
and into the fake where the fake answers otherwise.

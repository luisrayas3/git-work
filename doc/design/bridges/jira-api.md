# Jira Cloud REST API reference

What Jira Cloud's REST API does,
as far as the Jira sync relies on it:
**REST v3** (the platform) and **Jira Software REST 1.0** (agile).
It is a reference about Jira, not about git-work;
where it names what the sync does about a fact, it links [the Jira sync](../jira.md).

## How to read this

- A fact stated plainly is **confirmed**:
  by the OpenAPI spec of the operation its section names,
  unless the line names another source
  (an Atlassian page, a capture from a real site in an open-source client's fixtures,
  or Atlassian's own client).
- A fact marked **Unconfirmed** is inferred:
  from community reports, from general Jira behaviour, or from the spec's examples alone.
  It says what is **assumed**, which is the case the sync is built to survive
  (the harder one where the truth could go either way),
  and, where the fake Jira (`jira/jiratest`) has an answer for it, what the fake answers.
- **The spec's example is unrealistic** marks an official example
  that must not be taken as the wire shape.
- No fact here has been confirmed on a live site yet:
  [the sandbox](jira-sandbox.md) is what that needs.

## Sources

- The platform spec, `https://developer.atlassian.com/cloud/jira/platform/swagger-v3.v3.json`,
  rendered at `https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-<group>/`.
- The agile spec, `https://developer.atlassian.com/cloud/jira/software/swagger.v3.json`,
  rendered at `https://developer.atlassian.com/cloud/jira/software/rest/api-group-<group>/`.
- Atlassian's pages, each cited where it is used:
  rate limiting (`…/cloud/jira/platform/rate-limiting/`),
  search and reconcile (`…/cloud/jira/platform/search-and-reconcile/`),
  basic auth (`…/cloud/jira/platform/basic-auth-for-rest-apis/`),
  the v3 intro (`…/cloud/jira/platform/rest/v3/intro/`),
  the issue-linking model (`…/cloud/jira/platform/issue-linking-model/`),
  the ADF pages (`…/cloud/jira/platform/apis/document/`),
  the Jira Software REST intro (`…/cloud/jira/software/rest/intro/`),
  the JQL fields reference (`https://support.atlassian.com/jira-software-cloud/docs/jql-fields/`),
  and the deprecation notices named in place.
- Open-source clients, for captures of real responses and for Atlassian's own reading of the API:
  andygrunwald/go-jira (its `testing/mock-data` fixtures),
  ctreminiom/go-atlassian, pycontribs/jira, MrRefactoring/jira.js,
  and Atlassian's Java client JRJC.
- The specs are not vendored: fetch them from the URLs above to re-check a fact.

## Conventions

### Base URL and formats

- The base URL is `https://<site>.atlassian.net`.
  For OAuth 2.0 (3LO) and for scoped API tokens it is `https://api.atlassian.com/ex/jira/{cloudId}` (v3 intro).
- JSON in and out: `Content-Type: application/json` and `Accept: application/json`.
  **Unconfirmed**: a POST or PUT with a body and no `Content-Type` answers 415.
  Assumed: so; the header is always sent.
- Ids are **strings** in the platform API (`"id":"10002"`),
  except `statusCategory.id` (an int) and the agile ids of sprints and boards (ints).
- A multi-valued query parameter is either comma-separated (`fields=summary,status`)
  or repeated (`fields=a&fields=b`); the spec states both for `fields`, `properties` and others.
- Errors are an [`ErrorCollection`](#the-error-body).
- The response header **`X-AAccountId`** carries the caller's accountId on every response (v3 intro).

### Pagination

There are three paging models:

1. **Offset page bean**: `startAt`, `maxResults`, `total`, `isLast`, `values[]`,
   with optional `self` and `nextPage`.
   Used by `/changelog`, `/priority/search`, `/user/bulk`, `/statuses/search`, `/workflows/search`,
   `/comment/list`, and the agile `/board` and `/board/{id}/sprint`.
2. **Offset, legacy shape**: `startAt`, `maxResults`, `total` and a named array
   (`comments`, `issueTypes`, `fields`, `histories`), with **no `isLast`**.
   Used by comments, the paginated createmeta, and the embedded changelog.
3. **Token**: `nextPageToken` and `isLast`, with **no total**.
   Used by `/search/jql` and `/changelog/bulkfetch`.

Unpaged plain arrays: `/field`, `/status`, `/statuscategory`, `/priority`, `/resolution`,
`/project/{k}/statuses`, `/issuetype/project`, and `/user/search`
(offset-paged by its parameters, but its response is a bare array).

The server may return fewer than `maxResults` items when more exist,
which search states explicitly,
so a page short of `maxResults` is not the last;
the agile intro adds that "`total` may change while the client requests the next pages …
always assume that the requested page can be empty".

## Authentication

### Basic auth with an API token

Source: the basic-auth page.

```
Authorization: Basic base64("<email>:<api_token>")
```

- Only an API token is accepted as the password:
  "Authentication using passwords has been deprecated."
- After repeated failures, `X-Seraph-LoginReason: AUTHENTICATION_DENIED` means
  the password was not even checked (CAPTCHA).
  **Unconfirmed**: the status that comes with it is 401.
- **Unconfirmed**: **scoped API tokens** (granular scopes) are sent to
  `https://api.atlassian.com/ex/jira/{cloudId}/rest/api/3/...`, still with Basic auth,
  and `cloudId` comes from `https://<site>.atlassian.net/_edge/tenant_info`.
  Source: community reports.
  Assumed: the base URL is configured whole, never derived.
- **Unconfirmed**: API tokens expire (one year by default, set at creation).
  Assumed: so; a 401 reads as "token invalid or expired".
- API-token traffic is governed by the burst limits, not the points quotas
  (the rate-limiting page: "API token-based traffic is not affected by this change").

### Bearer tokens

- **OAuth 2.0 (3LO)**: `Authorization: Bearer <access_token>`,
  with the base `https://api.atlassian.com/ex/jira/{cloudId}`.
  The spec names scopes per operation
  (`read:jira-work`, `write:jira-work`, `read:jira-user`, `manage:jira-configuration`;
  agile: `read:sprint:jira-software`, `write:sprint:jira-software`, `write:issue:jira-software`,
  `write:board-scope:jira-software`).
- **Unconfirmed**: personal access tokens (`Authorization: Bearer <PAT>`) are Data Center's;
  Cloud has none.

### Anonymous calls

Many operations "can be accessed anonymously" (spec).
A request with **no** `Authorization` header runs as anonymous
and may answer **200 with nothing in it** (search, user search);
bad credentials answer 401.
So a 200 from search does not show the credentials were used:
`GET /myself` answers 401 to an anonymous caller.
The fake answers the same: 200 with nothing on search, 404 on issue reads, 401 on writes and on `/myself`.

### Who the caller is

`GET /rest/api/3/myself` (`getCurrentUser`), scope `read:jira-user`.

- Query: `expand` = `groups`, `applicationRoles`.
- 200: a `User`. 401 for bad credentials.

```json
{
  "self": "https://your-domain.atlassian.net/rest/api/3/user?accountId=5b10a2844c20165700ede21g",
  "accountId": "5b10a2844c20165700ede21g",
  "accountType": "atlassian",
  "emailAddress": "mia@example.com",
  "avatarUrls": {"16x16": "…", "24x24": "…", "32x32": "…", "48x48": "…"},
  "displayName": "Mia Krystof",
  "active": true,
  "timeZone": "Australia/Sydney",
  "locale": "en_US",
  "groups": {"size": 3, "items": []},
  "applicationRoles": {"size": 1, "items": []}
}
```

- `timeZone` is the zone [JQL date literals](#jql-for-incremental-sync) are read in.
- `key` and `name` are present but empty in the examples: removed for GDPR.

### Server info

`GET /rest/api/3/serverInfo` (`getServerInfo`); anonymous is allowed.

```json
{
  "baseUrl": "https://your-domain.atlassian.net",
  "version": "1001.0.0-SNAPSHOT",
  "versionNumbers": [5, 0, 0],
  "deploymentType": "Cloud",
  "buildNumber": 582,
  "buildDate": "2020-03-26T22:20:59.000+0000",
  "serverTime": "2020-03-31T16:43:50.000+0000",
  "scmInfo": "1f51473f5c7b75c1a69a0090f4832cdc5053702a",
  "serverTitle": "My Jira instance",
  "serverTimeZone": "Australia/Sydney",
  "defaultLocale": {"locale": "en_AU"}
}
```

- `deploymentType` is "always returned as *Cloud*" in Cloud.
  **Unconfirmed**: on Data Center it is `"Server"` or `"DataCenter"`.
  Assumed: anything other than `"Cloud"` is not Cloud.
- `serverTime` is in the issue timestamps' format ([the server clock](#the-server-clock)).
- `healthChecks` is "Deprecated and no longer returned".

## Searching

### Enhanced search

`GET /rest/api/3/search/jql` (`searchAndReconsileIssuesUsingJql`)
and `POST /rest/api/3/search/jql` (`searchAndReconsileIssuesUsingJqlPost`; the misspelling is Atlassian's).
Scope `read:jira-work`; anonymous is allowed.

Query parameters (GET), or the same names in the POST body (`SearchAndReconcileRequestBean`):

| param | type | default | notes (quoted or condensed from the spec) |
|---|---|---|---|
| `jql` | string | — | "requires a bounded query. A bounded query is a query with a search restriction." `order by key desc` is unbounded (refused); `project = X` is bounded. "`orderBy` clause can contain a maximum of 7 fields." |
| `nextPageToken` | string | null | Omitted for the first page. The token "will expire in 7 days". |
| `maxResults` | int32 | 50 | "API may return fewer items per page where a large number of fields or properties are requested. The greatest number of items returned per page is achieved when requesting `id` or `key` only. It returns max 5000 issues." It is advisory. |
| `fields` | string[] | **`id`** | `*all`, `*navigable`, `id`, a field id, or `-field` to exclude. "**By default, this resource returns IDs only.**" `*navigable` leaves out non-navigable fields; `*all` includes `comment` (a truncated page object) and heavy fields. |
| `expand` | string | — | **A comma-delimited string, in the POST body too** ("unlike the majority… `expand` is defined as a comma-delimited string"): `renderedFields`, `names`, `schema`, `transitions`, `operations`, `editmeta`, `changelog`, `versionedRepresentations`. |
| `properties` | string[] | — | Up to 5 issue property keys. |
| `fieldsByKeys` | bool | false | Fields named by key instead of id. |
| `failFast` | bool | false | Fail early if not all field data can be retrieved. |
| `reconcileIssues` | int64[] | — | "Strong consistency issue ids to be reconciled with search results. Accepts max 50 ids. This list of ids should be consistent with each paginated request across different pages." Numeric **ids**, not keys. |
| `includeArchivedProjects` | bool | false | Issues in archived projects are left out by default. |

POST body:

```json
{
  "jql": "project = PROJ AND updated >= \"2026/09/27 10:00\" ORDER BY updated ASC, key ASC",
  "fields": ["summary", "status", "issuetype", "updated"],
  "expand": "names,schema",
  "maxResults": 100,
  "reconcileIssues": [10042, 10043]
}
```

Page 2 and later send the same body with `"nextPageToken": "<the previous page's>"`.

200 (`SearchAndReconcileResults`):

```json
{
  "issues": [
    {
      "expand": "operations,versionedRepresentations,editmeta,changelog,renderedFields",
      "id": "10002",
      "self": "https://your-domain.atlassian.net/rest/api/3/issue/10002",
      "key": "PROJ-1",
      "fields": { "summary": "…", "updated": "2026-09-27T10:03:11.123+0000" }
    }
  ],
  "nextPageToken": "CAEaAggD",
  "isLast": false,
  "names": {"summary": "Summary"},
  "schema": {"summary": {"type": "string", "system": "summary"}},
  "warnings": [ {"type": "CLAUSE_LIMIT_EXCEEDED", "message": "…", "details": {}} ]
}
```

- The `reconcileIssues` list is the same on every page of one search;
  the fake answers 400 to one that changes.
- On the last page the token is absent or null, and `isLast` is true:
  the parameter's text says "not included", the schema "will be null".
  The fake omits it and sets `isLast`.
- **There is no `total`, `startAt` or `maxResults` in the response**;
  a count is [approximate](#approximate-count).
- `names` and `schema` are present only when expanded.
- `warnings` is "Experimental … rolling out behind a feature flag and may be absent, empty".
- 400 for an invalid search (a JQL parse error, an unbounded query), an `ErrorCollection` such as
  `{"errorMessages":["Error in the JQL Query: …"],"errors":{},"warningMessages":[]}`;
  **Unconfirmed**: the message text, and that a clause naming a project the caller cannot see is a
  400 "The value 'X' does not exist for the field 'project'.".
  401 for bad credentials.

### The old search is removed

`GET/POST /rest/api/3/search` (`searchForIssuesUsingJql`, `searchForIssuesUsingJqlPost`),
and the same under `/rest/api/2`, are `deprecated: true`:
"Endpoint is currently being removed" (CHANGE-2046).
They now answer **410 Gone** with `{"errorMessages":["The requested API has been removed. …"]}`
(atlassian-mcp-server issue 70, February 2026);
the removal rolled out per site through about October 2025
(Adaptavist's breaking-changes note, the CHANGE-2046 community thread).
The old parameters were `startAt`, `maxResults`, `validateQuery`, `fields` (default `*navigable`),
`expand` (an array in the POST body), `properties` and `fieldsByKeys`,
and the response carried `startAt`, `maxResults`, `total` and `warningMessages`.
It is still the search API on Data Center.
The fake answers 410 with that body.

### Approximate count

`POST /rest/api/3/search/approximate-count` (`countIssues`).

- Body `{"jql":"project = HSP"}`; the JQL must be bounded.
- 200: `{"count":153}`, "an estimated count … Recent updates might not be immediately visible".
  400 if the JQL cannot be parsed.

### Search index lag

From the search-and-reconcile page:

- "The API doesn't provide read-after-write consistency by default … subsequent search operations …
  without the `reconcileIssues` parameter may return stale or outdated data … for some time."
- "The delay might vary from a few seconds to minutes, depending on the operation.
  The majority of modifications are shown within seconds."
- `reconcileIssues`: at most 50 ids, and "consistency is ensured only for the specified issues";
  a reconciled issue is still returned only if it matches the JQL.
- **Unconfirmed**: `GET /rest/api/3/issue/{id}` reads the database, not the index,
  and is read-after-write consistent.
  The docs single out only search.
  Assumed: not trusted; a write is settled against a later read by Jira's clock
  ([the Jira sync](../jira.md#the-base-is-what-jira-was-seen-to-hold)).
  The fake is consistent by default, and lags with `WithStaleReads`.
- **Unconfirmed**: an issue property reaches the search index with the same lag as fields.
  The fake indexes properties with the search's lag.

### JQL for incremental sync

From the JQL fields reference (Updated, Created and Resolved), verbatim:

> Note that if a time-component is not specified, midnight will be assumed. Please note that the search results will be relative to your configured time zone (which is by default the Jira server's time zone). Use one of the following formats: "yyyy/MM/dd HH:mm" "yyyy-MM-dd HH:mm" "yyyy/MM/dd" "yyyy-MM-dd" Or use "w" (weeks), "d" (days), "h" (hours) or "m" (minutes) to specify a date relative to the current time. The default is "m" (minutes). Be sure to use quote-marks ( " )

```
project = PROJ AND updated >= "2026/09/27 10:03" ORDER BY updated ASC, key ASC
```

- **Minute granularity**: a literal has no seconds.
- **Zone**: a literal is read in the **caller's profile zone** (`/myself` `timeZone`),
  falling back to the site's (`/serverInfo` `serverTimeZone`);
  integrators report the same.
  [Timestamps](#timestamps) come back in the site's default user zone,
  which can differ.
- Relative values (`updated >= "-15m"`) avoid zones but use the server's clock.
- `parent = PROJ-12` works for all project types:
  "As of February 2024, Epic link function has been retired in favor of Parent."
- `sprint = 999`, `sprint in openSprints()`, `sprint in closedSprints()`.
- **Unconfirmed**: `key` and `issuekey` accept an old key after a move and match the moved issue.
  The fake resolves them so.
- **Unconfirmed**: how JQL answers an `id` or `key` naming a deleted issue.
  Assumed: a 400, the worse case; the fake answers 400.

## Reading issues

### Reading an issue

`GET /rest/api/3/issue/{issueIdOrKey}` (`getIssue`), scope `read:jira-work`.

| param | default | notes |
|---|---|---|
| `fields` | `*all` | "All fields are returned by default. This differs from [search] where the default is all navigable fields". `*all`, `*navigable`, a list, or `-x` to exclude. |
| `fieldsByKeys` | false | |
| `expand` | — | `renderedFields`, `names`, `schema`, `transitions`, `editmeta`, `changelog`, `versionedRepresentations` (with which "the `fields` parameter is ignored") |
| `properties` | null | `*all`, keys, or `-key` |
| `updateHistory` | false | Adds the issue to the caller's recently viewed. |
| `failFast` | false | |

- 200: `IssueBean`: `{expand, id, key, self, fields, renderedFields?, names?, schema?, transitions?, editmeta?, changelog?, operations?, properties?, versionedRepresentations?}`.
- 401 for bad credentials.
- **404 "if the issue is not found or the user does not have permission to view it"**:
  a 404 does not tell deleted, moved out of view and permission lost apart.

The spec's example is unrealistic:
it has `fields.comment` as an array, `fields.updated: 1`, a `sub-tasks` entry shaped like a link,
and a `watcher` field.
The shape below follows the platform's system field ids and the spec's component schemas
(`StatusDetails`, `IssueTypeDetails`, `UserDetails`, `Priority`, `IssueLink`),
checked against the go-jira and go-atlassian models and fixtures.

```json
{
  "expand": "renderedFields,names,schema,operations,editmeta,changelog,versionedRepresentations",
  "id": "10042",
  "key": "PROJ-12",
  "self": "https://your-domain.atlassian.net/rest/api/3/issue/10042",
  "fields": {
    "summary": "Checkout fails for guest users",
    "description": {
      "type": "doc", "version": 1,
      "content": [ {"type": "paragraph", "content": [ {"type": "text", "text": "Steps…"} ]} ]
    },
    "issuetype": {
      "self": "https://your-domain.atlassian.net/rest/api/3/issuetype/10001",
      "id": "10001", "description": "A small piece of work.", "iconUrl": "…",
      "name": "Task", "subtask": false, "avatarId": 10318, "hierarchyLevel": 0,
      "entityId": "9d7dd6f7-e8b6-4247-954b-7b2c9b2a5ba2",
      "scope": {"type": "PROJECT", "project": {"id": "10000"}}
    },
    "project": {
      "self": "…/rest/api/3/project/10000", "id": "10000", "key": "PROJ", "name": "Project",
      "projectTypeKey": "software", "simplified": false, "avatarUrls": {}
    },
    "status": {
      "self": "…/rest/api/3/status/10001", "description": "", "iconUrl": "…",
      "name": "In Progress", "id": "3",
      "statusCategory": {
        "self": "…/rest/api/3/statuscategory/4", "id": 4, "key": "indeterminate",
        "colorName": "yellow", "name": "In Progress"
      }
    },
    "priority": {
      "self": "…/rest/api/3/priority/3", "iconUrl": "…/images/icons/priorities/medium.svg",
      "name": "Medium", "id": "3"
    },
    "assignee": {
      "self": "…/rest/api/3/user?accountId=5b10…", "accountId": "5b10a2844c20165700ede21g",
      "emailAddress": "mia@example.com", "avatarUrls": {}, "displayName": "Mia Krystof",
      "active": true, "timeZone": "Australia/Sydney", "accountType": "atlassian"
    },
    "reporter": { "accountId": "…", "displayName": "…", "active": true, "accountType": "atlassian" },
    "creator": { "accountId": "…" },
    "labels": ["checkout", "guest"],
    "duedate": "2026-10-15",
    "created": "2026-09-01T09:12:44.513-0700",
    "updated": "2026-09-27T10:03:11.123-0700",
    "resolution": null,
    "resolutiondate": null,
    "statuscategorychangedate": "2026-09-20T11:00:00.000-0700",
    "parent": {
      "id": "10097", "key": "PROJ-3", "self": "…/rest/api/3/issue/10097",
      "fields": {
        "summary": "Guest checkout epic",
        "status": { "…": "StatusDetails" },
        "priority": { "…": "Priority" },
        "issuetype": { "id": "10000", "name": "Epic", "subtask": false, "hierarchyLevel": 1 }
      }
    },
    "subtasks": [
      {
        "id": "10050", "key": "PROJ-13", "self": "…",
        "fields": {
          "summary": "…", "status": {},
          "priority": {},
          "issuetype": { "name": "Sub-task", "subtask": true, "hierarchyLevel": -1 }
        }
      }
    ],
    "issuelinks": [
      {
        "id": "10001", "self": "…/rest/api/3/issueLink/10001",
        "type": { "id": "10000", "name": "Blocks", "inward": "is blocked by", "outward": "blocks", "self": "…" },
        "outwardIssue": {
          "id": "10060", "key": "PROJ-20", "self": "…",
          "fields": { "summary": "…", "status": {}, "priority": {}, "issuetype": {} }
        }
      }
    ],
    "comment": { "comments": [], "self": "…/issue/10042/comment", "maxResults": 0, "total": 0, "startAt": 0 },
    "customfield_10020": [
      {
        "id": 37, "name": "PROJ Sprint 4", "state": "active", "boardId": 5,
        "goal": "Ship guest checkout",
        "startDate": "2026-09-22T08:00:00.000Z",
        "endDate": "2026-10-06T08:00:00.000Z"
      }
    ],
    "customfield_10016": 3.0,
    "customfield_10019": "0|i0003r:"
  }
}
```

### The issue's fields

- **summary**: a string.
  **Unconfirmed**: at most 255 characters.
  Assumed: so; the fake refuses a longer one with `errors.summary`.
- **description**: ADF ([ADF](#adf)) or `null`.
  `environment` and every `textarea` custom field are ADF too;
  single-line `textfield` custom fields are plain strings (spec, createIssue).
  With `expand=renderedFields`, `renderedFields.description` is HTML.
- **issuetype**: `IssueTypeDetails`:
  `id, name, description, iconUrl, subtask, avatarId, hierarchyLevel, entityId (a uuid, team-managed), scope`;
  levels are in [issue types and hierarchy levels](#issue-types-and-hierarchy-levels).
- **status**: `StatusDetails` `{id, name, description, iconUrl, self, statusCategory, scope?}`,
  `statusCategory` being `{id: int, key, name, colorName, self}`
  ([status categories](#status-categories)).
  A status's meaning is its category's `key`; its name is the site's to change.
- **priority**: `{id, name, iconUrl, self}`.
  **Unconfirmed**: `null` when priorities are disabled or unset.
- **assignee, reporter, creator**: `UserDetails`
  `{self, accountId, accountType, emailAddress?, avatarUrls, displayName, active, timeZone?}`;
  `assignee` is `null` when unassigned.
  `emailAddress` "may be returned as null"; it is also seen absent, and is blank for a deleted user
  ([privacy](#privacy-and-account-types)).
  A deleted user's `displayName` is "Former user" or alike;
  a corrupted record's `accountId` is `unknown` (spec, `UserDetails`).
- **labels**: an array of strings.
  **Unconfirmed**: a label cannot contain a space.
  Assumed: so; the fake refuses one with `errors.labels`.
- **duedate**: `"YYYY-MM-DD"` or null.
- **created, updated, resolutiondate**: [timestamps](#timestamps).
- **parent**: the unified parent field,
  for sub-tasks (the parent is the level-0 issue) and for issues under an epic,
  in company-managed and team-managed projects alike
  (the Epic Link and Parent Link deprecation announcement).
  Its shape is `{"id","key","self","fields":{summary, status, priority, issuetype}}`,
  `fields` sometimes with `assignee` (go-atlassian `ParentFieldsScheme`).
  With no parent the key is absent **or** `null` (a go-jira fixture has `"parent": null`).
  **Unconfirmed**: levels above epic (Plans' "Parent Link") are exposed through `parent` too.
- **subtasks**: an array of `{id,key,self,fields{summary,status,priority,issuetype}}`, read-only;
  the inverse of `parent` for level -1 issues.
- **issuelinks**: an array of `IssueLink`, each with **either** `outwardIssue` **or** `inwardIssue`,
  relative to the issue viewed ([links on an issue](#links-on-an-issue)).
- **comment**: a page object `{comments[], maxResults, total, startAt, self}`, which may be truncated;
  [the comment list](#listing-comments) is complete.
  The spec example's array is unrealistic;
  the page object is go-jira's `Comments` and go-atlassian's `IssueCommentPageScheme`,
  and is in go-jira's fixtures.
- **Sprint** (`schema.custom = "com.pyxis.greenhopper.jira:gh-sprint"`, often `customfield_10020`;
  the id is the site's, found in [the field list](#listing-fields)):
  an array of sprint objects, or `null`.
  The attributes are `id, name, state, boardId, goal, startDate, endDate, completeDate`
  (the Sprint toString deprecation notice).
  The list "includes the active/future sprint that the issue is currently in,
  as well as any closed sprints that the issue was in previously" (software intro).
  A future sprint may lack dates; a real one's are UTC `Z` ([timestamps](#timestamps)).
  It is written as a number ([editing an issue](#editing-an-issue)).
- **Story points**: a number or null, in a field whose id and type key vary per site
  ([well-known custom fields](#well-known-custom-fields)).
- **Rank** (`schema.custom = "com.pyxis.greenhopper.jira:gh-lexo-rank"`; pycontribs/jira finds it by this key):
  a LexoRank string, `<bucket>|<base-36 value>:<suffix>`,
  such as `"0|i0003r:"` or the real `"0|0zzzzd:vi"` (a go-jira fixture),
  ordered as plain byte strings.
  The software intro calls Rank an internal field that "shouldn't be read or updated using the REST API";
  it is written only by [ranking](#ranking-issues).
  A board's rank field id is `board/{id}/configuration` → `ranking.rankCustomFieldId`.
- **Epic Link** (`com.pyxis.greenhopper.jira:gh-epic-link`, e.g. `customfield_10014`, a key string
  such as `"AR-37"` in the go-jira and go-atlassian fixtures):
  **deprecated** in favour of `parent`, with Parent Link (`customfield_10018`),
  the agile `epic` field, Epic Name and Epic Color,
  in REST and webhooks since 30 November 2022;
  "There will be no changes to the Jira UI. There will be no changes to JQL."
  (the deprecation announcement).
  **Unconfirmed**: many sites still return Epic Link.
  Assumed: `parent` is the one parent field; the fake emits no Epic Link.

### Keys, ids and moves

- `id` is a numeric string, immutable and unique per site.
  `key` is `<PROJECTKEY>-<n>` and **changes when an issue moves to another project**
  or the project key is renamed.
- A read by an old key finds the issue (spec, verbatim):
  "if the identifier doesn't match an issue, a case-insensitive search and check for moved issues is performed.
  If a matching issue is found its details are returned, a 302 or other redirect is **not** returned.
  The issue key returned in the response is the key of the issue found."
- A moved issue keeps its `id` and gets a new `key` and project.
- **Unconfirmed**: a move shows in the changelog as `Key` and `project` items.
  The fake emits them.
- `project.projectKeys` ([the project](#the-project)) may list a project's past keys.

### Field names and schemas

`expand=names,schema`:

- `names`: `{"customfield_10020": "Sprint", "summary": "Summary", …}`.
- `schema`: `{"customfield_10020": {"type":"array","items":"json","custom":"com.pyxis.greenhopper.jira:gh-sprint","customId":10020}, "summary": {"type":"string","system":"summary"}}`.
  The Sprint `items` is `"json"` (and so in a community capture, qaq-p/1570350;
  pycontribs and jira.js name `gh-sprint`).
- `JsonTypeBean`: `type` (required), `items`, `system`, `custom`, `customId`, `configuration`.

### The changelog on an issue

`expand=changelog` adds `changelog`, a `PageOfChangelogs` `{startAt, maxResults, total, histories[Changelog]}`,
"sorted by date, starting from the most recent".
**Unconfirmed**: it is capped at the 100 most recent entries.
Assumed: so; it is not read for completeness, and the fake caps it at 100.
Its histories come in no guaranteed order ([what the changelog holds](#what-the-changelog-holds)).

## Changelog

### Reading the changelog

`GET /rest/api/3/issue/{issueIdOrKey}/changelog` (`getChangeLogs`).

- Query: `startAt` (default 0), `maxResults` (**default 100**).
- Sorted "by date, starting from the oldest": the opposite of `expand=changelog`.
- 200: `PageBeanChangelog`. 404 if the issue is not found or not visible.

```json
{
  "self": "https://your-domain.atlassian.net/rest/api/3/issue/TT-1/changelog?startAt=2&maxResults=2",
  "nextPage": "https://your-domain.atlassian.net/rest/api/3/issue/TT-1/changelog?&startAt=4&maxResults=2",
  "maxResults": 2, "startAt": 2, "total": 5, "isLast": false,
  "values": [
    {
      "id": "10001",
      "author": {
        "accountId": "5b10a2844c20165700ede21g", "displayName": "Mia Krystof", "active": true,
        "emailAddress": "mia@example.com", "timeZone": "Australia/Sydney", "self": "…", "avatarUrls": {}
      },
      "created": "2026-09-27T10:03:11.123-0700",
      "items": [
        { "field": "status", "fieldtype": "jira", "fieldId": "status",
          "from": "10000", "fromString": "To Do", "to": "3", "toString": "In Progress" },
        { "field": "labels", "fieldtype": "jira", "fieldId": "labels",
          "from": null, "fromString": "label-1", "to": null, "toString": "label-1 label-2" },
        { "field": "Sprint", "fieldtype": "custom", "fieldId": "customfield_10020",
          "from": "36", "fromString": "Sprint 3", "to": "36, 37", "toString": "Sprint 3, Sprint 4" }
      ],
      "historyMetadata": { "type": "…", "activityDescription": "…" }
    }
  ]
}
```

The shape is the spec's; the item values are illustrative
(the spec's example items are unrealistic: `"field":"fields","fieldId":"fieldId"`).
`Changelog` is `id, author (UserDetails), created (date-time), items[ChangeDetails], historyMetadata`;
`ChangeDetails` is `field` (the field's **name**), `fieldId`, `fieldtype`, `from`, `fromString`, `to`, `toString`,
all strings and all nullable.
The spec adds: "Changelogs related to workflow associations are currently being deprecated."

### What the changelog holds

- `fieldtype` is `"jira"` for a system field and `"custom"` for a custom one.
- `from` and `to` are raw ids (a status id, an accountId, comma-joined sprint ids, an option id);
  `fromString` and `toString` are display values.
- For `labels`, both strings are space-joined **whole sets**, not deltas.
- **`fieldId` is often absent**: always on `IssueParentAssociation`,
  and on many custom items, such as `{"field":"Epic Child","fieldtype":"custom","from":null,"fromString":null,"to":"10048","toString":"SP-41"}`
  (blog.mikebowler.ca, 2024-04-09).
  `field` is sometimes a display name (`"status"`, `"Sprint"`, `"Rank"`, `"Link"`, `"Parent"`),
  and its case is not consistent (same source).
  An item is identified by `fieldId` when present, else by `field` compared case-insensitively.
- **Histories come in no guaranteed order**:
  "You can NOT assume that the items in `histories` will be returned in any particular order"
  (same source).
  They sort by `(created, numeric id)`.
  The fake shuffles them unless `WithOrderedChangelog`.
- A Sprint item: `{"field":"Sprint","fieldtype":"custom","fieldId":"customfield_10020","from":"76, 79","fromString":"Sprint 12, Sprint 13","to":"76, 80","toString":"Sprint 12, Sprint 14"}`;
  the ids are **comma-space** separated, and a sprint name may contain a comma,
  so `from` and `to` are what parse (blog.mikebowler.ca, 2026-01-29).
- **The parent**: the `field` values "Epic Link" and "Parent" are consolidated into **`IssueParentAssociation`**
  (the issue-reparenting changelog notice; the old values came alongside until 10 December 2021):
  `{"field":"IssueParentAssociation","fieldtype":"jira","from":"1234","fromString":"ABC-1","to":"4567","toString":"ABC-2"}`,
  `from`/`to` the **parent issue ids** and the strings their **keys**, with **no `fieldId`**:
  "Consumers should not use `fieldId` for the IssueParentAssociation changelog items"
  (Atlassian, community.developer.atlassian.com thread 48993).
- **Unconfirmed**: what is in the changelog and what is not, beyond the above:
  in are field edits, transitions (`status`, and `resolution` when set), assignee, sprint,
  rank (`Rank`, "Ranked higher/lower"), link adds and removes (`field:"Link"`, `toString:"This issue blocks PROJ-20"`, no `fieldId`),
  attachments, time tracking, the parent, and a move (`Key`, `project`, `Workflow`);
  out are **comment creates, edits and deletes**, watchers, votes, issue properties and remote links.
  Assumed: comments are never looked for in the changelog,
  and a `Link` or `Rank` item says only that the field changed.
  The fake emits Link and Rank items without `fieldId`.
- **Unconfirmed**: for `description`, the strings hold wiki or plain text, not ADF.
- **Unconfirmed**: changelog ids are numeric strings that grow site-wide.
  Assumed: undocumented, so never a watermark.

### Bulk changelog

`POST /rest/api/3/changelog/bulkfetch` (`getBulkChangelogs`), scope `read:jira-work`:
"Returns a paginated list of all changelogs for given issues sorted by changelog date and issue IDs,
starting from the oldest changelog and smallest issue ID … up to 1000 issues and can filter them by up to 10 field IDs."

```json
{ "issueIdsOrKeys": ["PROJ-12", "10043"], "fieldIds": ["status", "customfield_10020"], "maxResults": 1000, "nextPageToken": null }
```

200 (`BulkChangelogResponseBean`):

```json
{
  "issueChangeLogs": [
    { "issueId": "10100",
      "changeHistories": [
        { "id": "10001", "author": {}, "created": 1492070429,
          "items": [ {"field": "summary", "fieldId": "summary", "fieldtype": "jira", "fromString": "old summary", "toString": "new summary"} ] }
      ] }
  ],
  "nextPageToken": "UxAQBFRF"
}
```

- `nextPageToken` is null on the last page; there is no `isLast`.
- 400 with no ids or more than 1000.
- **Unconfirmed**: the form of `created`: the example's is an epoch number, the schema says `date-time`.
  Assumed: either; a number is epoch seconds, or milliseconds above 1e11.
- **Unconfirmed**: the default and maximum `maxResults`.
  Assumed: at most 1000, paged on the token.

## Writing issues

### Creating an issue

`POST /rest/api/3/issue` (`createIssue`), scope `write:jira-work`.

- Query: `updateHistory` (default false).
- Body: `IssueUpdateDetails` = `{fields?, update?, transition?, properties?[{key,value}], historyMetadata?}`;
  `fields` and `update` may not name the same field.
- The fields that may be set are the create screen's ([create metadata](#create-metadata)).

```json
{
  "fields": {
    "project":   {"key": "PROJ"},
    "issuetype": {"id": "10001"},
    "summary":   "Checkout fails for guest users",
    "description": {"type": "doc", "version": 1, "content": [
      {"type": "paragraph", "content": [ {"type": "text", "text": "Steps…"} ]} ]},
    "priority":  {"id": "3"},
    "assignee":  {"id": "5b10a2844c20165700ede21g"},
    "labels":    ["checkout"],
    "duedate":   "2026-10-15",
    "parent":    {"key": "PROJ-3"},
    "customfield_10020": 37,
    "customfield_10016": 3
  },
  "properties": [ {"key": "git-work", "value": {"id": "…"}} ]
}
```

- A user field takes `{"id": "<accountId>"}`, as the spec's examples do.
  **Unconfirmed**: `{"accountId": "…"}` is accepted too.
  Assumed: both work; `{"accountId"}` is the form sent, and the fake accepts both.
- The project is `{"id"}` or `{"key"}`; the issue type is `{"id"}`.
  **Unconfirmed**: the issue type as `{"name"}`.
  Assumed: not accepted; the fake takes `{"id"}` only.
- Sprint takes the **numeric sprint id**, not an array (software intro: `"customfield_10021": 2`).
- A sub-task needs a sub-task issue type and `parent`.
  "In a next-gen project any issue may be made a child providing that the parent and child are members of the same project."

201:

```json
{"id":"10000","key":"ED-24","self":"https://your-domain.atlassian.net/rest/api/3/issue/10000",
 "transition":{"status":200,"errorCollection":{"errorMessages":[],"errors":{}}}}
```

`transition` and `watchers` appear only when requested.

Errors:

- 400 `ErrorCollection`, for missing required fields, invalid values,
  fields not settable for the issue type, no permission,
  or a sub-task in another project than its parent.
  The spec's example is `{"errorMessages":["Field 'priority' is required"],"errors":{}}`.
  **Unconfirmed**: the wording of field errors, such as
  `{"errors":{"summary":"You must specify a summary of the issue."}}` and, for a field off the create screen,
  `"Field 'customfield_10016' cannot be set. It is not on the appropriate screen, or unknown."`.
- 401. 403 without permission. 422 when "a configuration problem prevents the creation".

### Bulk create

`POST /rest/api/3/issue/bulk` (`createIssues`): "Creates upto **50** issues".

- Body: `{"issueUpdates": [IssueUpdateDetails, …]}`.
- 201 when any succeed: `{"issues":[{"id","key","self"}…],"errors":[BulkOperationErrorResult]}`.
- 400 when all fail:

```json
{"issues":[],"errors":[{"elementErrors":{"errorMessages":[],"errors":{"issuetype":"The issue type selected is invalid.","project":"Sub-tasks must be created in the same project as the parent."}},"failedElementNumber":0,"status":400}]}
```

`failedElementNumber` is the 0-based index into `issueUpdates`.

### Editing an issue

`PUT /rest/api/3/issue/{issueIdOrKey}` (`editIssue`).

Query parameters:

- `notifyUsers` (default **true**).
  Turning notifications off needs admin or project admin;
  the spec says "If the user doesn't have the necessary permission the request is ignored",
  but the **whole edit is refused** with
  `{"errorMessages":["To discard the user notification either admin or project admin permissions are required."],"errors":{}}`
  (the UiPath forum, thread 543771; k15t's Backbone docs; a community thread, qaq-p/2928374).
  **Unconfirmed**: the status: 400, or 403.
  The fake answers 400 to a caller who is not a project admin.
- `overrideScreenSecurity`, `overrideEditableFlag`: Connect and Forge admin apps only; others get 403.
- `returnIssue` (default false): when true, 200 with the issue.
- `expand`, with `returnIssue`.

Body: `IssueUpdateDetails`, in two forms that may not name the same field:

- `fields`: `{"summary":"x"}` sets.
- `update`: `{"<field>": [ {"set"|"add"|"remove"|"edit"|"copy": value}, … ]}`;
  the operations a field allows are its edit metadata's `operations`.

```json
{
  "fields": {
    "summary": "Completed orders still displaying in pending",
    "customfield_10016": 5
  },
  "update": {
    "labels":      [ {"add": "triaged"}, {"remove": "blocker"} ],
    "parent":      [ {"set": {"none": true}} ]
  },
  "properties": [ {"key": "git-work", "value": {"id": "…"}} ]
}
```

- "issue transition is not supported and is ignored here": status changes by a [transition](#making-a-transition).
- "The parent field may be set by key or ID. For standard issue types,
  the parent may be removed by setting `update.parent.set.none` to *true*."
  By key: `"fields":{"parent":{"key":"PROJ-3"}}`.
- "**Note:** This endpoint doesn't check screen configurations to determine if a field is editable."
  Create and transitions still do.
- `description`, `environment` and textarea custom fields take ADF;
  a **string** sent to one is a 400
  `{"errors":{"description":"Operation value must be an Atlassian Document (see the Atlassian Document Format)"}}`
  (community threads qaq-p/1304733, qaq-p/1977160).
  Single-line text fields take a string.
- Sprint is written as **one number**, `"customfield_10020": 37`, which moves the issue into sprint 37;
  an array is refused with an error asking for a number (community qaq-p/1570350, the software intro).
  An issue is in at most one active or future sprint.
- **Unconfirmed**: `null` clears a field (`duedate`, `assignee`, a number custom field),
  and `null` on Sprint takes the issue out of its active or future sprint, closed ones staying.
  Assumed: so; the fake accepts `null` for all.
- One PUT is one per-issue write ([rate limits](#rate-limits)).

Responses:

- 204 on success, or 200 with the `IssueBean` when `returnIssue=true`.
- 400: no body, no permission to edit a field, an unknown field or one not on the project and issue type,
  an invalid value; field errors are under `errors`, by field id.
- 401. 403 (override flags). 404 (not found or not visible).
- **409 "if the issue could not be updated due to a conflicting update."**
- 422 on a configuration problem.

There is **no optimistic concurrency**: no ETag or If-Match is documented,
and the last writer wins per field.

### Changing an issue type by edit

- `PUT /issue/{id}` with `{"fields":{"issuetype":{"id":"…"}}}` changes an issue's type in place;
  a refused change is a 400 with the reason under `errors.issuetype`
  (spec: 400 on a field not settable for the issue).
- **Unconfirmed**: the edit is accepted only between issue types of one hierarchy level
  that share one workflow, and any other change needs a move, which the REST API does not offer.
  Source: inferred from Atlassian's help on changing an issue's type in the issue view;
  no live site has confirmed it for `PUT`.
  The fake refuses on exactly that condition.
- **Unconfirmed**: the set of status ids [`/project/{key}/statuses`](#statuses-per-issue-type)
  lists for an issue type identifies its workflow.
  A non-admin cannot read workflows ([workflows](#workflows)),
  so the status set is the only stand-in;
  two workflows may share one status set,
  and then a change the set allows can still be refused.
- A required field the new type has and the issue does not set may also refuse the change
  (spec: 400 on missing required fields); whether it does depends on the issue, not the type.
- What the sync does about these:
  [the Jira sync, "Retype targets follow Jira's levels and workflows"](../jira.md#retype-targets-follow-jiras-levels-and-workflows).

### Deleting an issue

`DELETE /rest/api/3/issue/{issueIdOrKey}` (`deleteIssue`).

- Query: `deleteSubtasks` (`"true"|"false"`, default false).
- 204. 400 "if the issue has subtasks and `deleteSubtasks` is not set to *true*". 401.
  403 without *Delete issues*. 404.
- **A deleted issue vanishes**: a read is a 404, search leaves it out,
  and neither search nor the changelog records the delete.
  The fake drops a deleted issue from search.
- Webhooks have `jira:issue_deleted`.

### Issue properties

An issue entity property is a JSON value on an issue under a key.

- `PUT /rest/api/3/issue/{issueIdOrKey}/properties/{propertyKey}` (`setIssueProperty`)
  takes the raw JSON value as its body, a "valid, non-empty JSON blob. The maximum length is 32768 characters".
  **201 when created, 200 when updated**; 400, 401, 403 (no *Edit issues*), 404.
- `GET …/properties/{propertyKey}` (`getIssueProperty`): 200 `{"key":"…","value":{…}}`, or 404.
  `DELETE` answers 204.
- Inline on create or edit, `"properties":[{"key","value"}]` (`IssueUpdateDetails.properties`);
  read in bulk with `properties=<key>` on an issue read or on search (at most 5 keys).
- A property is JQL-searchable only when an app declares it indexed.
- **Unconfirmed**: a property write bumps neither `updated` nor the changelog.
  Assumed: so; the fake bumps neither.

## Transitions

### Listing transitions

`GET /rest/api/3/issue/{issueIdOrKey}/transitions` (`getTransitions`).

Query parameters:

- `expand=transitions.fields`: each transition's screen fields; "Fields hidden from the screen are not returned".
- `transitionId`.
- `skipRemoteOnlyCondition` (admin apps).
- `includeUnavailableTransitions` (default false): "Whether details of transitions that fail a condition are included".
- `sortByOpsBarAndStatus` (default false).

"if a request is made for a transition that does not exist or cannot be performed on the issue,
given its status, the response will return any empty transitions list."
Without *Transition issues* the list is empty, not a 403.

200 (`Transitions`):

```json
{
  "expand": "transitions",
  "transitions": [
    {
      "id": "31", "name": "Done",
      "to": {
        "self": "…/rest/api/3/status/10002", "description": "", "iconUrl": "…",
        "name": "Done", "id": "10002",
        "statusCategory": {"self": "…", "id": 3, "key": "done", "colorName": "green", "name": "Done"}
      },
      "hasScreen": true, "isGlobal": true, "isInitial": false, "isAvailable": true,
      "isConditional": false, "isLooped": false,
      "fields": {
        "resolution": {
          "required": true,
          "schema": {"type": "resolution", "system": "resolution"},
          "name": "Resolution", "key": "resolution", "operations": ["set"],
          "allowedValues": [ {"self": "…", "id": "10000", "name": "Done"} ],
          "hasDefaultValue": false
        }
      }
    }
  ]
}
```

- `IssueTransition`: `id, name, to (StatusDetails), hasScreen, isGlobal, isInitial, isAvailable, isConditional, looped, fields, expand`.
  The schema's `looped` is `isLooped` on the wire (go-atlassian models `json:"isLooped"`).
- `fields` is present only with the expand.
  `FieldMetadata`: `required, schema, name, key, operations, allowedValues, autoCompleteUrl, hasDefaultValue, defaultValue, configuration`.

### Making a transition

`POST /rest/api/3/issue/{issueIdOrKey}/transitions` (`doTransition`).
Body: `IssueUpdateDetails`, with `transition.id` required.

```json
{
  "transition": {"id": "31"},
  "fields": {"resolution": {"name": "Done"}}
}
```

- 204 on success.
- 400: no transition given, no permission, a field in `fields`/`update` not on the transition's screen,
  a field in both, or otherwise invalid.
  A **transition not available from the current status** is
  `{"errorMessages":["Transition id '31' is not valid for this issue."],"errors":{}}`
  (several community reports, e.g. qaq-p/2158194);
  a required screen field left unset is a 400 with `errors` under its id,
  such as `{"resolution":"…"}`.
- **409** for **simultaneous transitions** on one issue:
  "The Jira Cloud API does not support simultaneous issue transitions on an issue …
  we will soon be replacing the 400 (Bad Request) status code with 409 (Conflict)"
  (the change notice on simultaneous transitions, effective from May 2020).
  The status may already have moved.
- 401. 404. 413 (a per-issue limit on comments, worklogs, attachments or links). 422 (configuration).

Only a transition changes the status,
and only one whose `to` is the target and that is available from the current status.

## Comments

### Listing comments

`GET /rest/api/3/issue/{issueIdOrKey}/comment` (`getComments`).

- Query: `startAt` (default 0), `maxResults` (default **100**),
  `orderBy` (`created`, `-created`, `+created`; anything else is a 400), `expand=renderedBody`.
- 200 `PageOfComments` `{startAt, maxResults, total, comments[]}`, with no `isLast`.
  404 if the issue is not found or not visible.
- Only the comments the caller may see are returned.

```json
{
  "self": "https://your-domain.atlassian.net/rest/api/3/issue/10010/comment/10000",
  "id": "10000",
  "author": {"accountId": "5b10…", "displayName": "Mia Krystof", "active": true, "self": "…"},
  "body": {"type": "doc", "version": 1, "content": [
    {"type": "paragraph", "content": [ {"type": "text", "text": "Lorem ipsum…"} ]} ]},
  "updateAuthor": {"accountId": "5b10…", "displayName": "Mia Krystof", "active": true},
  "created": "2021-01-17T12:34:00.000-0800",
  "updated": "2021-01-18T23:45:00.000-0800",
  "visibility": {"type": "role", "value": "Administrators", "identifier": "Administrators"},
  "jsdPublic": true,
  "renderedBody": "<p>Lorem ipsum…</p>",
  "properties": []
}
```

- `visibility` is optional: `type` is `group` or `role`, `value` the name,
  `identifier` the group id or role name
  ("the name of a group is mutable, to reliably identify a group use `identifier`").
- `jsdPublic` "Defaults to true when comments are created in the Jira Cloud Platform";
  `jsdAuthorCanSeeRequest` is JSM's.
- `renderedBody` is present only with `expand=renderedBody`.
- A comment's `updated` differs from its `created` once it is edited.
  Comments are not in the changelog.
- **Unconfirmed**: which comment writes bump the issue's `updated`.
  Jira's `CommentManager.create(…, modifyIssueUpdateDate)` exists, so an add very likely does;
  edits and deletes are unknown.
  Assumed: an add bumps it, an edit and a delete do not (the harder case); the fake does the same.
- **Unconfirmed**: `expand=properties` is honoured on the comment list.
  The fake honours it by default, and drops it with `WithCommentProperties(false)`.
- **Unconfirmed**: threaded comments carry a `parentId`
  (the spec's updateComment text mentions "Child comments inherit visibility from their parent comment").
  The fake emits none.

### Adding, editing and deleting comments

| op | method + path | operationId | body | success and errors |
|---|---|---|---|---|
| add | `POST /rest/api/3/issue/{key}/comment` | `addComment` | `{"body": ADF, "visibility"?: {...}, "properties"?: [...]}` | **201** with the Comment. 400, 401, 404, **413** (per-issue comment limit) |
| get | `GET /rest/api/3/issue/{key}/comment/{id}` | `getComment` | — | 200 Comment. 404 |
| update | `PUT /rest/api/3/issue/{key}/comment/{id}` | `updateComment` | as add; query `notifyUsers` (default true), `overrideEditableFlag`, `expand` | **200** with the Comment. 400 (no permission, or invalid, such as changing a child comment's visibility), 401, 404 |
| delete | `DELETE /rest/api/3/issue/{key}/comment/{id}` | `deleteComment` | — | **204**. 400 (no permission), 401, 404, 405 (anonymous) |

No permission to delete or update is a **400**, not a 403.

`POST /rest/api/3/comment/list` (`getCommentsByIds`) reads comments by id:
the body `{"ids":[int64…]}`, **at most 1000**, and an optional `expand` query;
200 is a `PageBeanComment` `{self,nextPage,maxResults,startAt,total,isLast,values[Comment]}`, else 400.

### Deleted comments leave nothing

A deleted comment vanishes from the list,
and nothing, in the changelog or elsewhere, records the delete.

### ADF

The Atlassian Document Format,
from `https://developer.atlassian.com/cloud/jira/platform/apis/document/structure/` and its node and mark pages.

- **Root**: `{"version":1,"type":"doc","content":[…]}`; the empty document is `{"version":1,"type":"doc","content":[]}`.
- **Top-level block nodes**: blockquote, bulletList, codeBlock, expand, heading, mediaGroup, mediaSingle,
  orderedList, panel, paragraph, rule, table, multiBodiedExtension, bodiedSyncBlock, syncBlock.
- **Child blocks**: listItem, media, nestedExpand, tableCell, tableHeader, tableRow, blockTaskItem, extensionFrame.
- **Inline nodes**: text, hardBreak, mention, emoji, date, inlineCard, status, mediaInline.
- **Marks**: strong, em, code, link, strike, underline, subsup, textColor, border.

```json
{"type":"paragraph","content":[
  {"type":"text","text":"Hello "},
  {"type":"text","text":"bold","marks":[{"type":"strong"}]},
  {"type":"text","text":" and "},
  {"type":"text","text":"italic","marks":[{"type":"em"}]},
  {"type":"hardBreak"},
  {"type":"text","text":"x := 1","marks":[{"type":"code"}]},
  {"type":"text","text":" see ","marks":[]},
  {"type":"text","text":"docs","marks":[{"type":"link","attrs":{"href":"https://example.com","title":"Docs"}}]}
]}
```

```json
{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Heading"}]}
```

```json
{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"func main() {}\nfmt.Println()"}]}
```

```json
{"type":"bulletList","content":[
  {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]}]},
  {"type":"listItem","content":[
    {"type":"paragraph","content":[{"type":"text","text":"two"}]},
    {"type":"bulletList","content":[
      {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"nested"}]}]}]}]}
]}
```

```json
{"type":"orderedList","attrs":{"order":1},"content":[ {"type":"listItem","content":[…]} ]}
```

```json
{"type":"blockquote","content":[{"type":"paragraph","content":[…]}]}
{"type":"rule"}
{"type":"mention","attrs":{"id":"<accountId>","text":"@Mia Krystof","accessLevel":""}}
```

- `heading.attrs.level` is 1–6 (required); its content is inline nodes.
- `codeBlock.attrs.language` is optional, as are `wrap` and `hideLineNumbers`;
  its content is "one or more text nodes **without marks**", newlines inside `text`.
- The `code` mark "can **ONLY** be combined with … `link`".
- The `link` mark: `attrs.href` is required; `title`, `id`, `collection` and `occurrenceKey` are optional.
- `listItem` content is at least one of paragraph (no marks), bulletList, orderedList, codeBlock (no marks)
  or mediaSingle; the pages do not say the first child must be a paragraph.
- `orderedList.attrs.order` is an int ≥ 0; the default start is 1.
- `mention.attrs`: `id` (required, the accountId), `text` (with the leading @),
  `accessLevel` (NONE, SITE, APPLICATION or CONTAINER), `userType` (DEFAULT, SPECIAL or APP).
- A plain `doc > paragraph > text` is accepted: it is the spec's own example body.
- **Unconfirmed**: a text node with an empty `text` is invalid,
  as is a paragraph ending in a hardBreak,
  and malformed ADF is a 400 `"INVALID_INPUT"`.

### Jira normalises ADF

What Jira returns is the stored ADF, which may differ byte-wise from what was written:
added `attrs` such as `localId`, merged text nodes, a normalised structure
(community threads qaq-p/1304733, qaq-p/1977160).
Two bodies are the same when their canonical forms are.
The fake adds `localId` attrs and merges text nodes the same way.

## Schema discovery

### Listing fields

`GET /rest/api/3/field` (`getFields`): an unpaged array of `FieldDetails`.
A field in no used configuration or screen is not returned,
and others only if they are used in a project the caller can browse.

```json
[
  {"id": "summary", "key": "summary", "name": "Summary", "custom": false, "orderable": true, "navigable": true,
   "searchable": true, "clauseNames": ["summary"], "schema": {"type": "string", "system": "summary"}},
  {"id": "customfield_10020", "key": "customfield_10020", "name": "Sprint", "untranslatedName": "Sprint",
   "custom": true, "orderable": true, "navigable": true, "searchable": true,
   "clauseNames": ["cf[10020]", "Sprint"],
   "schema": {"type": "array", "items": "json", "custom": "com.pyxis.greenhopper.jira:gh-sprint", "customId": 10020}}
]
```

- `FieldDetails`: `id, key, name, custom, orderable, navigable, searchable, clauseNames[], schema{type, items, system, custom, customId, configuration}, scope?`.
- `untranslatedName` is not in the schema but is seen on custom fields; it is optional.
- `scope` is present on a team-managed custom field, `{"type":"PROJECT","project":{"id":"10000"}}`:
  one name can exist several times, told apart by scope.
- Custom field ids (`customfield_NNNNN`) differ per site;
  a field's name is localised and editable, its `schema.custom` is not.
- `GET /rest/api/3/field/search` (`getFieldsPaginated`) is the paged, filterable variant
  (`startAt`, `maxResults` default 50, `type=custom|system`, `id[]`, `query`, `orderBy`, `expand`, `projectIds[]`).
  **Unconfirmed**: it is admin-only.

### Well-known custom fields

| field | `schema.custom` | type | source |
|---|---|---|---|
| Sprint | `com.pyxis.greenhopper.jira:gh-sprint` | array | the software intro |
| Rank | `com.pyxis.greenhopper.jira:gh-lexo-rank` | string | pycontribs/jira |
| Epic Link | `com.pyxis.greenhopper.jira:gh-epic-link` | string, deprecated | the go-jira and go-atlassian fixtures |
| Story point estimate | `com.pyxis.greenhopper.jira:jsw-story-points` **or** `com.atlassian.jira.plugin.system.customfieldtypes:float` | number | both seen (see below) |
| Story Points (classic) | `com.atlassian.jira.plugin.system.customfieldtypes:float`, named "Story Points" | number | by name only |
| Start date | `com.atlassian.jira.plugin.system.customfieldtypes:datepicker`, named "Start date" | date | **Unconfirmed** |

The story-points field has no reliable identity on Cloud
(JSWCLOUD-26668: no reliable way exists to identify the Story Points field;
both type keys are seen with the name "Story point estimate" or "Story Points").
The one a board uses is its configuration's `estimation.field.fieldId`
(`GET /rest/agile/1.0/board/{id}/configuration`; the spec's example is `"customfield_10002"`, "Story Points");
the software intro: "the field is just a regular numeric field.
The type of estimation and field used for estimation is determined by the board configuration."
Without a board, a field with `schema.custom` `jsw-story-points` is the likeliest,
then a `float` field whose `untranslatedName`, else `name`, is exactly "Story Points" or "Story point estimate".
The fake's sites give their story-points field the `float` key.

### The project

`GET /rest/api/3/project/{projectIdOrKey}` (`getProject`).

- `expand`: `description`, `issueTypes`, `lead`, `projectKeys`, `issueTypeHierarchy`;
  "description, issue types, and project lead are included in all responses by default."
- `properties`: project property keys.
- 200 `Project`. 401. 404 (not found or no permission). The key is case-sensitive.

```json
{
  "self": "https://your-domain.atlassian.net/rest/api/3/project/EX",
  "id": "10000", "key": "EX", "name": "Example",
  "description": "…", "lead": {"accountId": "…", "displayName": "…"},
  "assigneeType": "PROJECT_LEAD",
  "projectTypeKey": "software",
  "simplified": false,
  "style": "classic",
  "isPrivate": false,
  "archived": false,
  "issueTypes": [
    {"id": "3", "name": "Task", "subtask": false, "hierarchyLevel": 0, "avatarId": 1, "iconUrl": "…", "self": "…"},
    {"id": "10000", "name": "Epic", "subtask": false, "hierarchyLevel": 1},
    {"id": "10003", "name": "Sub-task", "subtask": true, "hierarchyLevel": -1}
  ],
  "versions": [], "components": [], "roles": {"Developers": "…/project/EX/role/10000"},
  "insight": {"lastIssueUpdateTime": "2021-04-22T05:37:05.000+0000", "totalIssueCount": 100},
  "uuid": "…"
}
```

- `style` is `classic` (company-managed) or `next-gen` (team-managed).
  A team-managed project has project-scoped issue types (`scope.type=PROJECT`), custom fields and statuses.
- `projectTypeKey` is `software`, `service_desk`, `business` or `product_discovery`.
- **Unconfirmed**: `simplified` is true for a team-managed project; the spec says only "Whether the project is simplified".
- **Unconfirmed**: with `expand=projectKeys`, a `projectKeys` array lists every key the project has had.
  The expand option exists ("All project keys associated with the project"),
  but the `Project` schema has no such property.
  Assumed: optional, and nothing depends on it.
- **Unconfirmed**: how fresh `insight.lastIssueUpdateTime` is.

### Statuses per issue type

`GET /rest/api/3/project/{projectIdOrKey}/statuses` (`getAllStatuses`):
the valid statuses of the project, grouped by issue type.

```json
[ {"self": "…/issueType/3", "id": "3", "name": "Task", "subtask": false,
   "statuses": [ {"self": "…", "description": "", "iconUrl": "…", "name": "In Progress", "id": "10000",
                  "statusCategory": {"id": 4, "key": "indeterminate", "colorName": "yellow", "name": "In Progress", "self": "…"}} ]} ]
```

The spec's example omits `statusCategory`, but the schema is `StatusDetails`, which has it.
Whether a status set identifies a workflow is
[unconfirmed](#changing-an-issue-type-by-edit).

### Issue types and hierarchy levels

`GET /rest/api/3/issuetype/project?projectId=<int64>` (`getIssueTypesForProject`).

- `projectId` is required and numeric; `level` filters: "`-1` for Subtask. `0` for Base. `1` for Epic."
- 200: an array of `IssueTypeDetails`. 400. 404 (no such project, or no permission).
- `GET /rest/api/3/project/{projectId}/hierarchy` (`getHierarchy`, "for a next-gen project"),
  with ints for ids:

```json
{"projectId": 10030, "hierarchy": [
  {"level": 0, "name": "Base", "issueTypes": [ {"id": 10008, "entityId": "…", "name": "Story", "avatarId": 10324} ]},
  {"level": 1, "name": "Epic", "issueTypes": [ {"id": 10007, "name": "Epic"} ]},
  {"level": -1, "name": "Subtask", "issueTypes": [ {"id": 10009, "name": "Subtask"} ]}]}
```

`hierarchyLevel`:

- `-1` is a sub-task, whose parent is at level 0.
- `0` is the base level (Story, Task, Bug), whose parent is an epic.
- `1` is Epic.
- **Unconfirmed**: `≥2` are levels above Epic (such as Initiative),
  configurable only with Premium or Enterprise (Plans), which enforce them;
  the spec documents only -1, 0 and 1.
- An issue's parent is one level up;
  sub-task under level 0 is the rule every plan enforces.

### Priorities

- `GET /rest/api/3/priority` (`getPriorities`) is **deprecated** ("Use Search priorities instead"):
  an unpaged array of `{self, statusColor, description, iconUrl, name, id}`.
- `GET /rest/api/3/priority/search` (`searchPriorities`):
  `startAt` and `maxResults` are **strings** (defaults "0" and "50"), with `id[]`, `projectId[]`, `priorityName`,
  `onlyDefault` (deprecated) and `expand=schemes`;
  a page bean of `Priority` `{id, name, description, iconUrl, statusColor, isDefault (deprecated), avatarId, self, schemes?}`.
  Its OAuth scope is `manage:jira-configuration`.
  **Unconfirmed**: Basic auth works for an ordinary user.
- Priorities can be per project (priority schemes); `projectId=` gives the project's.

### Status categories

`GET /rest/api/3/statuscategory` (`getStatusCategories`): an array of `{id: int, key, name, colorName, self}`;
`GET /rest/api/3/statuscategory/{idOrKey}` reads one.

| id | key | name | colorName |
|---|---|---|---|
| 1 | `undefined` | No Category | `medium-gray` |
| 2 | `new` | To Do | `blue-gray` |
| 3 | `done` | Done | `green` |
| 4 | `indeterminate` | In Progress | `yellow` |

Source: a capture from a real site in go-jira's fixtures (`all_statuscategories.json`),
and the `StatusCategory` javadoc.
The spec's examples, with keys `in-flight` and `completed`, are unrealistic.

A category is an object `{id, key, …}` on issues, transitions and `/status`,
and an enum string `TODO`, `IN_PROGRESS` or `DONE` on `/statuses*` and `/workflows/search`.
**Unconfirmed**: `new` is `TODO`, `indeterminate` `IN_PROGRESS`, and `done` `DONE`.

### Statuses

- `GET /rest/api/3/status` (`getStatuses`): "all statuses associated with active workflows",
  an array of `StatusDetails` with the category object; needs *Browse projects*.
- `GET /rest/api/3/statuses?id=…&id=…` (`getStatusesById`): a `JiraStatus` array, the category an enum string.
- `GET /rest/api/3/statuses/search` (`search`):
  `projectId`, `startAt`, `maxResults` (default 200), `searchString`, `statusCategory`, `includeGlobalStatuses`;
  a page bean of `JiraStatus` `{id, name, description, scope{type:"PROJECT"|"GLOBAL", project{id}}, statusCategory}`.
  **Needs Administer projects or Administer Jira**; scope `manage:jira-configuration`.

### Create metadata

The paginated endpoints, which list "the same fields that appear on the issue's create screen":

- `GET /rest/api/3/issue/createmeta/{projectIdOrKey}/issuetypes` (`getCreateIssueMetaIssueTypes`):
  `startAt`, `maxResults` (default 50, **at most 200**);
  200 `{"issueTypes":[{id,name,description,iconUrl,self,subtask,avatarId,hierarchyLevel,entityId,scope}], "startAt":0, "maxResults":50, "total":1}`
  (the schema also lists an alias array, `createMetaIssueType`);
  400 `{"errorMessages":["Parameter 'maxResults' must not exceed the limit '200'"],"errors":{},…}`.
- `GET /rest/api/3/issue/createmeta/{projectIdOrKey}/issuetypes/{issueTypeId}` (`getCreateIssueMetaIssueTypeId`):
  the same paging; 200 `{"fields":[FieldCreateMetadata], "startAt","maxResults","total"}`
  (with a `results` alias in the schema):

```json
{"fields":[{"fieldId":"assignee","key":"assignee","name":"Assignee","required":true,"hasDefaultValue":false,
            "operations":["set"],"schema":{"type":"user","system":"assignee"},"autoCompleteUrl":"…"}],
 "maxResults":1,"startAt":0,"total":1}
```

- `FieldCreateMetadata` requires `fieldId, key, name, operations, required, schema`,
  with `allowedValues, autoCompleteUrl, configuration, defaultValue, hasDefaultValue`;
  an option, priority or issue-type field's `allowedValues` are the objects (`{"id":"3","name":"Medium",…}`).
- Needs *Create issues* in the project.
- A field off the create screen is refused at create.

`GET /rest/api/3/issue/createmeta?projectKeys=&issuetypeIds=&expand=projects.issuetypes.fields`
(`getCreateIssueMeta`) is `deprecated: true` (CHANGE-1304).
**Unconfirmed**: deprecated around December 2023 with no removal date announced.

### Edit metadata

`GET /rest/api/3/issue/{issueIdOrKey}/editmeta` (`getEditIssueMeta`).

- Query: `overrideScreenSecurity`, `overrideEditableFlag` (admin apps; others get 403).
- 200: `{"fields": {"<fieldId>": FieldMetadata}}`;
  `operations` is such as `["set"]`, or `["add","set","remove"]` for labels.
- The spec lists nine conditions for a field to appear:
  on a screen; visible in the field configuration; shown on the issue; a valid custom field context;
  project, type and status present; a valid workflow step; the step editable (`jira.issue.editable`);
  *Edit issues*; and the workflow's `jira.permission.*` properties.
- Since [an edit](#editing-an-issue) no longer checks screens,
  edit metadata is narrower than what an edit accepts.

### Link types

`GET /rest/api/3/issueLinkType` (`getIssueLinkTypes`).

```json
{"issueLinkTypes":[
  {"id":"1000","name":"Duplicate","inward":"is duplicated by","outward":"duplicates","self":"…/issueLinkType/1000"},
  {"id":"10000","name":"Blocks","inward":"is blocked by","outward":"blocks","self":"…"}]}
```

- 404 when issue linking is disabled.
- A link type's `inward` and `outward` texts are the site's to edit:
  the spec's example has `"inward":"Blocked by","outward":"Blocks"`.
  **Unconfirmed**: a default site has `"is blocked by"` and `"blocks"`.
  A link type is identified by `id` or `name`.

### Resolutions

- `GET /rest/api/3/resolution` (`getResolutions`) is **deprecated**: an array of `{self,id,description,name}`.
- `GET /rest/api/3/resolution/search` (`searchResolutions`): `startAt` and `maxResults` are strings
  (defaults "0" and "50"), with `id[]` and `onlyDefault`; it returns `PageBeanResolutionJsonBean`.
- A resolution is set by a transition, on its screen; `fields.resolution` is null while unresolved.

### Workflows

- `GET /rest/api/3/workflows/search` (`searchWorkflows`):
  `startAt`, `maxResults`, `expand=values.transitions`, `queryString`, `orderBy` (name, created, updated),
  `scope` (GLOBAL or PROJECT), `isActive`, `projectId`.
  It needs "*Administer Jira* global permission to access all, including project-scoped, workflows",
  or "At least one of the *Administer projects* and *View (read-only) workflow* project permissions
  to access project-scoped workflows"; scope `manage:jira-configuration`.
  An ordinary user cannot read workflows.

```json
{"isLast":false,"maxResults":50,"startAt":0,"total":100,
 "statuses":[{"id":"10001","name":"To Do","statusCategory":"TODO","statusReference":"10001","scope":{"type":"GLOBAL"}}],
 "values":[{"id":"b9ff2384-…","name":"Workflow 1","scope":{"type":"GLOBAL"},"isEditable":true,
   "version":{"id":"f010…","versionNumber":0},
   "statuses":[{"statusReference":"10001","deprecated":false,"layout":{"x":114.9,"y":-16.0},"properties":{}}],
   "transitions":[
     {"id":"1","name":"Create","type":"INITIAL","toStatusReference":"10001","links":[]},
     {"id":"41","name":"Start work","type":"DIRECTED","toStatusReference":"10002",
      "links":[{"fromStatusReference":"10001","fromPort":0,"toPort":1}]},
     {"id":"31","name":"Done","type":"GLOBAL","toStatusReference":"10003","links":[]}]}]}
```

- A transition's `type` is `INITIAL`, `DIRECTED` or `GLOBAL`;
  a `GLOBAL` one is reachable from any status,
  and a `DIRECTED` one from each `links[].fromStatusReference`.
  **Unconfirmed**: a transition's `id` here is the one [made](#making-a-transition) on an issue.
- **Unconfirmed**: which issue type uses which workflow is in workflow schemes
  (`/rest/api/3/workflowscheme/project?projectId=`, admin);
  `projectId=` on the search does not say.
- `GET /rest/api/3/workflow/search` (`getWorkflowsPaginated`) was **deprecated**,
  "This will be removed on June 1, 2026 (CHANGE-2569)", classic workflows and admin only.
  **Unconfirmed**: it is removed, answering 404 or 410.
- Without admin, a workflow shows only through
  [the transitions](#listing-transitions) available on a real issue from its current status.

## Issue links

### Creating a link

`POST /rest/api/3/issueLink` (`linkIssues`).

```json
{
  "type": {"name": "Blocks"},
  "inwardIssue":  {"key": "PROJ-12"},
  "outwardIssue": {"key": "PROJ-20"}
}
```

This body means **PROJ-12 blocks PROJ-20** ([link direction](#link-direction)).

- `type` takes `{name}` or `{id}`; the issues `{id}` or `{key}`.
- **201 with an empty body**:
  "To obtain the ID of the issue link, use `…/issue/[linked issue key]?fields=issuelinks`."
- A duplicate request "indicates that the issue link was created" and makes no second link.
- An optional `comment` is "add[ed] to the from (outward) issue",
  and *Link issues* is needed "on the project containing the from (outward) issue";
  both contradict [the direction](#link-direction), which makes the inward issue the from one.
  Nothing settles which issue a link comment lands on;
  the fake puts it on the `outwardIssue`, as the spec's text says.
- Errors: 400 (the comment failed, and then no link is made either); 401;
  404 (linking disabled, either issue not visible, or no *Link issues*); 413 (the per-issue link limit).

### Link direction

A link type has an `outward` text ("blocks") and an `inward` one ("is blocked by").
A link is `source → destination`, read "source *outward* destination".
In the POST, **`inwardIssue` is the source and `outwardIssue` the destination**:

- `{"type":{"name":"Blocks"},"inwardIssue":{"key":"A"},"outwardIssue":{"key":"B"}}` means **A blocks B**.
- Viewing **A**, `fields.issuelinks` holds `{"outwardIssue": B}`, labelled with `type.outward`: "A blocks B".
- Viewing **B**, it holds `{"inwardIssue": A}`, labelled with `type.inward`: "B is blocked by A".
- In the POST, each issue goes under the key it appears under **when viewed from the other**.

Sources:

- Atlassian's Java client JRJC, `LinkIssuesInputGenerator`, sends `inwardIssue = fromIssueKey`
  and `outwardIssue = toIssueKey` (pre-5.0 servers took `fromIssueKey`/`toIssueKey`).
- The Jira REST docs, 4.4.1 to 7.x: the request "will create a link from the first issue to the second issue
  using the outward description. It also create a link from the second issue to the first issue using the inward description";
  the first issue is the from, inward one.
- A report against Jira Cloud: `outwardIssue: IQS-2, inwardIssue: IQS-4, Blocks`
  showed "IQS-2 is blocked by IQS-4" (atlassian-mcp-server issue 112).
- The issue-linking model page, for the read side:
  "if the issue link data contains an `inwardIssue` field, the link should be labeled with the value of the `type.inward` field".

### Reading and deleting a link

- `GET /rest/api/3/issueLink/{linkId}` (`getIssueLink`): 200 `IssueLink`
  `{id, self, type{id,name,inward,outward,self}, inwardIssue{id,key,self,fields{summary?,status,priority,issuetype}}, outwardIssue{…}}`,
  with both sides.
- `DELETE /rest/api/3/issueLink/{linkId}` (`deleteIssueLink`): the spec lists both **204** and 200.
  400 (an invalid id). 401. 404 (linking disabled, not found, or no permission).
- The link id is the `fields.issuelinks[].id` of both issues.

### Links on an issue

An entry of `fields.issuelinks` is the `IssueLink` without the side viewed:

```json
{"id":"10001","self":"…/issueLink/10001",
 "type":{"id":"10000","name":"Blocks","inward":"is blocked by","outward":"blocks","self":"…"},
 "outwardIssue":{"id":"10060","key":"PROJ-20","self":"…","fields":{"summary":"…","status":{…},"priority":{…},"issuetype":{…}}}}
```

- One link appears on both issues, with opposite sides.
- **Epic, parent and sub-task relations are not issue links**: they are `parent`.
- A deleted link vanishes from both issues, and the changelog's `Link` item is all that records it
  ([what the changelog holds](#what-the-changelog-holds)).

## Users

### Reading a user

`GET /rest/api/3/user?accountId=…` (`getUser`).

- `accountId` is required (at most 128 characters); `username` and `key` are "no longer available".
  `expand` takes `groups` and `applicationRoles`.
- 200 `User`. 401. **403 without the *Browse users and groups* global permission.** 404.

### Searching users

`GET /rest/api/3/user/search?query=…` (`findUsers`).

- `query` matches `displayName` and `emailAddress` by prefix;
  the alternatives are `accountId` (exact) and `property`, and one of the three is required.
  `startAt` and `maxResults` (default 50), "up to the thousandth user".
- 200 is a **bare array** of `User`. 400 (missing parameters, or both `query` and `accountId`). 401.
- **429**: "User search endpoints share a collective rate limit for the tenant … Please respect the Retry-After header."
- Without *Browse users and groups* the result is **empty, not an error**.
- A hidden email is found only by an exact match:
  "If a user has hidden their email address … partial matches of the email address will not find the user.
  An exact match is required." (the older search documentation).

### Bulk users

`GET /rest/api/3/user/bulk?accountId=a&accountId=b` (`bulkGetUsers`).

- `accountId[]` is required and repeated; `startAt`, and `maxResults` (default 10).
- 200: a page bean `{isLast, maxResults, startAt, total, values[User]}`. 400 without an accountId.
- **Unconfirmed**: at most 200 ids a call.

### Privacy and account types

- `accountType` is `atlassian` (a person), `app` (a Connect, Forge or OAuth integration,
  with `appType` `service`, `agent` or `unknown`), `customer` (a JSM portal user) or `unknown`.
- `emailAddress` "may be returned as null" by the user's profile visibility,
  is blank for a deleted user (spec, `User`), and is seen absent;
  absent, `null` and `""` all mean unknown.
  **Unconfirmed**: only `/myself` reliably shows the caller's own email, and even that by privacy settings.
  The fake omits a hidden email.
- `timeZone` and `locale` may be hidden; for `timeZone`, "the instance's default time zone will be returned" instead.
- `displayName` "may return an alternative value" for privacy.
- `active: false` is a deactivated account.
- An account is identified by `accountId`; most emails are hidden.

## Agile

### Agile ids, paging and dates

Base path `/rest/agile/1.0`; documentation at `https://developer.atlassian.com/cloud/jira/software/rest/`.

- Board, sprint and epic ids are **integers** in JSON.
- Paging is `{startAt, maxResults, total, isLast, values}`.
- 403 often means "does not have a valid license" (no Jira Software access).
- Sprint dates are UTC `Z` on real sites; the spec's examples show a colon offset ([timestamps](#timestamps)).

### Boards

`GET /rest/agile/1.0/board` (`getAllBoards`).

- `startAt` (0), `maxResults` (50), `type` (`scrum|kanban|simple`), `name`, `projectKeyOrId`,
  `accountIdLocation`, `projectLocation`, `includePrivate`, `negateLocationFiltering`, `orderBy=name`,
  `expand=admins,permissions`, `projectTypeLocation`, `filterId`.
- `projectKeyOrId` matches boards "relevant to a project … the jql filter defined in board contains a reference to a project";
  a project has any number of boards.

```json
{"isLast":false,"maxResults":2,"startAt":1,"total":5,"values":[
  {"id":84,"name":"scrum board","self":"…/rest/agile/1.0/board/84","type":"scrum"},
  {"id":92,"name":"kanban board","self":"…","type":"kanban"}]}
```

- **Unconfirmed**: an entry also carries `location {projectId, projectKey, projectName, displayName, projectTypeKey, avatarURI}`.
- Errors: 400, 401, 403 (license).
- `GET /rest/agile/1.0/board/{boardId}/configuration` (`getConfiguration`):
  the columns and their status ids, `estimation.field.fieldId` (the story-points field),
  `ranking.rankCustomFieldId`, `filter.id` and `location`.

### Sprints

`GET /rest/agile/1.0/board/{boardId}/sprint` (`getAllSprints`).

- `startAt`, `maxResults`, `state` (a comma list of `future,active,closed`).
- "Sprints will be ordered first by state (i.e. closed, active, future) then by their position in the backlog."

```json
{"isLast":false,"maxResults":2,"startAt":1,"total":5,"values":[
  {"id":37,"self":"…/rest/agile/1.0/sprint/23","state":"closed","name":"sprint 1",
   "startDate":"2015-04-11T15:22:00.000+10:00","endDate":"2015-04-20T01:22:00.000+10:00",
   "completeDate":"2015-04-20T11:04:00.000+10:00","originBoardId":5,"goal":"sprint 1 goal"},
  {"id":72,"self":"…","state":"future","name":"sprint 2","goal":"sprint 2 goal"}]}
```

- A future sprint may lack `startDate` and `endDate`; `completeDate` exists once it is closed;
  there is also a `createdDate` (in the update body's schema).
- Errors: 400, 401, 403, 404 (no such board, or not visible).
  **Unconfirmed**: a kanban board answers 400 `{"errorMessages":["The board does not support sprints"]}`.
- `GET /rest/agile/1.0/sprint/{sprintId}` (`getSprint`) reads one.
- `POST /rest/agile/1.0/sprint/{sprintId}` (`partiallyUpdateSprint`):
  a closed sprint allows only its name and goal;
  `future → active` needs `startDate` and `endDate`;
  `active → closed` sets `completeDate`; no other state change is allowed.
- **Unconfirmed**: `POST /rest/agile/1.0/sprint` creates one from `{name, originBoardId, startDate?, endDate?, goal?}`;
  the path exists.

### Moving issues to a sprint

`POST /rest/agile/1.0/sprint/{sprintId}/issue` (`moveIssuesToSprintAndRank`).

- "Issues can only be moved to open or active sprints. The maximum number of issues that can be moved in one operation is 50."
- Body: `{"issues":["PR-1","10001","PR-3"], "rankBeforeIssue":"PR-4", "rankAfterIssue"?:…, "rankCustomFieldId":10521}`;
  the rank parts are optional.
- 204. 400. 401. 403 (license, or no permission to assign). 404 (sprint).
- "an issue can only be in one active or future sprint at a time, and only the active/future sprint can edited"
  (agile spec, verbatim).
- [An edit](#editing-an-issue) of the Sprint field does the same for one issue.

### Moving issues to the backlog

`POST /rest/agile/1.0/backlog/issue` (`moveIssuesToBacklog`).

- Body: `{"issues":["10001","PR-1"]}`, at most 50:
  "equivalent to remove future and active sprints from a given set of issues."
- 204. 400. 401. 403. 404.
- **Unconfirmed**: the board-scoped `POST /rest/agile/1.0/backlog/{boardId}/issue` takes rank parameters too.

### Ranking issues

`PUT /rest/agile/1.0/issue/rank` (`rankIssues`).

- "Moves (ranks) issues before or after a given issue. At most 50 issues may be ranked at once."
- "If rankCustomFieldId is not defined, the default rank field will be used."

```json
{"issues":["PR-1","10001","PR-3"],"rankBeforeIssue":"PR-4","rankCustomFieldId":10521}
```

- **Unconfirmed**: `rankBeforeIssue` and `rankAfterIssue` together are refused.
  Assumed: exactly one is sent.
- **204** when all succeed, **207** when some fail:

```json
{"entries":[{"issueId":10000,"issueKey":"PR-1","status":200},
            {"issueId":10002,"issueKey":"PR-3","status":503,
             "errors":["JIRA Agile cannot execute the rank operation at this time. Please try again later."]}]}
```

- 400. 401. 403 (license, or no *Schedule issues*: "To rank issues user has to have schedule issue permission").
- There is no absolute position: a rank is always relative to another issue.

### Epics

The agile epic API is deprecated in favour of `parent`:

- `GET` and `POST` (a partial update) `/rest/agile/1.0/epic/{epicIdOrKey}`
  "does not work for epics in next-gen projects".
- `GET /rest/agile/1.0/epic/{epicIdOrKey}/issue` (`getIssuesForEpic`) is `deprecated: true`;
  its text points to JQL (`parent = EPIC-1`).
- `POST /rest/agile/1.0/epic/{epicIdOrKey}/issue` (`moveIssuesToEpic`, at most 50) and `/epic/none/issue`
  still exist, for classic projects.
- The agile `GET /rest/agile/1.0/issue/{key}` adds `sprint` (the current one), `closedSprints[]`, `flagged`
  and the deprecated `epic`
  (`"epic":{"id":37,"name":"epic 1","summary":"…","color":{"key":"color_4"},"done":true}`).
- **Unconfirmed**: `GET` and `PUT /rest/agile/1.0/issue/{key}/estimation?boardId=` read and write
  the board's estimation value as `{"value":"8.0"}`.

## Errors and limits

### Rate limits

Source: the rate-limiting page.

- The status is **429 Too Many Requests**. "Some transient 5xx responses may also include `Retry-After`".

| header | format | meaning |
|---|---|---|
| `Retry-After` | integer seconds | how long to wait |
| `X-RateLimit-Limit` | int | the maximum rate for the current scope |
| `X-RateLimit-Remaining` | int | the capacity left in the window |
| `X-RateLimit-Reset` | ISO-8601 timestamp | when the window resets (only on 429) |
| `X-RateLimit-NearLimit` | `true`/`false` | true when under 20% of capacity remains |
| `RateLimit-Reason` | enum | `jira-quota-global-based`, `jira-quota-tenant-based`, `jira-burst-based`, `jira-per-issue-on-write` |
| `Beta-RateLimit-Policy`, `Beta-RateLimit` | `"<policy>";q=…;w=…;r=…;t=…` | informational |

Legacy informational `X-Beta-RateLimit-*` headers also exist.
The page's burst example:

```
HTTP/1.1 429 Too Many Requests
Retry-After: 1
X-RateLimit-Limit: 350
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 2026-01-01T01:01:01Z
RateLimit-Reason: jira-burst-based
Content-Type: application/json
```

- Points quotas are hourly and apply to OAuth and Forge apps;
  "API token-based traffic is not affected … governed by existing burst rate limits".
- Burst limits: GET 100 a second, POST 100, PUT 50, DELETE 50.
- **Per-issue write limits: 20 writes per 2 s and 100 per 30 s** (`jira-per-issue-on-write`).
- The recommended backoff: `Retry-After` when given,
  else exponential from a 2 s base, capped at 30 s, with jitter ×0.7–1.3, and at most 4 retries.
- A rate-limited request is rejected before it is processed.
- **Unconfirmed**: the 429 body (an `ErrorCollection`, or plain text).
  Assumed: nothing depends on it.

### The error body

`ErrorCollection`:

```json
{"errorMessages": ["Issue does not exist or you do not have permission to see it."],
 "errors": {"summary": "You must specify a summary of the issue."},
 "status": 404}
```

- `errorMessages` is the global errors; `errors` maps a field id or parameter to its message.
- `status` is in the schema.
  **Unconfirmed**: it is often absent.
- A createmeta 400 also carries `"httpStatusCode":{…}` (spec example);
  the removed search added `warningMessages`.
- **Unconfirmed**: the wording of the 404 message above.

### Status codes

From the operations' descriptions:

- **200, 201, 204**: success.
  Creates are 201, an edit 204 (200 with `returnIssue`), a transition 204, deletes 204,
  a comment update 200, and a link create 201 with an empty body.
- **207**: partial success (agile rank).
- **400**: invalid input, and on many writes a missing *permission* too:
  comment delete and update, a field's edit permission, a transition not allowed.
- **401**: missing or bad credentials.
  **Unconfirmed**: a revoked or expired token is a 401, with an empty or `ErrorCollection` body.
- **403**: some permission failures (override flags, *Browse users and groups* on `/user`,
  create without permission), and "no valid license" on agile.
- **404**: "not found **or the user does not have permission to view it**":
  Jira hides existence, so a 404 on an issue may be deleted, moved out of view, or permission lost.
- **405**: an anonymous comment delete.
- **409**: "the issue could not be updated due to a conflicting update" (edit and transition).
- **410**: a removed endpoint ([the old search](#the-old-search-is-removed)).
- **413**: a per-issue limit breached (comments, worklogs, attachments, issue links, remote links).
- **422**: a configuration problem prevents the create or update.
- **429**: rate limited. **5xx**, 503 among them, are transient.

### Special headers

From the v3 intro, "Special headers":

- `X-Atlassian-Token: no-check` is required on a multipart attachment upload (the XSRF check).
- `X-Force-Accept-Language: true`, with `Accept-Language`, localises names.
- The response header `X-AAccountId` is the caller's accountId.
- **Unconfirmed**: responses carry an `X-AREQUESTID` for support; it is not documented.

## Time

### Timestamps

| where | format | example | Go layout |
|---|---|---|---|
| issue `created`, `updated`, `resolutiondate`, `statuscategorychangedate`; comment `created`/`updated`; changelog `created`; `serverInfo.serverTime` | ISO-8601, milliseconds, **offset without a colon**, in the **site's default user zone** | `2015-12-02T07:39:15.000-0800` | `2006-01-02T15:04:05.000-0700` |
| `duedate`, date-picker custom fields | a date | `2024-01-15` | `2006-01-02` |
| sprint `startDate`, `endDate`, `completeDate` (agile, and the Sprint field) | ISO-8601, milliseconds; real values are UTC `Z`, the spec's examples a colon offset | `2025-03-12T01:36:46.600Z` | RFC 3339, which takes both |
| `X-RateLimit-Reset` | ISO-8601 `Z` | `2025-10-08T15:00:00Z` | RFC 3339 |
| bulk changelog `created` | [unconfirmed](#bulk-changelog) | `1492070429` in the example | |
| JQL literals | `"yyyy/MM/dd HH:mm"`, `"yyyy-MM-dd HH:mm"`, `"yyyy/MM/dd"`, `"yyyy-MM-dd"`, or relative such as `"-5m"` | `"2026/09/27 10:03"` | `2006/01/02 15:04`, in the **caller's profile zone** |

- The zone of a timestamp is the site's:
  "By default, top-level timestamps (e.g. updated and created) are returned in ISO 8601 format,
  in the **system default user time zone**.
  To return date time data in the logged in user's timezone, please refer to renderedFields" (v3 intro).
  It is not `+0000`: a capture from a real site has `"created":"2015-12-02T07:39:15.000-0800"`
  (go-jira, `issues_in_sprint.json`).
- A JQL literal is read in the caller's profile zone ([JQL](#jql-for-incremental-sync)),
  which can differ from the site's: a response's offset is parsed, never assumed.
  The fake's site zone is `America/Los_Angeles`, its API user's `Europe/Berlin`.
- A real Sprint date: `"startDate":"2025-03-12T01:36:46.600Z"` (blog.mikebowler.ca, 2026-01-29).
- **Unconfirmed**: a datetime custom field accepts ISO-8601 on write
  (the createIssue example shows the legacy `"06/Jul/19 3:25 PM"`),
  and a date picker an ISO date (the example's `"09/Jun/19"` is a locale format).

### The server clock

The server's clock is `serverInfo.serverTime`, or the HTTP `Date` header (RFC 1123) of any response.

## Webhooks

**Unconfirmed**, the whole section (the `/webhook` paths are in the spec; the rest was not re-read):
Jira Cloud pushes `jira:issue_created`, `jira:issue_updated`, `jira:issue_deleted`, `comment_created`,
`comment_updated`, `comment_deleted`, `issuelink_created`, `issuelink_deleted`, `sprint_*` and `board_*` events,
registered by an admin (system webhooks) or by a Connect, Forge or OAuth app through `POST /rest/api/3/webhook`
(dynamic, JQL-filtered, **expiring after 30 days** unless refreshed with `PUT /rest/api/3/webhook/refresh`).
A payload carries the issue and, for an update, a `changelog`.
Delivery is at least once, with retries and **no ordering guarantee**, and an event can be missed.
A receiver needs a public endpoint.

## Data Center

**Unconfirmed**, the whole section (no Data Center documentation was read):

- **Base and version**: `https://jira.example.com[/context]/rest/api/2/...`; no v3.
  The agile API is `/rest/agile/1.0`, with the same shapes.
- **Auth**: personal access tokens, `Authorization: Bearer <PAT>` (8.14 and later);
  Basic with username and password, which an admin may disable; OAuth 1.0a application links.
  No API tokens, no email login.
- **Bodies**: `description`, `environment`, a comment's `body` and textarea fields are **wiki-markup strings**,
  not ADF. Cloud's `/rest/api/2` also reads and writes wiki strings.
- **Users**: identified by `name` (the username) and `key`; there is no `accountId`.
  A user is set with `{"name":"jdoe"}`; `/rest/api/2/user?username=`.
  Emails are generally visible.
- **Search**: the classic `/rest/api/2/search`, with `startAt`, `maxResults`, `total` and `validateQuery`;
  no `/search/jql`, no token.
  The index is near-synchronous on one node, asynchronous across a cluster.
- **Parent**: Epic Link and Epic Name are the norm; `parent` is for sub-tasks;
  Advanced Roadmaps uses "Parent Link" above epics.
- **Create metadata**: the old `createmeta?expand=projects.issuetypes.fields` was removed in 9.0
  for the paginated pair, as on Cloud.
- **Timestamps** and JQL literals: as on Cloud.
- **Rate limiting**: optional and admin-configured (8.6 and later), 429 with `Retry-After` and `X-RateLimit-*`.
- `serverInfo.deploymentType` is `"Server"` or `"DataCenter"`;
  `/myself` has `name`, `key` and `emailAddress`.

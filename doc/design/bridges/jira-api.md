# Jira Cloud REST API reference for a bidirectional sync tool

Compiled 2026-09-28. Target: **Jira Cloud, REST v3** (platform) and **Jira Software Cloud REST 1.0** (agile).

## 0. Sources and confidence

**Primary sources.** The official OpenAPI specs, downloaded on 2026-09-27. Every endpoint below was read from these files, with its `operationId`:

- Platform v3: `https://developer.atlassian.com/cloud/jira/platform/swagger-v3.v3.json`
  (info.version `1001.0.0-SNAPSHOT-44cdd07c…`, 423 paths). Rendered docs:
  `https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-<group>/#api-<path>-<method>`.
- Software/Agile: `https://developer.atlassian.com/cloud/jira/software/swagger.v3.json`. Rendered docs:
  `https://developer.atlassian.com/cloud/jira/software/rest/api-group-<group>/`.
- The specs are not vendored: fetch them from the URLs above to re-check a claim.

**Secondary sources.** These pages were fetched, and each is cited where it is used:

- rate-limiting: `https://developer.atlassian.com/cloud/jira/platform/rate-limiting/`
- search-and-reconcile: `https://developer.atlassian.com/cloud/jira/platform/search-and-reconcile/`
- basic auth: `https://developer.atlassian.com/cloud/jira/platform/basic-auth-for-rest-apis/`
- ADF node pages under `https://developer.atlassian.com/cloud/jira/platform/apis/document/`
- the Jira Software REST intro: `https://developer.atlassian.com/cloud/jira/software/rest/intro/`
- the sprint toString deprecation notice
- the Epic Link / Parent Link deprecation announcement
- the JQL fields reference: `https://support.atlassian.com/jira-software-cloud/docs/jql-fields/`

**Markers.**

- `[UNVERIFIED]` means the statement comes from memory, community posts or inference, not from a fetched official page or the spec. A later vetting pass should check every one of them.
- `[DOC-EXAMPLE-BOGUS]` means the official spec's example is known to be unrealistic, so the fake server must **not** copy it literally. For example, `statusCategory.key` in the examples is `"in-flight"` and `"completed"`.

**General conventions (all endpoints):**

- The base URL is `https://<site>.atlassian.net`. For OAuth 2.0 (3LO) and for scoped API tokens it is `https://api.atlassian.com/ex/jira/{cloudId}` (see §1).
- JSON in and out. Send `Content-Type: application/json` and `Accept: application/json`.
- IDs are **strings** in the platform API: `"id":"10002"`. The exceptions are `statusCategory.id` (int), comment ids (strings), and agile ids such as sprint and board (**ints**).
- A multi-valued query param is either comma-separated (`fields=summary,status`) or repeated (`fields=a&fields=b`); the spec states both for `fields`, `properties` and others.
- Standard error body is `ErrorCollection` (see §12).
- **Pagination models.** There are three, and a client must implement all of them:
  1. **Offset page bean**: `startAt`, `maxResults`, `total`, `isLast`, `values[]`, with optional `self` and `nextPage`. Used by `/changelog`, `/priority/search`, `/user/bulk`, `/statuses/search`, `/workflows/search` and agile `/board`, `/board/{id}/sprint`.
  2. **Offset, legacy shape**: `startAt`, `maxResults`, `total` plus a named array (`comments`, `issueTypes`, `fields`, `histories`), with **no `isLast`**. Used by comments, the new createmeta, and the embedded changelog.
  3. **Token**: `nextPageToken` plus `isLast`, with **no total**. Used by `/search/jql` and `/changelog/bulkfetch`.
  - **Unpaged** plain arrays: `/field`, `/status`, `/statuscategory`, `/priority`, `/resolution`, `/project/{k}/statuses`, `/issuetype/project`, `/user/search` (the last is offset-paged by params, but its response is a bare array).
- The server may return fewer than `maxResults` items even when more exist, which search states explicitly. Always page until `isLast`, a missing token, or an empty page. Never stop on "fewer than maxResults".
  The agile intro says: "`total` may change while the client requests the next pages … always assume that the requested page can be empty" (software/rest/intro).

---

## 1. Authentication and identity

### 1.1 Basic auth: email and API token

Source: `https://developer.atlassian.com/cloud/jira/platform/basic-auth-for-rest-apis/`

```
Authorization: Basic base64("<email>:<api_token>")
```

```sh
curl -u fred@example.com:freds_api_token -H "Accept: application/json" \
  https://your-domain.atlassian.net/rest/api/3/myself
```

- Passwords are not accepted in Cloud. Only an API token is used as the password. [VETTED: the basic-auth page says "Authentication using passwords has been deprecated."]
- CAPTCHA: after repeated failures, the doc says to check the `X-Seraph-LoginReason` header, where `AUTHENTICATION_DENIED` means the password was not even checked (CAPTCHA triggered). The doc does not state the status code, but it is 401 `[UNVERIFIED]`.
- **Scoped API tokens** (granular scopes) must be sent to `https://api.atlassian.com/ex/jira/{cloudId}/rest/api/3/...`, still with Basic auth. `[UNVERIFIED]` This comes from community and search results, not from the fetched basic-auth page.
  - `cloudId` comes from `https://<site>.atlassian.net/_edge/tenant_info`. `[UNVERIFIED]`
- API tokens now have an expiry (one year by default, set at creation). `[UNVERIFIED]` This changed around 2024–2025.
- Rate limiting note from rate-limiting page: "API token-based traffic is not affected by this change [points-based quotas], and will continue to be governed by existing burst rate limits".

### 1.2 Bearer

- **OAuth 2.0 (3LO)**: `Authorization: Bearer <access_token>`. The base must be `https://api.atlassian.com/ex/jira/{cloudId}`. Scopes appear in the spec per operation (e.g. `read:jira-work`, `write:jira-work`, `read:jira-user`, `manage:jira-configuration`; agile: `read:sprint:jira-software`, `write:sprint:jira-software`, `write:issue:jira-software`, `write:board-scope:jira-software`).
- **PAT (Personal Access Token)**: this is a **Data Center/Server** concept (`Authorization: Bearer <PAT>`). Cloud has no PATs. `[UNVERIFIED]`, but well known.
- Design implication: the client takes `(baseURL, authHeader)`; the fake server accepts any `Basic` or `Bearer` it was configured with and otherwise returns 401.

### 1.3 `GET /rest/api/3/myself`

`operationId=getCurrentUser`. Doc: `…/rest/v3/api-group-myself/#api-rest-api-3-myself-get`.

- Query: `expand` = `groups`, `applicationRoles`.
- 200: a `User` object. 401 if the credentials are bad. OAuth scope `read:jira-user`.

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

- **The `timeZone` here is the timezone JQL date literals are interpreted in** (§2.5). Read it once per sync run.
- `key` and `name` are present but empty strings in examples. They were removed for GDPR, so do not use them.

### 1.4 `GET /rest/api/3/serverInfo`

`operationId=getServerInfo`. Anonymous is allowed.

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

- `deploymentType` is "always returned as *Cloud*" in Cloud (spec). On DC it is `"Server"` or `"DataCenter"` `[UNVERIFIED]`, so use it to pick Cloud or DC mode.
- `serverTime` is useful for clock-skew correction of sync watermarks.
- `healthChecks` is "Deprecated and no longer returned".

---

## 2. Searching

### 2.1 `GET /rest/api/3/search/jql` and `POST /rest/api/3/search/jql` (enhanced search, current)

`operationId=searchAndReconsileIssuesUsingJql` (GET) and `searchAndReconsileIssuesUsingJqlPost` (POST); the misspelling is Atlassian's.
Doc: `…/rest/v3/api-group-issue-search/#api-rest-api-3-search-jql-get`. Scope `read:jira-work`. Anonymous is allowed.

Query params (GET), or the same names in the POST body (`SearchAndReconcileRequestBean`):

| param | type | default | notes (quoted/condensed from spec) |
|---|---|---|---|
| `jql` | string | — | "requires a bounded query. A bounded query is a query with a search restriction." `order by key desc` is unbounded (rejected); `assignee = currentUser() order by key` is bounded. "`orderBy` clause can contain a maximum of 7 fields." |
| `nextPageToken` | string | null | Omit it for the first page. "The `nextPageToken` field is **not included** in the response for the last page". The token "will expire in 7 days". |
| `maxResults` | int32 | 50 | "API may return fewer items per page where a large number of fields or properties are requested. The greatest number of items returned per page is achieved when requesting `id` or `key` only. It returns max 5000 issues." |
| `fields` | string[] | **`id`** | `*all`, `*navigable`, `id`, a field id, or `-field` to exclude. "**By default, this resource returns IDs only.**" Always pass fields explicitly. |
| `expand` | string | — | **Comma-delimited string, also in the POST body** ("unlike the majority… `expand` is defined as a comma-delimited string"): `renderedFields`, `names`, `schema`, `transitions`, `operations`, `editmeta`, `changelog`, `versionedRepresentations`. |
| `properties` | string[] | — | Up to 5 issue property keys. |
| `fieldsByKeys` | bool | false | Reference fields by key instead of id. |
| `failFast` | bool | false | Fail the request early if not all field data can be retrieved. |
| `reconcileIssues` | int64[] | — | "Strong consistency issue ids to be reconciled with search results. Accepts max 50 ids. This list of ids should be consistent with each paginated request across different pages." Numeric **ids**, not keys. |
| `includeArchivedProjects` | bool | false | Issues in archived projects are excluded by default. |

POST body example:

```json
{
  "jql": "project = PROJ AND updated >= \"2026/09/27 10:00\" ORDER BY updated ASC, key ASC",
  "fields": ["summary", "status", "issuetype", "priority", "assignee", "reporter", "labels",
             "duedate", "created", "updated", "parent", "issuelinks", "description",
             "customfield_10020", "customfield_10016", "customfield_10019"],
  "expand": "names,schema",
  "maxResults": 100,
  "fieldsByKeys": false,
  "reconcileIssues": [10042, 10043]
}
```

For page 2 and later, send the same body plus `"nextPageToken": "<token from previous page>"`.

200 response (`SearchAndReconcileResults`):

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
  "names": {"summary": "Summary", "customfield_10020": "Sprint"},
  "schema": {"summary": {"type": "string", "system": "summary"}},
  "warnings": [ {"type": "CLAUSE_LIMIT_EXCEEDED", "message": "…", "details": {}} ]
}
```

- `nextPageToken` is absent or null on the last page. `isLast` is a boolean.
  [VETTED: the spec says both: the param doc says "not included", and the schema says "will be null"; `isLast` is in the schema. The client stops when `isLast==true` **or** the token is missing, null or empty. The fake omits the token on the last page and sets `isLast:true`.]
- **There is no `total`, `startAt` or `maxResults` in the response.** Use §2.3 for a count.
- `names` and `schema` are present only when expanded.
- `warnings` is "Experimental … rolling out behind a feature flag and may be absent, empty" (spec).

Errors: 400 "if the search request is invalid" (JQL parse error, unbounded query). The body is `ErrorCollection`, e.g.
`{"errorMessages":["Error in the JQL Query: …"],"errors":{},"warningMessages":[]}`. `[UNVERIFIED]` exact message text.
401 is for bad credentials. A JQL clause naming a project the user cannot see gives 400 "The value 'X' does not exist for the field 'project'." `[UNVERIFIED]`

### 2.2 Old `GET/POST /rest/api/3/search` (startAt/total): removed

`operationId=searchForIssuesUsingJql` and `searchForIssuesUsingJqlPost` are `deprecated: true` in the spec, with the text: "Endpoint is currently being removed. [More details](https://developer.atlassian.com/changelog/#CHANGE-2046)".

- Timeline per CHANGE-2046, from community and search summaries `[UNVERIFIED exact dates]`: deprecated 1 May 2025; progressive shutdown August–October 2025; blocked everywhere by the end of October 2025. The same applies to `/rest/api/2/search` and to `/rest/api/3/expression/eval` (replaced by `/expression/evaluate`) `[UNVERIFIED]`.
- Old params were `startAt`, `maxResults` (default 50), `validateQuery` (`strict|warn|none`), `fields` (default `*navigable`), `expand` (array in the POST body), `properties` and `fieldsByKeys`. The old response carried `startAt`, `maxResults`, `total` and `warningMessages`.
- **Do not implement it for Cloud.** It is still the search API on Data Center (§15).
- [VETTED: it now answers **410 Gone** with `{"errorMessages":["The requested API has been removed. …"]}` (atlassian-mcp-server#70, 2026-02-20). Removal was first dated 1 May 2025, moved to 1 Aug 2025, and rolled out per site through about Oct 2025 (Adaptavist breaking-changes note). The fake returns this 410.]

### 2.3 `POST /rest/api/3/search/approximate-count`

`operationId=countIssues`.

- Body `{"jql":"project = HSP"}`. The JQL must be bounded.
- 200: `{"count":153}`. "Provide an estimated count … Recent updates might not be immediately visible". 400 if the JQL cannot be parsed.

### 2.4 Consistency (search index lag)

From `…/search-and-reconcile/`:

- "The API doesn't provide read-after-write consistency by default … subsequent search operations … without the `reconcileIssues` parameter may return stale or outdated data … for some time."
- "The delay might vary from a few seconds to minutes, depending on the operation. The majority of modifications are shown within seconds."
- `reconcileIssues`: max 50 ids, and "consistency is ensured only for the specified issues". A reconciled issue is still returned only if it matches the JQL.
- `GET /rest/api/3/issue/{key}` reads the database, not the index, so it is read-after-write consistent `[UNVERIFIED]`. This is the standard understanding; the docs only single out search.
  [VETTED: still undocumented. Robust choice: verify our own writes by GET, and also pass `reconcileIssues` on the next search. The fake makes GET consistent and search lagging unless reconciled.]

### 2.5 JQL for incremental sync

From the JQL fields reference (support.atlassian.com/jira-software-cloud/docs/jql-fields/, Updated, Created and Resolved sections), verbatim:

> Note that if a time-component is not specified, midnight will be assumed. Please note that the search results will be relative to your configured time zone (which is by default the Jira server's time zone). Use one of the following formats: "yyyy/MM/dd HH:mm" "yyyy-MM-dd HH:mm" "yyyy/MM/dd" "yyyy-MM-dd" Or use "w" (weeks), "d" (days), "h" (hours) or "m" (minutes) to specify a date relative to the current time. The default is "m" (minutes). Be sure to use quote-marks ( " )

Recommended incremental query:

```
project = PROJ AND updated >= "2026/09/27 10:03" ORDER BY updated ASC, key ASC
```

- **Minute granularity**: literals have no seconds, so use `>=` with the watermark truncated down to the minute, and dedupe by `(id, updated)`.
- **Timezone**: the literal is interpreted in the **authenticated user's profile timezone** (`/myself` `timeZone`). The fallback is the site default (`/serverInfo` `serverTimeZone`). Convert your UTC watermark into that zone before formatting.
  [VETTED: correct. But response timestamps are rendered in the **system default user time zone** (v3 intro, "Timestamps"), which can differ from the caller's profile zone. Parse offsets from responses, and format JQL in `/myself.timeZone`. The fake uses two different zones.]
- Alternative: relative values (`updated >= "-15m"`) avoid timezone problems but depend on the server's clock.
- Also subtract a safety overlap of a few minutes for index lag (§2.4).
- `parent = PROJ-12` works for all project types. The JQL doc: "As of February 2024, Epic link function has been retired in favor of Parent. Existing filters that use the Epic link function still function".
- `sprint = 999`, `sprint in openSprints()`, `sprint in closedSprints()` (JQL doc).
- `key` and `issuekey` accept old keys after a move and match the new issue `[UNVERIFIED]`.

---
## 3. Issue read

### 3.1 `GET /rest/api/3/issue/{issueIdOrKey}`

`operationId=getIssue`. Doc: `…/rest/v3/api-group-issues/#api-rest-api-3-issue-issueidorkey-get`. Scope `read:jira-work`.

Query params:

| param | default | notes |
|---|---|---|
| `fields` | `*all` | "All fields are returned by default. This differs from [search] where the default is all navigable fields". Accepts `*all`, `*navigable`, a list, or `-x` to exclude. |
| `fieldsByKeys` | false | |
| `expand` | — | `renderedFields`, `names`, `schema`, `transitions`, `editmeta`, `changelog`, `versionedRepresentations` ("When included in the request, the `fields` parameter is ignored") |
| `properties` | null | `*all`, keys, or `-key` |
| `updateHistory` | false | Adds the issue to recently viewed. Keep it false for sync. |
| `failFast` | false | |

Key lookup semantics (spec, verbatim): "if the identifier doesn't match an issue, a case-insensitive search and check for moved issues is performed. If a matching issue is found its details are returned, a 302 or other redirect is **not** returned. The issue key returned in the response is the key of the issue found."
So a GET by an old key returns the issue with its **new** key. Detect a rename when `response.key != requested key`.

Responses:

- 200: `IssueBean`: `{expand, id, key, self, fields, renderedFields?, names?, schema?, transitions?, editmeta?, changelog?, operations?, properties?, versionedRepresentations?}`.
- 401 for bad credentials.
- **404: "if the issue is not found or the user does not have permission to view it."** 404 is also what you get for no permission (§12).

**`[DOC-EXAMPLE-BOGUS]`**: the spec's 200 example is unrealistic. It has `fields.comment` as an array, `fields.updated: 1`, a `sub-tasks` entry shaped like a link, and a `watcher` field. The real shape follows; field names and types are from the platform's system field ids. Nested object shapes follow the spec's component schemas (`StatusDetails`, `IssueTypeDetails`, `UserDetails`, `Priority`, `IssueLink`). Items marked `[UNVERIFIED]` are from practice.

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
    "created": "2026-09-01T09:12:44.513+0200",
    "updated": "2026-09-27T10:03:11.123+0200",
    "resolution": null,
    "resolutiondate": null,
    "statuscategorychangedate": "2026-09-20T11:00:00.000+0200",
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
    "customfield_10019": "0|i0003r:",
    "customfield_10014": "PROJ-3"
  }
}
```

Field notes:

- **summary**: a string. The limit is 255 characters `[UNVERIFIED]`.
- **description**: ADF (§7.4) or `null`. `environment` and every `textarea` custom field are also ADF. Single-line `textfield` custom fields are plain strings (spec, createIssue description).
  With `expand=renderedFields`, `renderedFields.description` is HTML.
- **issuetype**: `IssueTypeDetails`: `id, name, description, iconUrl, subtask, avatarId, hierarchyLevel, entityId (uuid, next-gen), scope`. See §8.4 for `hierarchyLevel`.
- **status**: `StatusDetails` `{id, name, description, iconUrl, self, statusCategory, scope?}`. `statusCategory` is `{id:int, key, name, colorName, self}`.
  Real keys: `new` (id 2, "To Do", `blue-gray`), `indeterminate` (id 4, "In Progress", `yellow`), `done` (id 3, "Done", `green`), and `undefined` (id 1, "No Category", `medium-gray`). [VETTED: correct, and it matches a real capture (go-jira `testing/mock-data/all_statuscategories.json`). The spec's examples show bogus keys `in-flight` and `completed` `[DOC-EXAMPLE-BOGUS]`.]
  **Map workflow semantics on `statusCategory.key`, never on the status name.**
- **priority**: `{id, name, iconUrl, self}`. It can be `null` if priorities are disabled or unset `[UNVERIFIED]`.
- **assignee, reporter, creator**: `UserDetails` `{self, accountId, accountType, emailAddress?, avatarUrls, displayName, active, timeZone?}`. `assignee` is `null` when unassigned.
  `emailAddress` "may be returned as null" (spec) depending on the user's privacy settings. It is usually **absent** rather than null `[UNVERIFIED]`.
  [VETTED: treat absent, `null` and `""` alike, because deleted users have "email is blank" (spec `User`).]
  For deleted users (GDPR "right to be forgotten"), `displayName` is something like "Former user" and the other fields have defaults. For corrupted records, `accountId` is `unknown` (spec, UserDetails).
- **labels**: a string array; labels cannot contain spaces `[UNVERIFIED]`.
- **duedate**: `"YYYY-MM-DD"` or null.
- **created, updated, resolutiondate**: `"2026-09-27T10:03:11.123+0200"`, meaning ms precision and an offset **without a colon**. Parse with Go layout `2006-01-02T15:04:05.000-0700`.
  [VETTED: the offset is the **system default user time zone** (v3 intro: "top-level timestamps … are returned in ISO 8601 format, in the system default user time zone"), not the caller's. A real fixture shows `"2015-12-02T07:39:15.000-0800"` (go-jira `issues_in_sprint.json`). The fake must emit a non-UTC offset.]
- **parent** (unified parent field): present for sub-tasks (parent is the level-0 issue) and for issues under an epic (parent is the epic), in both company-managed and team-managed projects.
  Shape from the deprecation announcement: `{"id","key","self","fields":{…}}`. [VETTED: `fields` holds `summary, status, priority, issuetype`, and possibly `assignee` (go-atlassian `ParentFieldsScheme`). With no parent the key is absent **or** `null` (a go-jira fixture has `"parent": null`), so accept both.]
  Levels above epic (Plans/Advanced Roadmaps "Parent Link", `customfield_xxxxx`) are also exposed via `parent` after the unification `[UNVERIFIED]`.
- **subtasks**: an array of `{id,key,self,fields{summary,status,priority,issuetype}}`, read-only. It is the inverse of `parent` for level -1 issues.
- **issuelinks**: an array of `IssueLink`. Each entry has **either** `outwardIssue` **or** `inwardIssue`, never both, relative to the issue being viewed (§9).
- **comment**: in `GET issue`, a page object `{comments[], maxResults, total, startAt, self}`. It may be truncated, so use the comments endpoint (§7) for sync. [VETTED: the page-object shape is confirmed by go-jira `Comments{comments}`, go-atlassian `IssueCommentPageScheme` and the go-jira fixtures. The spec example's array is `[DOC-EXAMPLE-BOGUS]`.]
- **Sprint** (`schema.custom = "com.pyxis.greenhopper.jira:gh-sprint"`, id typically `customfield_10020` but **discover it via /field**): an array of sprint objects or `null`.
  The object attributes per the toString deprecation notice are `id, name, state, boardId, goal, startDate, endDate, completeDate`. Dates are ISO-8601 with ms.
  [VETTED: real Cloud values are **UTC `Z`**, e.g. `"startDate":"2025-03-12T01:36:46.600Z"` (blog.mikebowler.ca 2026-01-29 capture). The `+10:00` values are doc examples. Parse with RFC 3339, which takes both; the fake emits `Z`.]
  The software intro says the list "includes the active/future sprint that the issue is currently in, as well as any closed sprints that the issue was in previously". Future sprints may lack dates.
- **Story points**: the ids vary per site, so **discover them via /field**. There are two fields `[UNVERIFIED names and custom type keys]`.
  [VETTED: the type keys are **not** reliable. "Story point estimate" is seen with both `jsw-story-points` and `customfieldtypes:float`. The resolution order is: (1) board config `estimation.field.fieldId`; (2) `schema.custom == com.pyxis.greenhopper.jira:jsw-story-points`; (3) a `float` field whose `untranslatedName`/`name` is exactly "Story Points" or "Story point estimate"; (4) ask the user. See jira-api-vetting.md C10.]
  - "Story point estimate": `com.pyxis.greenhopper.jira:jsw-story-points`, used by team-managed projects and newer company-managed boards, often `customfield_10016`.
  - "Story Points": `com.atlassian.jira.plugin.system.customfieldtypes:float`, classic.
  - The value is a number or null. Which one the board uses is `GET /rest/agile/1.0/board/{id}/configuration` → `estimation.field.fieldId` (spec example `"customfield_10002"`, displayName "Story Points"). The software intro: "the field is just a regular numeric field. The type of estimation and field used for estimation is determined by the board configuration."
- **Rank**: `schema.custom = "com.pyxis.greenhopper.jira:gh-lexo-rank"`. The value is a LexoRank string like `"0|i0003r:"`. [VETTED: pycontribs/jira finds the rank field by exactly this key. A real value is `"0|0zzzzd:vi"` (go-jira fixture). The format is `<bucket>|<base36>:<suffix>`; compare as byte strings.]
  The software intro says Rank is an internal field that "shouldn't be read or updated using the REST API". Read it only for ordering (it is lexicographically sortable) and write it only through `PUT /rest/agile/1.0/issue/rank` (§11.5).
  The rank field id is `board/{id}/configuration` → `ranking.rankCustomFieldId` (spec example `10020`).
- **Epic Link** (`com.pyxis.greenhopper.jira:gh-epic-link`, e.g. `customfield_10014`, a string key): **deprecated** in favour of `parent`.
  The announcement (community.developer.atlassian.com/t/…/54048) lists Epic Link `"customfield_10014": "ABC-1"`, Parent Link `customfield_10018`, the agile `epic` field, Epic Name and Epic Color as deprecated in REST and webhooks. The deadline was 30 November 2022, and "There will be no changes to the Jira UI. There will be no changes to JQL."
  Current status: many sites still return `customfield_10014` `[UNVERIFIED]`. **Read `parent` and ignore Epic Link.**
- **Keys and ids**: `id` is a numeric string, immutable, unique per site. `key` = `<PROJECTKEY>-<n>` **changes when an issue is moved to another project** or when a project key is renamed. Old keys keep resolving via GET (see above). **Store the id as the durable alias and the key as a display alias.**
  With `expand=changelog`, a move shows up as changelog items for `Key` and `project` `[UNVERIFIED]`.

### 3.2 `expand=names,schema`

- `names`: `{"customfield_10020": "Sprint", "summary": "Summary", …}`.
- `schema`: `{"customfield_10020": {"type":"array","items":"json","custom":"com.pyxis.greenhopper.jira:gh-sprint","customId":10020}, "summary": {"type":"string","system":"summary"}}`. The Sprint `items` value is `"json"`. [VETTED: community qaq-p/1570350 shows the same schema.]
  The `JsonTypeBean` fields are `type` (required), `items`, `system`, `custom`, `customId`, `configuration`.

### 3.3 `expand=changelog` on GET issue

It adds `changelog` as a `PageOfChangelogs` `{startAt, maxResults, total, histories[Changelog]}`. The spec's expand text says it is "sorted by date, starting from the most recent".
It is capped at the 100 most recent entries `[UNVERIFIED]`. **Use the dedicated endpoint (§4) for completeness.**

---

## 4. Changelog

### 4.1 `GET /rest/api/3/issue/{issueIdOrKey}/changelog`

`operationId=getChangeLogs`. Doc: `…/api-group-issues/#api-rest-api-3-issue-issueidorkey-changelog-get`.

- Query: `startAt` (default 0), `maxResults` (**default 100**).
- It is sorted "by date, starting from the oldest". Note that this is the opposite order from `expand=changelog`.
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
      "created": "2026-09-27T10:03:11.123+0000",
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

- The shape is taken from the spec. The item values are illustrative, and `[DOC-EXAMPLE-BOGUS]` the spec uses `"field":"fields","fieldId":"fieldId"`.
- The spec's `Changelog` has `id, author (UserDetails), created (date-time), items[ChangeDetails], historyMetadata`. `ChangeDetails` has `field` (the field **name**), `fieldId`, `fieldtype`, `from`, `fromString`, `to`, `toString`. All are strings and may be null.
- `fieldtype` is `"jira"` for system fields and `"custom"` for custom fields. [VETTED: a real Sprint item is `"fieldtype":"custom","fieldId":"customfield_10020","from":"76, 79"`, with ids **comma-space** separated (blog.mikebowler.ca). Parse `from`/`to`, never `fromString`, because sprint names may contain commas.]
- `from` and `to` hold raw ids: a status id, a user accountId, sprint ids comma-joined, an option id. `fromString` and `toString` hold display values.
- For `labels`, both strings are space-joined **whole sets**, not deltas (the spec example shows `"label-1"` → `"label-1 label-2"`).
- For `description`, `fromString` and `toString` hold the text in wiki or plain rendering, not ADF `[UNVERIFIED]`.
- `field` for a system field is sometimes the display name (`"status"`, `"assignee"`, `"Sprint"`, `"Rank"`, `"Link"`, `"Parent"`), so **match on `fieldId`**.
  [VETTED: `fieldId` is **often absent**, not only on old entries: on `IssueParentAssociation` always, and on many custom items (e.g. `{"field":"Epic Child","fieldtype":"custom",…}` with no `fieldId`). Rule: match `fieldId` if present, else `field`, **case-insensitively**. Also, histories are in **no guaranteed order**, so sort by `(created, numeric id)` yourself (blog.mikebowler.ca/2024/04/09/jira-issue-history/). The fake omits `fieldId` on some custom items.]
- The spec schema description says: "Changelogs related to workflow associations are currently being deprecated."

What is and is not in the changelog `[UNVERIFIED]` (standard behaviour, not stated in the fetched pages):

- **In**: field edits, status transitions (`status`, plus `resolution` when set), assignee, sprint, rank (`Rank` item, "Ranked higher/lower"), link add and remove (`field:"Link"`, e.g. `toString:"This issue blocks PROJ-20"`), attachment add and remove, worklog-derived time tracking fields, parent change, and issue move (`Key`, `project`, `Workflow`).
- **Not in**: **comment create, edit or delete** (use comment `created`/`updated` timestamps), watchers, votes, issue properties, and remote links `[UNVERIFIED]`.
- **Parent reparenting**: per the deprecation notice (`…/deprecation-notice-issue-reparenting-changelogs/`), the `field` values "Epic Link" and "Parent" in changelogs are consolidated into **`IssueParentAssociation`**. Old values were returned alongside it until 10 Dec 2021. Handle all three names.
  [VETTED: the item is `{"field":"IssueParentAssociation","fieldtype":"jira","from":"1234","fromString":"ABC-1","to":"4567","toString":"ABC-2"}`, where `from`/`to` are **parent issue ids** and the strings are **keys**, with **no `fieldId`**. Atlassian: "Consumers should not use `fieldId` for the IssueParentAssociation changelog items" (community.developer.atlassian.com/t/…/48993).]

### 4.2 `POST /rest/api/3/changelog/bulkfetch`

`operationId=getBulkChangelogs`. Doc: `…/api-group-issues/#api-rest-api-3-changelog-bulkfetch-post`. Scope `read:jira-work`.

"Returns a paginated list of all changelogs for given issues sorted by changelog date and issue IDs, starting from the oldest changelog and smallest issue ID … up to 1000 issues and can filter them by up to 10 field IDs."

Request (`BulkChangelogRequestBean`; `issueIdsOrKeys` is required):

```json
{ "issueIdsOrKeys": ["PROJ-12", "10043"], "fieldIds": ["status", "customfield_10020"], "maxResults": 1000, "nextPageToken": null }
```

The `maxResults` default and maximum are not stated in the spec `[UNVERIFIED]`.

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

- `[DOC-EXAMPLE-BOGUS?]` The spec example shows `created` as an **epoch number** (1492070429), while the schema says `date-time`. The real format is unknown `[UNVERIFIED]`, so parse both.
- `nextPageToken` is null on the last page. There is no `isLast`.
- 400 if there are no ids or more than 1000.

---
## 5. Create, edit and delete issues

### 5.1 `POST /rest/api/3/issue`

`operationId=createIssue`. Doc: `…/api-group-issues/#api-rest-api-3-issue-post`. Scope `write:jira-work`.

- Query: `updateHistory` (default false).
- Body: `IssueUpdateDetails` = `{fields?, update?, transition?, properties?[{key,value}], historyMetadata?}`. `fields` and `update` must not name the same field.
- Settable fields come from createmeta (§8.8), which lists "the same fields that appear on the issue's create screen".

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
  }
}
```

- User fields take `{"id": "<accountId>"}`; the spec examples use `assignee:{id}` and `reporter:{id}`. `{"accountId": "…"}` is also accepted `[UNVERIFIED]`. [VETTED: still unverified. The client sends `{"accountId"}`, the widely used form, and the fake accepts both.]
- The project can be given as `{"id"}` or `{"key"}`. The issue type can be given as `{"id"}` or `{"name"}` `[UNVERIFIED for name]`.
- Sprint takes the **numeric sprint id**, not an array. The software intro gives `"customfield_10021": 2`.
- Sub-task: `issuetype` must be a subtask type and `parent` must be set. "In a next-gen project any issue may be made a child providing that the parent and child are members of the same project."

201 response:

```json
{"id":"10000","key":"ED-24","self":"https://your-domain.atlassian.net/rest/api/3/issue/10000",
 "transition":{"status":200,"errorCollection":{"errorMessages":[],"errors":{}}}}
```

`transition` and `watchers` (both `NestedResponse`) appear only if they were requested.

Errors:

- 400 `ErrorCollection`. The spec example is `{"errorMessages":["Field 'priority' is required"],"errors":{}}`. Field-level errors look like `{"errors":{"summary":"You must specify a summary of the issue."}}` `[UNVERIFIED text]`.
  The spec reasons are: missing required fields, invalid values, fields not settable for the issue type, no permission, subtask in a different project than its parent.
  A field that is not on the create screen gives: `"errors":{"customfield_10016":"Field 'customfield_10016' cannot be set. It is not on the appropriate screen, or unknown."}` `[UNVERIFIED text]`.
- 401. 403 when the user lacks permission. 422 when "a configuration problem prevents the creation".

### 5.2 `POST /rest/api/3/issue/bulk`

`operationId=createIssues`. "Creates upto **50** issues".

- Body: `{"issueUpdates": [IssueUpdateDetails, …]}`.
- 201 (when any succeed): `{"issues":[{"id","key","self"}…],"errors":[BulkOperationErrorResult]}`.
- 400 when all fail, e.g.:

```json
{"issues":[],"errors":[{"elementErrors":{"errorMessages":[],"errors":{"issuetype":"The issue type selected is invalid.","project":"Sub-tasks must be created in the same project as the parent."}},"failedElementNumber":0,"status":400}]}
```

`failedElementNumber` is the 0-based index into `issueUpdates`.

### 5.3 `PUT /rest/api/3/issue/{issueIdOrKey}`

`operationId=editIssue`. Doc: `…/api-group-issues/#api-rest-api-3-issue-issueidorkey-put`.

Query params:

- `notifyUsers` (default **true**). Turning notifications off needs admin or project-admin; "If the user doesn't have the necessary permission the request is ignored".
  [VETTED: in practice the **whole edit fails** with `{"errorMessages":["To discard the user notification either admin or project admin permissions are required."],"errors":{}}` (UiPath forum 543771, k15t Backbone docs); assume 400. The client leaves `notifyUsers` unset, or on this error retries without it. The fake rejects it for non-admins.]
- `overrideScreenSecurity`, `overrideEditableFlag`: Connect and Forge admin apps only. Other callers get 403.
- `returnIssue` (default false). When true, the response is 200 with the issue.
- `expand`: used when `returnIssue` is true.

Body: `IssueUpdateDetails`. There are two forms, and one field cannot appear in both:

- `fields`: `{"summary":"x"}` is a plain set.
- `update`: `{"<field>": [ {"set"|"add"|"remove"|"edit"|"copy": value}, … ]}`. `FieldUpdateOperation` has the keys `set, add, remove, edit, copy`. The allowed ops per field are in editmeta `operations`.

```json
{
  "fields": {
    "summary": "Completed orders still displaying in pending",
    "customfield_10016": 5
  },
  "update": {
    "labels":      [ {"add": "triaged"}, {"remove": "blocker"} ],
    "components":  [ {"set": ""} ],
    "timetracking":[ {"edit": {"originalEstimate": "1w 1d", "remainingEstimate": "4d"}} ],
    "parent":      [ {"set": {"none": true}} ]
  },
  "properties": [ {"key": "gitwork.sync", "value": {"rev": 12}} ]
}
```

Semantics from the spec text:

- "issue transition is not supported and is ignored here". Status cannot be set here, so use §6.
- "The parent field may be set by key or ID. For standard issue types, the parent may be removed by setting `update.parent.set.none` to *true*."
  Setting it by key is `"fields":{"parent":{"key":"PROJ-3"}}`.
- "**Note:** This endpoint doesn't check screen configurations to determine if a field is editable." This is newer behaviour, see the "Deprecation of override screen security" announcement. **Do not model screen checks in the fake for PUT.** They are still applied on create and transition.
- `description`, `environment` and textarea custom fields take ADF. Single-line text fields take a string.
- Clearing a field: `"fields":{"duedate":null}`, `"assignee":null`, `"customfield_10016":null` `[UNVERIFIED]`, the standard behaviour.
- Sprint: `"customfield_10020": 37` moves the issue into sprint 37. `null` removes it from its active or future sprint `[UNVERIFIED for null]`.

Responses:

- 204 (empty) on success, or 200 with the `IssueBean` when `returnIssue=true`.
- 400: missing body, no permission to edit a field, unknown field or field not associated with the project and issue type, invalid transition.
- 401. 403 (override flags). 404 (not found or not visible).
- **409 "if the issue could not be updated due to a conflicting update."** Retry after re-reading.
- 422 on a configuration problem.
- Transitions additionally have 413 (per-issue limit on comments, worklogs, attachments or links).

There is **no optimistic concurrency** (no ETag or If-Match). The last writer wins per field. `[UNVERIFIED]` No such header is documented in the spec.

### 5.4 `DELETE /rest/api/3/issue/{issueIdOrKey}`

`operationId=deleteIssue`.

- Query: `deleteSubtasks` (`"true"|"false"`, default false).
- 204 on success. 400 "if the issue has subtasks and `deleteSubtasks` is not set to *true*". 401. 403 (no *Delete issues* permission). 404.
- A deleted issue simply stops appearing in search, and **there is no tombstone**. Detect deletions by 404 on GET, or by reconciling the full id set of the project periodically. Webhooks have `jira:issue_deleted`, but we poll.

---

## 6. Transitions

### 6.1 `GET /rest/api/3/issue/{issueIdOrKey}/transitions`

`operationId=getTransitions`. Doc: `…/api-group-issues/#api-rest-api-3-issue-issueidorkey-transitions-get`.

Query params:

- `expand=transitions.fields`: the screen fields per transition. "Fields hidden from the screen are not returned".
- `transitionId`
- `skipRemoteOnlyCondition` (admin apps)
- `includeUnavailableTransitions` (default false): "Whether details of transitions that fail a condition are included".
- `sortByOpsBarAndStatus` (default false)

Semantics:

- "if a request is made for a transition that does not exist or cannot be performed on the issue, given its status, the response will return any empty transitions list."
- Without the *Transition issues* permission, the list is empty (not 403).

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

- `IssueTransition` properties per the spec: `id, name, to (StatusDetails), hasScreen, isGlobal, isInitial, isAvailable, isConditional, looped, fields (map of FieldMetadata), expand`.
- The property is named `looped` in the schema but `isLooped` on the wire. [VETTED: go-atlassian models `json:"isLooped"`; ignore it.]
- `fields` is present only with the expand. `FieldMetadata` has `required, schema, name, key, operations, allowedValues, autoCompleteUrl, hasDefaultValue, defaultValue, configuration`.

### 6.2 `POST /rest/api/3/issue/{issueIdOrKey}/transitions`

`operationId=doTransition`. Body: `IssueUpdateDetails`, with `transition.id` required.

```json
{
  "transition": {"id": "31"},
  "fields": {"resolution": {"name": "Done"}},
  "update": {"comment": [ {"add": {"body": {"type": "doc", "version": 1, "content": [
    {"type": "paragraph", "content": [ {"type": "text", "text": "Closing via git-work"} ]} ]}}} ]}
}
```

Responses:

- 204 on success.
- 400: no transition specified, no permission, a field in `fields`/`update` that isn't on the transition screen, a field in both, or otherwise invalid.
  This includes a **transition not available from the current status**. The body is `{"errorMessages":["Transition id '31' is not valid for this issue."],"errors":{}}` [VETTED: the text is confirmed by multiple community reports, e.g. qaq-p/2158194].
  **409** is returned for **simultaneous transitions** on one issue (formerly 400; change notice "update in simultaneous transitions", 2020). On 409, re-read the transitions and retry.
  A failed validator (e.g. a required resolution) gives `errors:{"resolution":"…"}` `[UNVERIFIED]`.
- 401. 404. 409 (conflicting update). 413 (per-issue limits). 422 (configuration).

**Sync implication**: to set a status you must find a transition whose `to.id` equals the target status id among the currently available transitions. If none exists (no direct edge), either fail, or do a BFS over the workflow graph (§8.12) and walk several transitions.

---

## 7. Comments

### 7.1 `GET /rest/api/3/issue/{issueIdOrKey}/comment`

`operationId=getComments`. Doc: `…/api-group-issue-comments/#api-rest-api-3-issue-issueidorkey-comment-get`.

- Query: `startAt` (int64, default 0), `maxResults` (default **100**), `orderBy` (`created`, `-created`, `+created`; anything else gives 400), `expand=renderedBody`.
- 200 `PageOfComments` `{startAt, maxResults, total, comments[]}` (no `isLast`). 404 if the issue is not found or not visible.
- Only comments the user can see are returned (visibility restrictions).

`Comment` shape (spec properties):

```json
{
  "self": "https://your-domain.atlassian.net/rest/api/3/issue/10010/comment/10000",
  "id": "10000",
  "author": {"accountId": "5b10…", "displayName": "Mia Krystof", "active": true, "self": "…"},
  "body": {"type": "doc", "version": 1, "content": [
    {"type": "paragraph", "content": [ {"type": "text", "text": "Lorem ipsum…"} ]} ]},
  "updateAuthor": {"accountId": "5b10…", "displayName": "Mia Krystof", "active": true},
  "created": "2021-01-17T12:34:00.000+0000",
  "updated": "2021-01-18T23:45:00.000+0000",
  "visibility": {"type": "role", "value": "Administrators", "identifier": "Administrators"},
  "jsdPublic": true,
  "renderedBody": "<p>Lorem ipsum…</p>",
  "properties": []
}
```

- `visibility` is optional: `type` is `group` or `role`; `value` is the name; `identifier` is the group id or role name. "the name of a group is mutable, to reliably identify a group use `identifier`".
- `jsdPublic`: "Defaults to true when comments are created in the Jira Cloud Platform". It is always true outside JSM.
- `jsdAuthorCanSeeRequest` also exists (JSM).
- `renderedBody` is present only with `expand=renderedBody`.
- The comment `updated` differs from `created` when the comment was edited. **Comment edits do not bump anything in the changelog.** Whether they bump the issue's `updated` is `[UNVERIFIED]`; believed yes for add and edit.
  [VETTED: still unverified; Jira's `CommentManager.create(…, modifyIssueUpdateDate)` exists, so add very likely bumps it. The client must not rely on it: also run a periodic full comment reconcile. The fake bumps on add but not on edit or delete (the harder case).]
- The spec's updateComment text mentions "Child comments inherit visibility from their parent comment". Threaded comments exist on some products, and a `parentId` may appear `[UNVERIFIED]`.

### 7.2 Add, update, delete, get one

| op | method + path | operationId | body | success |
|---|---|---|---|---|
| add | `POST /rest/api/3/issue/{key}/comment` | `addComment` | `{"body": ADF, "visibility"?: {...}, "properties"?: [...]}` | **201** + Comment. 400, 401, 404, **413** (per-issue comment limit) |
| get | `GET /rest/api/3/issue/{key}/comment/{id}` | `getComment` | — | 200 Comment. 404 |
| update | `PUT /rest/api/3/issue/{key}/comment/{id}` | `updateComment` | same as add. Query `notifyUsers` (default true), `overrideEditableFlag`, `expand` | **200** + Comment. 400 (no permission or invalid, including changing a child comment's visibility), 401, 404 |
| delete | `DELETE /rest/api/3/issue/{key}/comment/{id}` | `deleteComment` | — | **204**. 400 (no permission), 401, 404, 405 (anonymous) |

Note that no-permission on delete and update is **400**, not 403.

Bulk: `POST /rest/api/3/comment/list` with `{"ids":[…]}` gets comments by id (`getCommentsByIds`). [VETTED from the spec: `ids` is int64[] with **at most 1000**; an optional `expand` query. It returns `PageBeanComment` `{self,nextPage,maxResults,startAt,total,isLast,values[]}`, or 400.]

### 7.3 Detecting deleted comments

Nothing records a deletion. Diff the comment id set per issue.

### 7.4 ADF (Atlassian Document Format), the minimum for text round-trips

Source: `https://developer.atlassian.com/cloud/jira/platform/apis/document/structure/` and the node and mark pages.

- **Root**: `{"version":1,"type":"doc","content":[…]}`. The empty document is `{"version":1,"type":"doc","content":[]}`.
- **Top-level block nodes**: blockquote, bulletList, codeBlock, expand, heading, mediaGroup, mediaSingle, orderedList, panel, paragraph, rule, table, multiBodiedExtension, bodiedSyncBlock, syncBlock.
- **Child blocks**: listItem, media, nestedExpand, tableCell, tableHeader, tableRow, blockTaskItem, extensionFrame.
- **Inline nodes**: text, hardBreak, mention, emoji, date, inlineCard, status, mediaInline.
- **Marks**: strong, em, code, link, strike, underline, subsup, textColor, border.

Node snippets:

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

Rules (from the node pages):

- `heading.attrs.level` is 1–6 (required), and its content is inline nodes.
- `codeBlock.attrs.language` is optional, as are `wrap` and `hideLineNumbers`. Its content is "one or more text nodes **without marks**", with newlines inside `text`.
- `code` mark: "can **ONLY** be combined with … `link`".
- `link` mark: `attrs.href` required; `title`, `id`, `collection` and `occurrenceKey` are optional.
- `listItem` content is at least one of paragraph (no marks), bulletList, orderedList, codeBlock (no marks), or mediaSingle. The docs don't say the first child must be a paragraph, but always emit a paragraph first.
- `orderedList.attrs.order` is an int ≥ 0; the default start is 1.
- `mention.attrs`: `id` (required, the accountId), `text` (with the leading @), `accessLevel` (NONE, SITE, APPLICATION or CONTAINER), `userType` (DEFAULT, SPECIAL or APP).
- A text node with an empty `text` is invalid, and so is a paragraph with a hardBreak only at the end `[UNVERIFIED]`. The server returns 400 `"INVALID_INPUT"` for malformed ADF `[UNVERIFIED]`.
- [VETTED: sending a **plain string** to an ADF field in v3 gives 400 `{"errors":{"description":"Operation value must be an Atlassian Document (see the Atlassian Document Format)"}}` (community qaq-p/1304733, qaq-p/1977160). The fake enforces this. Jira may normalise stored ADF (e.g. add `attrs`/`localId`), so compare canonicalised ADF, not bytes.]

Round-trip advice:

- Convert Markdown ↔ ADF with a fixed subset: paragraph, heading, bullet and ordered lists, codeBlock, blockquote, rule, hardBreak, strong, em, code, link, strike.
- Keep unknown nodes (mention, inlineCard, media, table, panel, status, emoji, date) **losslessly**. Either store the raw ADF alongside, or render them to Markdown placeholders and refuse to overwrite a Jira body that contains nodes the converter cannot regenerate.
- Otherwise every round trip destroys mentions and attachments.

---
## 8. Metadata for schema discovery

### 8.1 `GET /rest/api/3/field`

`operationId=getFields`. Doc: `…/api-group-issue-fields/#api-rest-api-3-field-get`.

- Unpaged array of `FieldDetails`. The permission rule: fields in no used configuration or screen are not returned, and others only if they are used in a project the user can browse.

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

- `FieldDetails` fields: `id, key, name, custom, orderable, navigable, searchable, clauseNames[], schema{type, items, system, custom, customId, configuration}, scope?`. `untranslatedName` appears on custom fields.
  - `scope` is present on team-managed custom fields: `{"type":"PROJECT","project":{"id":"10000"}}`. The same name can exist several times, so disambiguate by scope.
- Paged, filterable variant: `GET /rest/api/3/field/search`, `operationId=getFieldsPaginated`. Params: `startAt`, `maxResults` (default 50), `type=custom|system`, `id[]`, `query`, `orderBy`, `expand`, `projectIds[]`. It returns a page bean. This endpoint is admin-only `[UNVERIFIED]`.
- **Discovering well-known custom fields.** Match on `schema.custom`, not on the name, which is localized and user-editable:

| logical field | schema.custom | schema.type | status |
|---|---|---|---|
| Sprint | `com.pyxis.greenhopper.jira:gh-sprint` | array | verified from the software intro |
| Rank | `com.pyxis.greenhopper.jira:gh-lexo-rank` | any/string | [VETTED: pycontribs/jira] |
| Epic Link | `com.pyxis.greenhopper.jira:gh-epic-link` | any | [VETTED: go-jira and go-atlassian fixtures], deprecated |
| Story point estimate | `com.pyxis.greenhopper.jira:jsw-story-points` **or** `…customfieldtypes:float` | number | [VETTED: both seen; see the §3.1 resolution order] |
| Story Points (classic) | `com.atlassian.jira.plugin.system.customfieldtypes:float`, name "Story Points" | number | [VETTED: common; name match only] |
| Start date | `com.atlassian.jira.plugin.system.customfieldtypes:datepicker`, name "Start date" | date | `[UNVERIFIED]` |

### 8.2 `GET /rest/api/3/project/{projectIdOrKey}`

`operationId=getProject`. Doc: `…/api-group-projects/#api-rest-api-3-project-projectidorkey-get`.

- `expand`: `description`, `issueTypes`, `lead`, `projectKeys`, `issueTypeHierarchy`. The spec notes that "description, issue types, and project lead are included in all responses by default."
- `properties`: a list of project property keys.
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
  "projectKeys": ["EX", "OLDEX"],
  "versions": [], "components": [], "roles": {"Developers": "…/project/EX/role/10000"},
  "insight": {"lastIssueUpdateTime": "2021-04-22T05:37:05.000+0000", "totalIssueCount": 100},
  "uuid": "…"
}
```

- `style` is `classic` (company-managed) or `next-gen` (team-managed).
- `simplified` is true for team-managed projects `[UNVERIFIED]`: the spec only says "Whether the project is simplified".
- `projectTypeKey` is one of `software`, `service_desk`, `business` or `product_discovery`.
- `projectKeys` (with `expand=projectKeys`) lists **all keys the project has had**, useful for recognising old issue keys `[UNVERIFIED semantics]`. [VETTED: the expand option exists ("All project keys associated with the project"), but the `Project` schema has no such property, so the response key name is unverified. Treat it as optional.]
- `insight.lastIssueUpdateTime` is a cheap "anything changed?" probe `[UNVERIFIED freshness]`.
- Team-managed projects have project-scoped issue types (`scope.type=PROJECT`) and project-scoped custom fields and statuses.

### 8.3 `GET /rest/api/3/project/{projectIdOrKey}/statuses`

`operationId=getAllStatuses`. Returns the valid statuses grouped by issue type.

```json
[ {"self": "…/issueType/3", "id": "3", "name": "Task", "subtask": false,
   "statuses": [ {"self": "…", "description": "", "iconUrl": "…", "name": "In Progress", "id": "10000",
                  "statusCategory": {"id": 4, "key": "indeterminate", "colorName": "yellow", "name": "In Progress", "self": "…"}} ]} ]
```

The spec example omits `statusCategory`, but the schema is `StatusDetails`, which includes it.

### 8.4 `GET /rest/api/3/issuetype/project?projectId=<int64>`

`operationId=getIssueTypesForProject`.

- `projectId` is required and numeric. `level` filters: "`-1` for Subtask. `0` for Base. `1` for Epic."
- 200 array of `IssueTypeDetails`. 400. 404 (project not found or no permission).
- The project's hierarchy: `GET /rest/api/3/project/{projectId}/hierarchy` (`operationId=getHierarchy`, "for a next-gen project"). It returns:

```json
{"projectId": 10030, "hierarchy": [
  {"level": 0, "name": "Base", "issueTypes": [ {"id": 10008, "entityId": "…", "name": "Story", "avatarId": 10324} ]},
  {"level": 1, "name": "Epic", "issueTypes": [ {"id": 10007, "name": "Epic"} ]},
  {"level": -1, "name": "Subtask", "issueTypes": [ {"id": 10009, "name": "Subtask"} ]}]}
```

Note that the ids are ints here.

**hierarchyLevel semantics:**

- `-1` is a sub-task and must have a parent at level 0.
- `0` is the base level (Story, Task, Bug), and its parent is an epic.
- `1` is Epic.
- `≥2` are custom levels above Epic (e.g. Initiative). These are configurable only with Premium/Enterprise (Plans) `[UNVERIFIED]`: the spec only documents -1, 0 and 1.
- Rule: an issue's parent is at level+1. Sub-task ↔ level 0 is the only rule enforced for all plans. Levels above 1 are enforced by Plans `[UNVERIFIED]`.

### 8.5 Priorities

- `GET /rest/api/3/priority`: `operationId=getPriorities`, **deprecated** ("Use Search priorities instead"). An unpaged array of `{self, statusColor, description, iconUrl, name, id}`.
- `GET /rest/api/3/priority/search`: `operationId=searchPriorities`.
  - Params: `startAt` and `maxResults` (strings! default "0" and "50"), `id[]`, `projectId[]`, `priorityName`, `onlyDefault` (deprecated), `expand=schemes`.
  - Response: a page bean `{isLast, maxResults, startAt, total, values[Priority]}`. `Priority` = `{id, name, description, iconUrl, statusColor, isDefault (deprecated), avatarId, self, schemes?}`.
  - The spec lists its OAuth scope as `manage:jira-configuration`. Basic auth works for normal users `[UNVERIFIED]`.
- Priorities can be per project (priority schemes). Use `projectId=` to get the ones valid for our project.

### 8.6 `GET /rest/api/3/statuscategory`

`operationId=getStatusCategories`. An array of `StatusCategory` `{id:int, key, name, colorName, self}`.

- The real values are the four in §3.1 (`undefined`, `new`, `indeterminate`, `done`). [VETTED: go-jira real fixture; the spec example keys are `[DOC-EXAMPLE-BOGUS]`.]
- `GET /rest/api/3/statuscategory/{idOrKey}` also exists.

### 8.7 Statuses

- `GET /rest/api/3/status`: `operationId=getStatuses`. "all statuses associated with active workflows". An array of `StatusDetails` with the nested `statusCategory` object. Needs *Browse projects*.
- `GET /rest/api/3/statuses?id=…&id=…`: `operationId=getStatusesById`. Bulk get. The response is a `JiraStatus` array whose `statusCategory` is an **enum string** `TODO`, `IN_PROGRESS` or `DONE`, not an object.
- `GET /rest/api/3/statuses/search`: `operationId=search`.
  - Params: `projectId`, `startAt`, `maxResults` (default 200), `searchString`, `statusCategory` (`TODO|IN_PROGRESS|DONE`), `includeGlobalStatuses`.
  - Page bean with `values[JiraStatus]` = `{id, name, description, scope{type:"PROJECT"|"GLOBAL", project{id}}, statusCategory:"DONE"}`.
  - **Needs Administer projects or Administer Jira.** OAuth scope `manage:jira-configuration`.
- **Status category representation differs by endpoint**: an object `{id,key}` on issue, transition and `/status`, and an enum string `TODO|IN_PROGRESS|DONE` on `/statuses*` and `/workflows/search`. The mapping is `new`↔`TODO`, `indeterminate`↔`IN_PROGRESS`, `done`↔`DONE` `[UNVERIFIED mapping, obvious]`.

### 8.8 Create metadata

**New, paginated (use these):**

- `GET /rest/api/3/issue/createmeta/{projectIdOrKey}/issuetypes`: `operationId=getCreateIssueMetaIssueTypes`.
  - `startAt`, and `maxResults` (default 50, **max 200**).
  - 200: `{"issueTypes":[{id,name,description,iconUrl,self,subtask,avatarId,hierarchyLevel,entityId,scope}], "startAt":0, "maxResults":50, "total":1}`. The spec schema also lists a legacy alias array `createMetaIssueType`.
  - 400 example: `{"errorMessages":["Parameter 'maxResults' must not exceed the limit '200'"],"errors":{},…}`.
- `GET /rest/api/3/issue/createmeta/{projectIdOrKey}/issuetypes/{issueTypeId}`: `operationId=getCreateIssueMetaIssueTypeId`.
  - Same paging.
  - 200: `{"fields":[FieldCreateMetadata], "startAt","maxResults","total"}`. The schema also has a `results` alias. The spec example:

```json
{"fields":[{"fieldId":"assignee","key":"assignee","name":"Assignee","required":true,"hasDefaultValue":false,
            "operations":["set"],"schema":{"type":"user","system":"assignee"},"autoCompleteUrl":"…"}],
 "maxResults":1,"startAt":0,"total":1}
```

  - `FieldCreateMetadata` has these required keys: `fieldId, key, name, operations, required, schema`, plus `allowedValues, autoCompleteUrl, configuration, defaultValue, hasDefaultValue`.
  - For option, priority and issuetype fields, `allowedValues` holds the objects (e.g. `{"id":"3","name":"Medium",…}`).
- Permission: *Create issues* in the project.

**Old, deprecated:** `GET /rest/api/3/issue/createmeta?projectKeys=&issuetypeIds=&expand=projects.issuetypes.fields`, `operationId=getCreateIssueMeta`, `deprecated: true`.

- "Deprecated, see [Create Issue Meta Endpoint Deprecation Notice](https://developer.atlassian.com/cloud/jira/platform/changelog/#CHANGE-1304)".
- It was deprecated around December 2023, with no firm removal date announced as far as we found `[UNVERIFIED]`. Do not use it.

### 8.9 `GET /rest/api/3/issue/{issueIdOrKey}/editmeta`

`operationId=getEditIssueMeta`.

- Query: `overrideScreenSecurity`, `overrideEditableFlag` (admin apps; others get 403).
- 200: `{"fields": {"<fieldId>": FieldMetadata}}`. `FieldMetadata` = `{required, schema, name, key, operations[], allowedValues?, autoCompleteUrl?, hasDefaultValue, defaultValue?, configuration?}`. `operations` is e.g. `["set"]`, `["add","set","remove"]` for labels, or `["set","add"]`.
- The spec lists 9 conditions for a field to appear: on a screen; visible in the field configuration; shown on the issue; a valid custom field context; project, type and status present; a valid workflow step; the step editable (`jira.issue.editable`); *Edit issues* permission; and workflow `jira.permission.*` properties.
- Since PUT no longer checks screens (§5.3), editmeta is conservative relative to what PUT accepts.

### 8.10 `GET /rest/api/3/issueLinkType`

`operationId=getIssueLinkTypes`.

```json
{"issueLinkTypes":[
  {"id":"1000","name":"Duplicate","inward":"is duplicated by","outward":"duplicates","self":"…/issueLinkType/1000"},
  {"id":"10000","name":"Blocks","inward":"is blocked by","outward":"blocks","self":"…"}]}
```

- 404 if issue linking is disabled.
- The spec example's Blocks entry has `"inward":"Blocked by","outward":"Blocks"`. Default sites have `"is blocked by"` / `"blocks"` `[UNVERIFIED]`. **Never hard-code the text; use `name` or `id`.**

### 8.11 Resolutions

- `GET /rest/api/3/resolution`: `operationId=getResolutions`, **deprecated**. An array of `{self,id,description,name}`.
- The replacement is `GET /rest/api/3/resolution/search` (`searchResolutions`; paged: `startAt`, `maxResults`, `id[]`, `onlyDefault`). [VETTED from the spec: `startAt`/`maxResults` are strings, defaults "0"/"50"; the response is `PageBeanResolutionJsonBean`.]
- A resolution is set by transitions (screen field `resolution`). `fields.resolution` is null while unresolved.

### 8.12 Workflow and transition-graph discovery without an issue

- `GET /rest/api/3/workflows/search`: `operationId=searchWorkflows`.
  - Params: `startAt`, `maxResults`, `expand=values.transitions`, `queryString`, `orderBy` (name, created, updated), `scope` (GLOBAL or PROJECT), `isActive`, `projectId`.
  - **Permissions**: "*Administer Jira* global permission to access all, including project-scoped, workflows", or "At least one of the *Administer projects* and *View (read-only) workflow* project permissions to access project-scoped workflows".
  - OAuth scope `manage:jira-configuration`.
  - Response (trimmed from the spec):

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

  - Graph rules: `type` is `INITIAL`, `DIRECTED` or `GLOBAL`. A `GLOBAL` transition is reachable from any status. `DIRECTED` edges come from each `links[].fromStatusReference`. A transition's `id` is the id to POST in §6.2 `[UNVERIFIED: same id space]`.
  - Mapping workflow ↔ (project, issue type) needs workflow schemes (`/rest/api/3/workflowscheme/project?projectId=`, admin) `[UNVERIFIED]`. Alternatively use `projectId=` on the search, but that does not tell you which issue type uses which workflow.
- `GET /rest/api/3/workflow/search`: `operationId=getWorkflowsPaginated`, **deprecated**. "This will be removed on June 1, 2026 (CHANGE-2569)". Classic workflows only, admin only. It is **already removed** as of today (2026-09-28) `[UNVERIFIED]`.
- **Non-admin fallback**: observe `GET /issue/{key}/transitions` on real issues and cache the edges by `(project, issuetype, fromStatus)`. This is the only discovery available to a normal user.

---

## 9. Issue links

### 9.1 `POST /rest/api/3/issueLink`

`operationId=linkIssues`. Doc: `…/api-group-issue-links/#api-rest-api-3-issuelink-post`.

```json
{
  "type": {"name": "Blocks"},
  "inwardIssue":  {"key": "PROJ-12"},
  "outwardIssue": {"key": "PROJ-20"}
}
```

[VETTED: the keys were swapped so that this body means **PROJ-12 blocks PROJ-20**, matching the §3.1 read example. The `comment` was removed because the client must not send one; see below.]

- `type` takes `{name}` or `{id}`. The issues take `{id}` or `{key}`. `comment` is optional and "add[ed] to the from (outward) issue".
- **201 with an empty body.** "To obtain the ID of the issue link, use `…/issue/[linked issue key]?fields=issuelinks`." A duplicate request "indicates that the issue link was created" and creates no second link, so the call is idempotent.
- Errors: 400 (the comment failed, and then the link is not created either); 401; 404 (linking disabled, either issue not visible, or no *Link issues* permission); 413 (per-issue link limit).

**Which is which.** [VETTED: rewritten. The previous text had the POST direction backwards.]
A link type has `outward` text ("blocks") and `inward` text ("is blocked by"). Internally a link is `source → destination`, read as "source *outward-text* destination".
In the POST, **`inwardIssue` is the source and `outwardIssue` is the destination**:

- `{"type":{"name":"Blocks"},"inwardIssue":{"key":"A"},"outwardIssue":{"key":"B"}}` means **A blocks B**.
- Viewing **A**, `fields.issuelinks` has `{"outwardIssue": B}`, labelled with `type.outward`: "A blocks B".
- Viewing **B**, it has `{"inwardIssue": A}`, labelled with `type.inward`: "B is blocked by A".
- Mnemonic: in the POST, put each issue under the key it will appear under **when viewed from the other issue**.

Evidence:

- Atlassian's Java client JRJC `LinkIssuesInputGenerator` sends `inwardIssue = fromIssueKey` and `outwardIssue = toIssueKey`; pre-5.0 servers used `fromIssueKey`/`toIssueKey` directly.
- The Jira REST docs (4.4+): the request "will create a link from the first issue to the second issue using the outward description".
- A Cloud report: `outwardIssue: IQS-2, inwardIssue: IQS-4, Blocks` made the UI show "IQS-2 is blocked by IQS-4" (github.com/atlassian/atlassian-mcp-server/issues/112).
- The read-side labelling rule: developer.atlassian.com/cloud/jira/platform/issue-linking-model/.

Contradiction: the spec's linkIssues text calls the outward issue the "from" issue, for the comment target and for the *Link issues* permission. **The client never sends `comment` on link creation**, and a live smoke test should still confirm the direction.
The fake stores `{source: inwardIssue, dest: outwardIssue}`. It emits `outwardIssue: dest` on the source's `issuelinks` and `inwardIssue: source` on the dest's, and puts a comment, if one is given, on the `outwardIssue`, as the spec says.

### 9.2 `GET /rest/api/3/issueLink/{linkId}` and `DELETE /rest/api/3/issueLink/{linkId}`

- GET: `operationId=getIssueLink`. 200 `IssueLink` `{id, self, type{id,name,inward,outward,self}, inwardIssue{id,key,self,fields{summary?,status,priority,issuetype}}, outwardIssue{…}}`. Both sides are present here.
- DELETE: `operationId=deleteIssueLink`. **204** (the spec also lists "200 response"). 400 (invalid id). 401. 404 (disabled, not found, or no permission).
- The link id is the same id that appears in `fields.issuelinks[].id` of both issues.

### 9.3 Shape inside `fields.issuelinks`

This is the `IssueLink` without the viewed side:

```json
{"id":"10001","self":"…/issueLink/10001",
 "type":{"id":"10000","name":"Blocks","inward":"is blocked by","outward":"blocks","self":"…"},
 "outwardIssue":{"id":"10060","key":"PROJ-20","self":"…","fields":{"summary":"…","status":{…},"priority":{…},"issuetype":{…}}}}
```

- Links are symmetric records: one link appears on both issues with opposite sides.
- **Epic/parent and sub-task relations are not issue links.** They are `parent`.

---

## 10. Users

### 10.1 `GET /rest/api/3/user?accountId=…`

`operationId=getUser`.

- `accountId` is required (max 128 characters). `username` and `key` are "no longer available". `expand` takes `groups` and `applicationRoles`.
- 200 `User`. 401. **403 without the *Browse users and groups* global permission.** 404 if not found.

### 10.2 `GET /rest/api/3/user/search?query=…`

`operationId=findUsers`.

- `query` is matched against `displayName` and `emailAddress` by prefix. Alternatives are `accountId` (exact) or `property`, and one of the three is required. Paging is `startAt` and `maxResults` (default 50), "up to the thousandth user".
- 200 is a **bare array** of `User`. 400 (missing params, or both `query` and `accountId`). 401.
- **429**: "User search endpoints share a collective rate limit for the tenant … Please respect the Retry-After header."
- Without *Browse users and groups*, the result is **empty, not an error**.
- Email lookup: an exact email usually finds the user even when the email is hidden. Per the old search docs: "If a user has hidden their email address … partial matches of the email address will not find the user. An exact match is required."

### 10.3 `GET /rest/api/3/user/bulk?accountId=a&accountId=b`

`operationId=bulkGetUsers`.

- `accountId[]` is required and repeated. `startAt`, and `maxResults` (default 10).
- 200 page bean `{isLast, maxResults, startAt, total, values[User]}`. 400 if accountId is missing.
- The maximum is 200 ids per call `[UNVERIFIED]`.

### 10.4 Privacy, GDPR, account types

- `User.accountType` is an enum: `atlassian` (normal), `app` (Connect/Forge/OAuth integration, `appType` `service|agent|unknown`), `customer` (JSM portal-only), or `unknown`.
- `emailAddress` "may be returned as null" depending on profile visibility. It is typically omitted `[UNVERIFIED]`.
  Only `/myself` reliably includes the caller's own email, and even that depends on privacy settings `[UNVERIFIED]`.
- `timeZone` and `locale` may also be hidden. For `timeZone`, the spec says "the instance's default time zone will be returned" instead.
- `displayName` "may return an alternative value" for privacy.
- `active: false` means deactivated.
- **Identity mapping implication**: key identities on `accountId`. Treat email as an optional, best-effort join key. Never try to create Jira users.

---
## 11. Agile (Jira Software REST 1.0)

Base path: `/rest/agile/1.0`. Doc root: `https://developer.atlassian.com/cloud/jira/software/rest/`.

- Agile ids (board, sprint, epic) are **integers** in JSON.
- Paging is `{startAt, maxResults, total, isLast, values}`.
- 403 here often means "does not have a valid license" (no Jira Software access).
- Sprint dates carry a **colon offset** (`2015-04-11T15:22:00.000+10:00`) in the spec examples, unlike platform timestamps. [VETTED: real values are UTC `Z` (`2025-03-12T01:36:46.600Z`). Parse with RFC 3339.]

### 11.1 `GET /rest/agile/1.0/board`

`operationId=getAllBoards`. Doc: `…/software/rest/api-group-board/#api-rest-agile-1-0-board-get`.

- Params: `startAt` (0), `maxResults` (50), `type` (`scrum|kanban|simple`), `name`, `projectKeyOrId`, `accountIdLocation`, `projectLocation`, `includePrivate`, `negateLocationFiltering`, `orderBy=name`, `expand=admins,permissions`, `projectTypeLocation`, `filterId`.
- `projectKeyOrId` matches boards "relevant to a project … the jql filter defined in board contains a reference to a project". A project can have 0..n boards.
- 200:

```json
{"isLast":false,"maxResults":2,"startAt":1,"total":5,"values":[
  {"id":84,"name":"scrum board","self":"…/rest/agile/1.0/board/84","type":"scrum"},
  {"id":92,"name":"kanban board","self":"…","type":"kanban"}]}
```

- Real entries also carry `location {projectId, projectKey, projectName, displayName, projectTypeKey, avatarURI}` `[UNVERIFIED]`.
- Errors: 400, 401, 403 (license).
- `GET /rest/agile/1.0/board/{boardId}/configuration` (`getConfiguration`) gives the columns → status ids, `estimation.field.fieldId` (the story-points field), `ranking.rankCustomFieldId`, `filter.id` and `location`. See §3.1.

### 11.2 `GET /rest/agile/1.0/board/{boardId}/sprint`

`operationId=getAllSprints`.

- Params: `startAt`, `maxResults`, `state` (comma list of `future,active,closed`).
- "Sprints will be ordered first by state (i.e. closed, active, future) then by their position in the backlog."
- 200:

```json
{"isLast":false,"maxResults":2,"startAt":1,"total":5,"values":[
  {"id":37,"self":"…/rest/agile/1.0/sprint/23","state":"closed","name":"sprint 1",
   "startDate":"2015-04-11T15:22:00.000+10:00","endDate":"2015-04-20T01:22:00.000+10:00",
   "completeDate":"2015-04-20T11:04:00.000+10:00","originBoardId":5,"goal":"sprint 1 goal"},
  {"id":72,"self":"…","state":"future","name":"sprint 2","goal":"sprint 2 goal"}]}
```

- Future sprints may lack `startDate` and `endDate`. `completeDate` exists only when the sprint is closed. There is also a `createdDate` (it is in the sprint update body schema).
- Errors: 400, 401, 403, 404 (board not found or not visible). Kanban boards return 400 ("board does not support sprints") `[UNVERIFIED]`.
- Single sprint: `GET /rest/agile/1.0/sprint/{sprintId}` (`getSprint`), with the same object.
- Partial update: `POST /rest/agile/1.0/sprint/{sprintId}` (`partiallyUpdateSprint`). Rules from the spec:
  - Closed sprints allow only name and goal.
  - `future→active` needs `startDate` and `endDate`.
  - `active→closed` sets `completeDate`.
  - No other state changes are allowed.
- Create: `POST /rest/agile/1.0/sprint` with `{name, originBoardId, startDate?, endDate?, goal?}` `[UNVERIFIED body; path exists]`.

### 11.3 `POST /rest/agile/1.0/sprint/{sprintId}/issue` (move issues to a sprint)

`operationId=moveIssuesToSprintAndRank`.

- "Issues can only be moved to open or active sprints. The maximum number of issues that can be moved in one operation is 50."
- Body: `{"issues":["PR-1","10001","PR-3"], "rankBeforeIssue":"PR-4", "rankAfterIssue"?:…, "rankCustomFieldId":10521}`. The rank parts are optional.
- 204. 400. 401. 403 (license or no permission to assign). 404 (sprint).
- The same is possible via the platform: `PUT /rest/api/3/issue/{key}` with `{"fields":{"customfield_10020": 37}}` (the software intro example `"customfield_10021": 2`). "an issue can only be in one active or future sprint at a time" (intro).

### 11.4 `POST /rest/agile/1.0/backlog/issue` (move to backlog)

`operationId=moveIssuesToBacklog`.

- Body: `{"issues":["10001","PR-1"]}`, at most 50. "equivalent to remove future and active sprints from a given set of issues."
- 204. 400. 401. 403. 404.
- The board-scoped variant is `POST /rest/agile/1.0/backlog/{boardId}/issue` (it also takes rank params) `[UNVERIFIED body]`.

### 11.5 `PUT /rest/agile/1.0/issue/rank`

`operationId=rankIssues`.

- "Moves (ranks) issues before or after a given issue. At most 50 issues may be ranked at once."
- "If rankCustomFieldId is not defined, the default rank field will be used."

```json
{"issues":["PR-1","10001","PR-3"],"rankBeforeIssue":"PR-4","rankCustomFieldId":10521}
```

- Use exactly one of `rankBeforeIssue` / `rankAfterIssue` `[UNVERIFIED: both are allowed?]`.
- **204** when all succeed. **207** when some fail:

```json
{"entries":[{"issueId":10000,"issueKey":"PR-1","status":200},
            {"issueId":10002,"issueKey":"PR-3","status":503,
             "errors":["JIRA Agile cannot execute the rank operation at this time. Please try again later."]}]}
```

- 400. 401. 403 (license, or no *Schedule issues* permission: "To rank issues user has to have schedule issue permission").
- There is no absolute-position API. Ranking is always relative to another issue, so a sync must translate a local order into a sequence of relative moves.

### 11.6 Epics (deprecated in favour of `parent`)

- `GET /rest/agile/1.0/epic/{epicIdOrKey}` and `POST` (partial update): "does not work for epics in next-gen projects".
- `GET /rest/agile/1.0/epic/{epicIdOrKey}/issue` (`getIssuesForEpic`) is `deprecated: true`. Its text says to use JQL search instead (`parent = EPIC-1`).
- `POST /rest/agile/1.0/epic/{epicIdOrKey}/issue` (`moveIssuesToEpic`, max 50) and `/epic/none/issue` still exist but are classic-only.
- **Use `parent` on PUT issue.**
- The agile `GET /rest/agile/1.0/issue/{key}` adds the `sprint` (the current one), `closedSprints[]`, `flagged` and `epic` fields. The spec example: `"epic":{"id":37,"name":"epic 1","summary":"…","color":{"key":"color_4"},"done":true}`. The `epic` field is deprecated.
- Estimation: `GET/PUT /rest/agile/1.0/issue/{key}/estimation?boardId=` reads or writes the board's estimation field value `[UNVERIFIED body: {"value":"8.0"}]`.

---

## 12. Rate limiting and errors

### 12.1 Rate limits

Source: `https://developer.atlassian.com/cloud/jira/platform/rate-limiting/`.

- The status is **429 Too Many Requests**. "Some transient 5xx responses may also include `Retry-After`".
- Headers:

| header | format | meaning |
|---|---|---|
| `Retry-After` | integer seconds | how long to wait |
| `X-RateLimit-Limit` | int | max rate for the current scope |
| `X-RateLimit-Remaining` | int | remaining capacity in the window |
| `X-RateLimit-Reset` | ISO-8601 timestamp | window reset (only on 429) |
| `X-RateLimit-NearLimit` | `true`/`false` | true when less than 20% of capacity remains |
| `RateLimit-Reason` | enum | `jira-quota-global-based`, `jira-quota-tenant-based`, `jira-burst-based`, `jira-per-issue-on-write` |
| `Beta-RateLimit-Policy`, `Beta-RateLimit` | `"<policy>";q=…;w=…;r=…;t=…` | informational |

- Example:

```
HTTP/1.1 429 Too Many Requests
Retry-After: 1847
X-RateLimit-Limit: 100000
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 2025-10-08T15:00:00Z
RateLimit-Reason: jira-quota-global-based
```

- Points quotas are hourly and apply to OAuth and Forge apps. Per that page, "API token-based traffic is not affected … governed by existing burst rate limits".
- Burst defaults: GET 100 rps, POST 100 rps, PUT 50 rps, DELETE 50 rps.
- **Per-issue write limits: 20 writes per 2 s and 100 writes per 30 s** (reason `jira-per-issue-on-write`). This matters when pushing many field edits to one issue: batch them into one PUT.
- Recommended backoff: respect `Retry-After`, otherwise exponential backoff from a 2 s base, capped at 30 s, with jitter ×0.7–1.3, and at most 4 retries.
- The 429 body is not specified. It is probably `ErrorCollection` or plain text `[UNVERIFIED]`, so the client must not depend on it.
- [VETTED: the page's own burst example is `Retry-After: 1`, `X-RateLimit-Limit: 350`, `X-RateLimit-Remaining: 0`, `X-RateLimit-Reset: 2026-01-01T01:01:01Z`, `RateLimit-Reason: jira-burst-based`, `Content-Type: application/json`. `Retry-After` is "Only returned with 429 responses", but also on some 503s. Legacy informational `X-Beta-RateLimit-*` headers also exist. The fake's 429 should look like this.]

### 12.2 Error body

The `ErrorCollection` schema:

```json
{"errorMessages": ["Issue does not exist or you do not have permission to see it."],
 "errors": {"summary": "You must specify a summary of the issue."},
 "status": 404}
```

- `errorMessages` is a string array of global errors. `errors` maps a field id or parameter to a message. `status` is in the schema, but it is often absent `[UNVERIFIED]`.
- Some 400s (createmeta) also carry `"httpStatusCode":{…}` (spec example).
- Some responses add `warningMessages` (old search). JQL errors are 400 with `errorMessages`.
- The 404 text above is the well-known message `[UNVERIFIED wording]`.

### 12.3 Status code semantics (from the per-operation spec descriptions)

- **200/201/204**: success. Creates are 201, PUT edit is 204 (200 with `returnIssue`), transitions are 204, deletes are 204, comment update is 200, and link create is 201 with an empty body.
- **207**: partial success (agile rank).
- **400**: invalid input. It is also used for *permission* failures on many writes: comment delete and update, field edit permission, and transition not permitted.
- **401**: missing or bad credentials. Cloud returns 401 for a revoked or expired token `[UNVERIFIED body]`.
- **403**: some permission failures (override flags, *Browse users and groups* on `/user`, create without permission), plus "no valid license" on agile.
- **404**: "not found **or the user does not have permission to view it**". Jira hides existence, so a 404 on GET issue is ambiguous between deleted, moved-and-hidden and permission lost. Confirm deletion with a search by id (`id = 10042`), or treat it as "gone from our view".
- **405**: anonymous call to comment delete.
- **409**: "the issue could not be updated due to a conflicting update" (edit and transition). Retry.
- **413**: a per-issue limit was breached (comments, worklogs, attachments, issue links, remote links).
- **422**: a configuration problem prevents the create or update.
- **429**: rate limited. **5xx and 503** are transient; retry with backoff.

### 12.4 Special request headers [VETTED: the first two are confirmed by the v3 intro "Special headers"; `X-AREQUESTID` is not documented]

- `X-Atlassian-Token: no-check` is required for multipart attachment upload (XSRF check).
- `X-Force-Accept-Language: true` together with `Accept-Language` localizes names.
- Response headers include `X-AREQUESTID`, useful for support tickets.
- [VETTED: the documented response header is **`X-AAccountId`**, the caller's accountId. It is a cheap identity check.]

---

## 13. Time formats

| where | format | example | Go layout |
|---|---|---|---|
| issue `created`, `updated`, `resolutiondate`, `statuscategorychangedate`; comment `created`/`updated`; changelog `created` | ISO-8601, ms, **offset without colon**, in the **site default user zone** [VETTED] | `2015-12-02T07:39:15.000-0800` | `2006-01-02T15:04:05.000-0700` |
| `duedate`, date-picker custom fields | date only | `2024-01-15` | `2006-01-02` |
| datetime custom fields (write) | ISO-8601 accepted `[UNVERIFIED]`; the createIssue example shows the legacy `"06/Jul/19 3:25 PM"` | | |
| sprint `startDate`/`endDate`/`completeDate` (agile and Sprint field) | ISO-8601, ms; real values are UTC `Z`, doc examples show a colon offset [VETTED] | `2025-03-12T01:36:46.600Z` | `time.RFC3339Nano` |
| `serverInfo.serverTime` | as issue timestamps | `2020-03-31T16:43:50.000+0000` | as above |
| `X-RateLimit-Reset` | ISO-8601 `Z` | `2025-10-08T15:00:00Z` | RFC3339 |
| bulk changelog `created` | the spec example shows epoch seconds `1492070429` `[UNVERIFIED]` | | parse both |
| JQL literals | `"yyyy/MM/dd HH:mm"`, `"yyyy-MM-dd HH:mm"`, `"yyyy/MM/dd"`, `"yyyy-MM-dd"`, relative `"-5m"` | `"2026/09/27 10:03"` | `2006/01/02 15:04` in the **user's timezone** |

Notes:

- [VETTED] Timestamps are rendered in the "system default user time zone" (v3 intro), and JQL literals are read in the caller's profile zone. Always parse the offset; never assume `+0000`.
- [VETTED: changed.] The fake server emits platform timestamps in a **non-UTC** site zone (e.g. `-0700`), uses a *different* zone for the user's JQL, and emits `Z` for sprint dates.
- The create example's custom date formats (`"09/Jun/19"`) are legacy locale-dependent formats. Send ISO dates (`"2019-06-09"`) for date pickers `[UNVERIFIED]`.

---

## 14. Webhooks (we poll, one paragraph)

Jira Cloud can push `jira:issue_created`, `jira:issue_updated`, `jira:issue_deleted`, `comment_created`, `comment_updated`, `comment_deleted`, `issuelink_created`, `issuelink_deleted`, `sprint_*` and `board_*` events. They are registered either by an admin (system webhooks, `Settings → System → Webhooks`) or by Connect/Forge/OAuth apps through `POST /rest/api/3/webhook` (dynamic webhooks, JQL-filtered, **expiring after 30 days** unless refreshed via `PUT /rest/api/3/webhook/refresh`).
The payloads carry the issue plus a `changelog` for updates. Delivery is at-least-once with retries and **no ordering guarantee**, and events can be missed. Even webhook-based integrations therefore reconcile by polling.
A standalone CLI with no public endpoint cannot receive them anyway. `[UNVERIFIED]` This whole paragraph is from memory; the `/webhook` paths exist in the spec but were not re-read.

---

## 15. Jira Data Center / Server (REST v2) differences (brief)

`[UNVERIFIED]` This whole section is from memory; no DC docs were fetched.

- **Base and version**: `https://jira.example.com[/context]/rest/api/2/...`. There is no v3 on DC. The agile API is `/rest/agile/1.0` (the same shapes).
- **Auth**: Personal Access Tokens, `Authorization: Bearer <PAT>` (DC 8.14+). Basic auth uses username and password, and may be disabled by admins. OAuth 1.0a application links are legacy. There are no API tokens or email login.
- **Bodies**: `description`, `environment`, comment `body` and textarea fields are **wiki-markup strings** (`*bold*`, `_em_`, `{code:go}…{code}`, `h2.`, `* item`, `# item`, `[text|url]`), not ADF. Cloud's `/rest/api/2` also accepts and returns wiki strings, an option for Cloud too.
- **Users**: identified by `name` (username) and `key`. `accountId` does not exist. Set users with `{"name":"jdoe"}`. `/rest/api/2/user?username=`, `/user/search?username=` (DC 8.x; later versions use `query`). Email is generally visible.
- **Search**: the classic `GET/POST /rest/api/2/search` with `startAt`, `maxResults` (often capped at 1000 by `jira.search.views.default.max`), `total` and `validateQuery`. There is no `/search/jql` or `nextPageToken`. The index is near-synchronous on a single node, but DC clusters replicate the index asynchronously.
- **Parent**: Epic Link (`customfield_xxxxx`, a string key) and Epic Name are still the norm on DC. `parent` is only for sub-tasks. DC 9.x/10.x may add a unified parent; this is uncertain. Advanced Roadmaps uses the "Parent Link" custom field for levels above epic.
- **Create metadata**: the old `GET /rest/api/2/issue/createmeta?expand=projects.issuetypes.fields` was removed in DC 9.0, replaced by `/issue/createmeta/{projectIdOrKey}/issuetypes[/{issueTypeId}]`, the same as Cloud.
- **Timestamps**: the same `2024-01-01T10:00:00.000+0000` format. JQL date literals are the same, in the user's timezone.
- **Rate limiting**: an optional admin-configured limit (DC 8.6+), returning 429 with `Retry-After` and `X-RateLimit-*`.
- **Other**: `serverInfo.deploymentType` is `"Server"` or `"DataCenter"`. `/myself` has `name`, `key` and `emailAddress`. Statuses, transitions, links, comments paging and changelog (`/issue/{key}?expand=changelog`; the dedicated `/changelog` endpoint exists only on newer DC) are otherwise similar.

---

## 16. Implications for a sync tool (gotchas)

1. **Search is eventually consistent.** After our own write, a search may not reflect it for seconds to minutes.
   - Pass the ids we just wrote in `reconcileIssues` (max 50, numeric ids), or verify with `GET /issue/{id}`, which reads the database.
   - Overlap the `updated` watermark by several minutes and dedupe by `(id, updated)`.
2. **JQL time literals have minute granularity and use the caller's profile timezone.**
   - Fetch `/myself.timeZone`, convert the UTC watermark into it, truncate to the minute and use `>=`.
   - A DST fold can create a duplicated or missing hour, so overlap covers it.
   - `ORDER BY updated ASC, key ASC`, or `id ASC`, keeps the ordering stable. The orderBy maximum is 7 fields.
3. **`/search/jql` has no total and no offset.** Page with `nextPageToken` (it expires in 7 days) until it is absent.
   - The default `fields` is `id` only, so always pass fields.
   - `maxResults` is advisory, and pages can be short.
   - The JQL must be bounded (`project = X` suffices).
   - The old `/rest/api/3/search` is **removed** (CHANGE-2046). Do not fall back to it on Cloud.
4. **Keys are not stable; ids are.**
   - A move or project-key rename changes the key. GET with the old key silently returns the issue under the new key, with no redirect.
   - Alias on `id`, refresh `key` on each read, and record old keys as secondary aliases (`project.projectKeys` lists historical project keys).
5. **Parent unification.**
   - Read hierarchy only from `fields.parent` (sub-task→story and story→epic) and `issuetype.hierarchyLevel` (-1, 0, 1, ≥2).
   - Epic Link, Parent Link and the agile `epic` field are deprecated; JQL uses `parent =`.
   - In the changelog, look for the `IssueParentAssociation` item as well as legacy `Epic Link` / `Parent`.
   - To un-parent: `update.parent[{set:{none:true}}]`.
6. **The changelog does not contain comments.** Sync comments separately (paged `GET …/comment`, with `created`/`updated` per comment).
   - Deletions of comments, links and issues leave no tombstone. Detect them by set-diff.
   - The changelog `labels` items carry whole sets as space-joined strings.
   - Match changelog items on `fieldId` when present, else on `field` case-insensitively. `IssueParentAssociation` never has a `fieldId`. Sort histories yourself. [VETTED]
   - `/changelog` is oldest-first, while `expand=changelog` is newest-first and truncated.
   - `/changelog/bulkfetch` does up to 1000 issues or 10 fields per call, but its `created` format is uncertain.
7. **Custom field ids differ per site.**
   - Discover Sprint, Story Points, Rank and Start date via `/field` using `schema.custom`, never hard-code `customfield_100xx`.
   - Team-managed projects have project-scoped duplicates with the same name.
   - The board configuration tells which estimation field and rank field a board uses.
8. **Status cannot be set by edit.** Only a transition changes it, and the transition must be available from the current status.
   - Available transitions depend on the user, conditions and the current status.
   - The full workflow graph (`/workflows/search`) requires admin or *View workflow* permission, and `/workflow/search` was removed on 2026-06-01.
   - For normal users, learn edges from `GET /transitions` observations.
   - Transitions may require screen fields (resolution); use `expand=transitions.fields`.
   - Map semantics on `statusCategory.key` (`new`, `indeterminate`, `done`). The status category appears as an object in issue APIs but as an enum string (`TODO|IN_PROGRESS|DONE`) in `/statuses*` and `/workflows*`.
9. **createmeta**: the old `createmeta?expand=projects.issuetypes.fields` is deprecated (CHANGE-1304). Use the two paginated endpoints (`maxResults` ≤ 200). They reflect the create screen, so a field not on the screen is rejected at create.
   PUT edit no longer checks screens, but editmeta still reflects screen rules.
10. **ADF.** Description, environment, textarea fields and comments are ADF in v3.
    - Keep a lossless round trip. Unknown nodes (mentions, media, tables, panels) must be preserved, or edits from our side must be refused.
    - `codeBlock` text carries no marks. `code` combines only with `link`.
    - Single-line text custom fields are plain strings.
11. **Writes.**
    - Each PUT is one per-issue write. The per-issue limits are 20 per 2 s and 100 per 30 s, so batch all field changes for one issue into one PUT.
    - Set `notifyUsers=false` only if we have the permission; otherwise **the whole request fails** (400, "To discard the user notification either admin or project admin permissions are required.") [VETTED: it is not silently ignored].
    - There is no optimistic locking (409 exists only for concurrent internal conflicts). Implement 3-way merge client-side: Jira is canonical and wins a double edit.
    - Links: POST returns an empty 201, and the link id must be re-read from `fields.issuelinks`. A duplicate POST is a no-op. [VETTED] Direction: `inwardIssue` = source, `outwardIssue` = destination, so `{inwardIssue:A, outwardIssue:B, Blocks}` = **A blocks B** (§9.1). Never send a link comment.
12. **Permissions are hidden behind 404s.**
    - A 404 on GET issue can mean deleted, moved out of view, or lost permission; do not auto-delete locally on a single 404.
    - `/user/search` returns an empty list (not 403) without *Browse users*. `/user` returns 403.
    - Comment delete and update permission failures are 400.
13. **Users (GDPR).**
    - Key on `accountId`. `emailAddress` is often absent, so email-based identity adoption is best-effort.
    - `accountType` `app` or `customer` marks non-humans or JSM customers.
    - Deleted users show "Former user"-style display names with an `unknown` accountId.
14. **Sprints.**
    - The Sprint field is an array of sprint objects (current plus historical closed), each with an integer id.
    - Write a single integer sprint id (not an array); only one open or active sprint is allowed at a time.
    - Use the agile move-to-sprint and move-to-backlog endpoints for up to 50 issues.
    - Agile timestamps are RFC 3339; real values are `Z` [VETTED].
    - Kanban boards have no sprints.
15. **Rank.** Read only for ordering (a lexicographic LexoRank string). Write only via `PUT /rest/agile/1.0/issue/rank`, relative before or after, up to 50 per call. Handle a **207** partial failure, and retry 503 entries.
16. **Rate limits.** Honour `Retry-After` on 429 and on 5xx. Watch `X-RateLimit-NearLimit` to slow down proactively. Use backoff from 2 s base to 30 s cap with jitter. User-search has its own tenant-wide limit.
17. **Deletions and moves are invisible to polling by `updated`.** Periodically run a cheap full id scan (`project = X`, `fields=id`, largest pages) and diff the id set.
    An issue moved **out** of the project disappears from `project = X` but still exists. Check it with `GET /issue/{id}`, which returns the new key and project.
18. **Archived projects** are excluded from search unless `includeArchivedProjects=true`.
19. **The fake-server fidelity checklist** (what tests should exercise):
    - token paging with short pages
    - index lag with `reconcileIssues`
    - key-rename lookup
    - 404-for-permission
    - transitions availability, including a 400 on an invalid transition
    - per-issue 429 with `Retry-After`
    - ADF bodies
    - Sprint field as an array on read and an int on write
    - an empty 201 on link create
    - 207 on rank
    - changelog oldest-first paging with `isLast`
    - comments not in the changelog
    - [VETTED additions:]
      - link direction per §9.1
      - `IssueParentAssociation` without `fieldId`
      - custom changelog items without `fieldId`
      - shuffled `expand=changelog` histories
      - non-UTC timestamp offsets, with the site zone ≠ the user's JQL zone
      - `notifyUsers=false` rejected for non-admins
      - 410 on `/rest/api/3/search`
      - 409 on concurrent transitions
      - string-into-ADF rejected
      - deleted issues vanishing from search
      - issue properties (sync marker)
      - comment add bumping `updated` but edit/delete not
      - `nextPageToken` omitted with `isLast:true`

---

## 17. Vetting addenda [VETTED 2026-09-28]

The full evidence is in `jira-api-vetting.md` next to this file. The items below were missing from this reference:

1. **Issue properties as the Jira-side sync marker.**
   - `PUT /rest/api/3/issue/{id}/properties/{key}` (`setIssueProperty`) takes the body as the raw JSON value (non-empty, at most 32768 characters). It returns **201** when created, **200** when updated, then 400, 401, 403, 404.
   - `GET` returns `{"key","value"}`, and `DELETE` returns 204.
   - They can also be set inline via `IssueUpdateDetails.properties` on create or edit, and read via `properties=` (at most 5) on GET issue or `/search/jql`.
   - They are not JQL-searchable without an app index.
2. **Server clock.** Take `serverInfo.serverTime` or the HTTP `Date` header to measure skew before computing JQL watermarks.
3. **Our own echoes.** After each write, record the new `updated` (`returnIssue=true` on PUT, or a GET), so the next poll does not treat our write as a remote edit.
4. **ADF normalisation.** Compare canonicalised ADF or the derived Markdown, never bytes.
5. **Missing credentials run anonymously** and may yield 200 with empty results (search, user search). Probe `/myself` at start-up.
6. **Changelog ids** are numeric strings that increase in practice, but this is undocumented. Dedupe by id; do not use the max id as a watermark.
7. **`reconcileIssues`** must be the same list on every page of one search (spec).

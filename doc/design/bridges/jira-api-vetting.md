# Vetting of jira-api.md (Jira Cloud REST reference)

Vetted 2026-09-28 against `platform.json` / `agile.json` (the OpenAPI specs named in jira-api.md), plus web sources and the source and
fixtures of open-source clients (andygrunwald/go-jira,
ctreminiom/go-atlassian, pycontribs/jira, MrRefactoring/jira.js).

Spec check summary: every one of the 66 endpoint/method pairs named in jira-api.md exists in the spec
with the stated `operationId`, and every stated default and deprecation flag matches
(`v.py` output). The mistakes are in the semantics, the example payloads and the
`[UNVERIFIED]` claims, not in the endpoint list.

## 1. Corrections (must fix)

### C1. Issue-link direction in `POST /issueLink` is the opposite of what jira-api.md §9.1 said
jira-api.md claimed: `{"type":"Blocks","outwardIssue":A,"inwardIssue":B}` means **A blocks B**.
**Wrong.** It is: **`inwardIssue` is the source and `outwardIssue` the destination, and
"source <outward text> destination".** So `{"inwardIssue":{"key":"A"},"outwardIssue":{"key":"B"},"type":{"name":"Blocks"}}`
means **A blocks B**. Viewing A, `fields.issuelinks` holds `{"outwardIssue": B}`, labelled
"blocks". Viewing B, it holds `{"inwardIssue": A}`, labelled "is blocked by".
Mnemonic: in the POST, each issue goes under the key it will appear under **when viewed from the other issue**.
Evidence:
- Atlassian's own Java client (JRJC) `LinkIssuesInputGenerator.generate()`:
  `res.put("inwardIssue", …getFromIssueKey()); res.put("outwardIssue", …getToIssueKey());`,
  and for pre-5.0 servers `fromIssueKey`/`toIssueKey`
  (https://github.com/betaphreak/jira-rest-java-client/blob/master/core/src/main/java/com/atlassian/jira/rest/client/internal/json/gen/LinkIssuesInputGenerator.java).
- The Jira REST docs (4.4.1–7.x) for POST issueLink: "will create a link from the first issue to the second issue using the outward description. It also create a link from the second issue to the first issue using the inward description" (https://docs.atlassian.com/software/jira/docs/api/REST/4.4.1/). The first issue is the from/inward one.
- An empirical report against Jira Cloud: sending `outwardIssue: IQS-2, inwardIssue: IQS-4, type Blocks` made the UI show "IQS-2 is blocked by IQS-4", which means IQS-4 blocks IQS-2, consistent with the rule above (https://github.com/atlassian/atlassian-mcp-server/issues/112). The reporter expected the opposite and filed it as a bug.
- The read side matches https://developer.atlassian.com/cloud/jira/platform/issue-linking-model/: "if the issue link data contains an `inwardIssue` field, the link should be labeled with the value of the `type.inward` field".
- Caveat: the spec's linkIssues text says the optional comment is added "to the from (outward) issue", and that *Link issues* is needed "on the project containing the from (outward) issue". This contradicts JRJC's from=inward. Do not rely on link comments. The fake should add the comment to `outwardIssue`, as the spec says, and the client should never send one.
- Fake server: store `{source: inwardIssue, dest: outwardIssue}`. On GET of the source, emit `outwardIssue: dest`; on GET of the dest, emit `inwardIssue: source`.

### C2. `IssueParentAssociation` changelog items have **no `fieldId`** (jira-api.md §4.1, §16.6 said "match on `fieldId`")
The announcement's example is `{"field":"IssueParentAssociation","fieldtype":"jira","from":"1234","fromString":"ABC-1","to":"4567","toString":"ABC-2"}`, with no `fieldId`, and Atlassian replied: "IssueParentAssociation changelog items do not have `fieldId` … Consumers should not use `fieldId` for the IssueParentAssociation changelog items."
`from`/`to` are **parent issue ids**, and `fromString`/`toString` are parent **keys**.
(https://community.developer.atlassian.com/t/deprecation-of-fields-values-epic-link-and-parent-in-issue-history-changelogs/48993)
Rule: match on `fieldId` when it is present, else on `field`. The fake must emit this item without `fieldId`.

### C3. Sprint dates are UTC `Z`, not a colon offset (jira-api.md §3.1, §11, §13)
Real Sprint field values on issues look like `"startDate":"2025-03-12T01:36:46.600Z"`
(https://blog.mikebowler.ca/2026/01/29/jira-api-sprints/, a Jan 2026 capture from Jira Cloud).
The `+10:00` values are spec examples. Parse sprint dates with RFC 3339, which accepts both. The fake should emit `Z`.

### C4. Platform timestamps are **not** `+0000`; the offset is the viewing user's zone (jira-api.md §3.1, §13 "fake should emit +0000")
The go-jira fixture captured from a real Cloud site (`testing/mock-data/issues_in_sprint.json`, example.atlassian.net) has
`"created":"2015-12-02T07:39:15.000-0800"` and `"resolutiondate":"2015-12-07T14:19:13.000-0800"`.
The fake server should render timestamps in a **non-UTC** zone (e.g. the configured user's `timeZone`, `-0700`/`+0200`), so that a client which assumes `+0000` fails tests.
The format `2006-01-02T15:04:05.000-0700` is confirmed. That is the go-jira `Time` layout, and `Date` is `2006-01-02`.

### C5. `notifyUsers=false` without permission **fails the request**; it is not silently ignored (jira-api.md §5.3, §16.11)
The spec says only "If the user doesn't have the necessary permission the request is ignored", which is ambiguous.
In practice the whole edit is rejected with
`{"errorMessages":["To discard the user notification either admin or project admin permissions are required."],"errors":{}}`
(https://forum.uipath.com/t/receiving-error-while-updating-issue-in-jira-tickets-using-update-issue-activity-error-update-issue-response-content-errormessages-to-discard-the-user-notification-either-admin-or-project-admin-permissions-are-required-errors/543771, https://help.k15t.com/backbone-issue-sync/5.13/server/admin-or-project-admin-permissions-are-required, https://community.atlassian.com/forums/Jira-questions/Automation-error-quot-To-discard-the-user-notification-either/qaq-p/2928374).
The status code is not captured in those reports; assume 400, and treat 403 the same way.
Client: default to `notifyUsers` unset (true). If it is set to false and this message comes back, retry once without it and remember that for the rest of the run.
Fake server: reject `notifyUsers=false` with this body and 400 unless the configured user is a project admin.

### C6. Which zone the timestamps are rendered in (jira-api.md §3.1 and §13 said the requesting user's zone)
The REST v3 intro (https://developer.atlassian.com/cloud/jira/platform/rest/v3/intro/#timestamps): "By default, top-level timestamps (e.g. updated and created) are returned in ISO 8601 format, in the **system default user time zone**. To return date time data in the logged in user's timezone, please refer to renderedFields".
JQL date literals, however, are read in the **searching user's profile** zone (JQL fields doc: "relative to your configured time zone").
The two zones **can differ**. Parse the offset from the response, and format JQL literals in `/myself.timeZone`.
Fake server: make the site zone and the user zone different (e.g. site `America/Los_Angeles`, user `Europe/Berlin`), so that a client which mixes them up fails.

### C7. The old `/rest/api/{2,3}/search` now answers **410 Gone** (jira-api.md §2.2 did not say which status)
The body is `{"errorMessages":["The requested API has been removed. …"]}` (https://github.com/atlassian/atlassian-mcp-server/issues/70, observed 2026-02-20).
Timeline: removal was first set for 1 May 2025, then moved to 1 August 2025, and rolled out per site through about October 2025
(https://docs.adaptavist.com/sr4jc/latest/release-notes/breaking-changes/atlassian-rest-api-search-endpoints-deprecation/,
https://community.atlassian.com/forums/Jira-questions/Sunset-schedule-for-Issue-API-CHANGE-2046/qaq-p/3105876).
Fake server: return 410 with that body on `/rest/api/3/search`, so that an accidental use fails loudly.

### C8. Changelog: `fieldId` is often absent, and `histories` order is not guaranteed (jira-api.md §4.1)
- `fieldId` is absent on IssueParentAssociation (C2) and on many custom-field items, e.g. `{"field":"Epic Child","fieldtype":"custom","from":null,"fromString":null,"to":"10048","toString":"SP-41"}`.
  The same source says of `histories`: "You can NOT assume that the items in `histories` will be returned in any particular order" (https://blog.mikebowler.ca/2024/04/09/jira-issue-history/).
  Rule: key an item on `fieldId` if present, else on `field`, **compared case-insensitively** ("there is no consistency in case sensitivity", same source).
  Sort histories by `(created, id numeric)` yourself.
- Sprint items are confirmed: `{"field":"Sprint","fieldtype":"custom","fieldId":"customfield_10020","from":"76, 79","fromString":"Sprint 12, Sprint 13","to":"76, 80","toString":"Sprint 12, Sprint 14"}`.
  The ids are **comma-space** separated. Sprint names may contain commas, so parse `from`/`to`, never the strings (https://blog.mikebowler.ca/2026/01/29/jira-api-sprints/).
- Fake server: omit `fieldId` on custom-field items at random, emit IssueParentAssociation without `fieldId`, and return `expand=changelog` histories in shuffled order.

### C9. Concurrent transitions answer 409, not 400 (jira-api.md §6.2 listed 409 without its meaning)
"The Jira Cloud API does not support simultaneous issue transitions on an issue … we will soon be replacing the 400 (Bad Request) status code with 409 (Conflict)", rolled out no sooner than 4 May 2020
(https://developer.atlassian.com/cloud/jira/platform/change-notice-update-in-simultaneous-transitions-issue-api/).
Client: on 409, re-read `GET …/transitions` and retry, because the status may already have moved.

### C10. "Story point estimate" is not always `jsw-story-points` (jira-api.md §3.1 and §8.1 tables)
Both type keys are seen in the wild for story-point fields:
`com.pyxis.greenhopper.jira:jsw-story-points`, and `com.atlassian.jira.plugin.system.customfieldtypes:float` with the name "Story point estimate" or "Story Points"
(https://github.com/stablyai/orca/pull/21044 review; community results; https://jira.atlassian.com/browse/JSWCLOUD-26668 says no reliable way exists to identify the Story Points field on Cloud).
Resolution order for the client:
1. the board configuration's `estimation.field.fieldId`;
2. else a field with `schema.custom == jsw-story-points`;
3. else a `float` field whose `untranslatedName` (else `name`) is exactly "Story Points" or "Story point estimate";
4. else ask the user.

### C11. Link comment target and permission wording contradict each other in the spec (jira-api.md §9.1)
See C1. The client must **not** send `comment` on POST /issueLink.
The fake may accept it, and should put the comment on the `outwardIssue`, as the spec literally says.

### C12. Old-search params (jira-api.md §2.2): harmless, but superseded by C7
There is nothing to implement.

### C13. Smaller spec-level fixes
- The jira-api.md §7.2 bulk comment endpoint is confirmed (it was flagged unverified). `POST /rest/api/3/comment/list` (`getCommentsByIds`) takes the body `{"ids":[int64…]}`, with **at most 1000** ids and an optional `expand` query. It returns a **`PageBeanComment`** `{self,nextPage,maxResults,startAt,total,isLast,values[Comment]}`, and 400 on a bad request.
- The jira-api.md §8.11 resolution search is confirmed: `GET /rest/api/3/resolution/search` (`searchResolutions`) has `startAt` and `maxResults` as **strings**, defaults "0" and "50", plus `id[]` and `onlyDefault`. The response is `PageBeanResolutionJsonBean`.
- For §8.2, the `projectKeys` **expand option exists**, but the `Project` schema has no `projectKeys` property. The response key name is therefore unverified; parse it leniently, and do not depend on it.
- In §8.1, `untranslatedName` is not in the `FieldDetails` schema, only in practice. It is optional.
- In §6.1, `isLooped` is confirmed as the wire name: go-atlassian models it as `json:"isLooped"` (`pkg/infra/models/jira_issue_v3.go:149`). The spec schema calls it `looped`. Ignore both.
- In §9.2, `DELETE /issueLink/{id}` lists both 200 and 204. The client should accept any 2xx.
- In §11.3, "an issue can only be in one active or future sprint at a time, and only the active/future sprint can edited" is verbatim from the agile spec.

## 2. Resolved unverified items

| # | jira-api.md claim | verdict | evidence |
|---|---|---|---|
| R1 | Link direction (§9.1) | **Wrong; corrected in C1** | JRJC source, the 4.4 REST docs, and the MCP #112 report |
| R2 | Status categories: 1 `undefined` "No Category" medium-gray; 2 `new` "To Do" blue-gray; 3 `done` "Done" green; 4 `indeterminate` "In Progress" yellow | **Correct** | A real capture in go-jira `testing/mock-data/all_statuscategories.json` (issues.apache.org) has exactly these ids, keys, names and colours. Also https://docs.atlassian.com/DAC/javadoc/jira/reference/com/atlassian/jira/issue/status/category/StatusCategory.html |
| R3 | `fields.comment` on GET issue is a page object `{comments[],self,maxResults,total,startAt}` | **Correct** (the spec example's array is bogus) | go-jira `type Comments struct{Comments []*Comment "json:comments"}`; go-atlassian `IssueCommentPageScheme{startAt,maxResults,total,comments}` on `fields.comment` (`jira_issue_v3.go:131`); go-jira test fixtures `"comment":{"comments":[…]}` |
| R4 | `fields.parent` = `{id,key,self,fields{summary,status,priority,issuetype}}` | **Correct**; `fields` may also carry `assignee` | go-atlassian `ParentScheme`/`ParentFieldsScheme` (`jira_issue_v2.go:139-153`). With no parent the key is absent or null: go-jira's fixture has `"parent": null`, so accept both |
| R5 | `emailAddress` usually absent rather than null | **Both happen; treat absent, null and "" alike** | The spec says "may be returned as null"; the User description says deleted users have "email is blank" |
| R6 | Sprint `schema` = `{"type":"array","items":"json","custom":"com.pyxis.greenhopper.jira:gh-sprint"}` | **Correct** | community thread qaq-p/1570350; pycontribs and jira.js reference `gh-sprint` |
| R7 | Rank type key `com.pyxis.greenhopper.jira:gh-lexo-rank` | **Correct** | pycontribs `client.py:5869` finds the rank field by exactly this key |
| R8 | LexoRank string format | **Correct in shape**: `<bucket 0-2>|<base-36 value>:<suffix>`, e.g. real `"0|0zzzzd:vi"` (go-jira `issues_in_sprint.json`), `"0|i0003r:"` | Compare as plain byte strings. Never generate one; write only via `PUT /agile/1.0/issue/rank` |
| R9 | Epic Link key `com.pyxis.greenhopper.jira:gh-epic-link` | **Correct** | go-jira and go-atlassian fixtures (grep in `../src`); value is a key string like `"AR-37"` (go-jira fixture `customfield_10700`) |
| R10 | Story point field type keys | **Partly wrong; see C10** | |
| R11 | Changelog field names for parent, sprint and fieldId | **Resolved; see C2 and C8** | |
| R12 | `/search/jql` deprecation timeline | **Resolved; see C7** | |
| R13 | `nextPageToken` absent vs null on the last page | **Both are in the spec text**: the param doc says "not included", the schema says "will be null". `isLast` is in the schema | Client: stop when `isLast==true` **or** the token is missing, null or "". Fake: omit the token on the last page and set `isLast:true` |
| R14 | JQL literals in the user's profile zone | **Correct** | JQL doc ("your configured time zone"); multiple integrator reports (e.g. github.com/dzianisv/CodeBridge PR #21) |
| R15 | Timestamp format `2006-01-02T15:04:05.000-0700`; `duedate` `2006-01-02` | **Correct**; the zone rule is in C6 | go-jira `Time`/`Date` types, and the fixture `"2015-12-02T07:39:15.000-0800"` |
| R16 | Transition to an unavailable id gives 400 `{"errorMessages":["Transition id 'X' is not valid for this issue."]}` | **Correct** (text confirmed by several community reports) | https://community.atlassian.com/forums/Jira-questions/Rest-API-says-Transition-id-is-not-valid-for-this-issue-but-I/qaq-p/2158194. A missing required screen field is also 400 with `errors:{"resolution":"…"}` |
| R17 | ADF: a plain `doc>paragraph>text` is accepted | **Correct** (it is the spec's own example body). A **string** sent to a v3 ADF field gives 400 `errors:{"description":"Operation value must be an Atlassian Document (see the Atlassian Document Format)"}` | community qaq-p/1304733 and qaq-p/1977160. Jira returns the stored ADF, possibly with added `attrs` (e.g. `localId`) and a normalised structure; compare semantically |
| R18 | Sprint written as a number | **Correct**: `"customfield_10020": 37`. An array fails with an error demanding a number | community qaq-p/1570350; software REST intro |
| R19 | 429 headers | **Correct**; the doc example is `Retry-After: 1`, `X-RateLimit-Limit: 350`, `X-RateLimit-Remaining: 0`, `X-RateLimit-Reset: 2026-01-01T01:01:01Z`, `RateLimit-Reason: jira-burst-based`, `Content-Type: application/json`. Legacy `X-Beta-RateLimit-*` informational headers also exist | rate-limiting page (re-fetched 2026-09-28) |
| R20 | Passwords not accepted for Basic auth | **Correct**: "Authentication using passwords has been deprecated." | basic-auth page (re-fetched) |
| R21 | `X-Force-Accept-Language` and `X-Atlassian-Token: no-check` | **Correct** | v3 intro "Special headers". The intro also documents the response header **`X-AAccountId`** (the caller's accountId). `X-AREQUESTID` is still not documented |
| R22 | OAuth base `https://api.atlassian.com/ex/jira/{cloudId}` | **Correct** | v3 intro |

## 3. Still unverified, with the robust assumption

Each entry gives the choice that works whichever way the truth falls. The fake should implement the **harder** side where the note says so.

| item | robust client behaviour | fake-server behaviour |
|---|---|---|
| GET `/issue/{id}` read-after-write consistency (§2.4) | Believe it, but after our own write verify via GET, and also pass `reconcileIssues` on the next search | GET is consistent; search lags (configurable delay) unless the id is in `reconcileIssues` |
| Whether comment add, edit or delete bumps `issue.updated` (§7.1). Jira's `CommentManager.create(..., modifyIssueUpdateDate)` exists and REST adds very likely set it, but edits and deletes are unconfirmed | Do not rely on it: re-sync comments on every issue whose `updated` moved, **and** run a periodic full comment reconcile (`comment/list` or per-issue paging) | A switch; default: add bumps, edit and delete do not (the harder case) |
| `bulkfetch` changelog `created`: an epoch in the example, date-time in the schema | Accept a JSON number (epoch seconds, or ms if > 1e11) or a string | Emit the string form (as the schema says); a unit test covers the number |
| `bulkfetch` `maxResults` default and maximum | Send nothing or ≤ 1000, and page on the token | Cap at 100 per page to force paging |
| `expand=changelog` capped at 100 histories | Never use it for sync; use `/issue/{id}/changelog` | Cap at 100, in shuffled order (C8) |
| Link changelog items (`field:"Link"`, `to`=key, `toString:"This issue blocks PROJ-20"`), with no `fieldId` | Treat any `Link` item only as a *hint* to re-read `fields.issuelinks`, and diff link ids | Emit such items without `fieldId` |
| Rank changelog (`field:"Rank"`, `toString:"Ranked higher"`) | Hint only; read the rank field value | Emit it |
| A move shows as `Key` / `project` changelog items | Detect a move by `response.key != stored key` or `fields.project.id` change, not by changelog | Emit `Key` and `project` items on a move |
| `emailAddress` absent vs null vs "" | Treat all three as unknown | Omit it for users with hidden email |
| Comment `parentId` (threaded comments) | Ignore unknown keys; do not reply-thread | Never emit it |
| `summary` max 255 characters | Truncate or refuse locally at 255 runes | 400 `errors.summary` above 255 |
| Labels cannot contain spaces | Replace spaces with `-` or refuse locally | 400 `errors.labels` on a space |
| `issuetype:{name}` accepted on create | Always send `{id}` | Accept `id` only |
| User fields accept `{accountId}` as well as `{id}` | Send `{"accountId": …}`. It is the widely used form, and the spec's create example shows `{"id": …}`, so both work in practice | Accept both; a client test checks it sends `accountId` |
| Clearing with `null` (`duedate`, `assignee`, custom number, **Sprint**) | Send `null`. For Sprint, prefer `POST /agile/1.0/backlog/issue` | Accept `null` for all |
| DC `deploymentType` "Server"/"DataCenter" | Anything other than "Cloud" means not Cloud | Always "Cloud" |
| `projectKeys` expand response shape | Optional | Emit `projectKeys: []string` |
| Scoped API tokens need the `api.atlassian.com/ex/jira/{cloudId}` base; `cloudId` from `https://<site>/_edge/tenant_info` | Make the base URL configurable; never derive it | Serve under any base |
| API tokens expire | Surface 401 clearly: "token invalid or expired" | 401 with an empty or ErrorCollection body |
| `/workflow/search` removed after 2026-06-01 | Do not call it | 404 or 410 |
| Old createmeta removal | Do not call it | Not implemented (404) |
| Kanban `board/{id}/sprint` gives 400 | Treat 400 there as "no sprints" | 400 `{"errorMessages":["The board does not support sprints"]}` |
| rank: both `rankBeforeIssue` and `rankAfterIssue` | Send exactly one | 400 if both |
| Issue-property writes do not bump `updated` and are not in the changelog | Do not use property writes as change signals | Do not bump |
| `Epic Link` still returned on some sites | Read `parent` only | Do not emit Epic Link |

## 4. Missing items (a sync tool needs these; jira-api.md did not cover them)

1. **Issue entity properties as the Jira-side sync marker.**
   - `PUT /rest/api/3/issue/{issueIdOrKey}/properties/{propertyKey}` (`setIssueProperty`) takes the body as the raw JSON value, "valid, non-empty JSON blob. The maximum length is 32768 characters".
     It returns **201 when created, 200 when updated**, 400, 401, 403 (no *Edit issues*), and 404.
   - `GET …/properties/{key}` (`getIssueProperty`) returns 200 `{"key":"…","value":{…}}`, or 404.
   - `DELETE` returns 204.
   - They can also be set inline on create or edit via `"properties":[{"key","value"}]` (`IssueUpdateDetails.properties`), and read in bulk with `properties=<key>` on GET issue or `/search/jql` (at most 5 keys).
   - Use: stash `{"gitwork":{"id":"<entity id>","rev":<lamport>}}` so that a re-clone can re-link issues without a local mapping file.
   - They are JQL-searchable only if an app declares them as indexed, so do not plan on JQL over them.
   - Fake: implement both property endpoints plus the inline form and `properties=` on reads.
2. **`fields=*all` vs `*navigable` on `/search/jql`.** The default is `id` only. `*navigable` excludes non-navigable fields; `*all` includes `comment` (a truncated page object) and heavy fields.
   Request an explicit list, which the page-size note also favours. The same `fields` list with `-comment`/`-description` excludes those fields.
3. **`expand=versionedRepresentations` ignores `fields`** (spec, getIssue expand). Never combine it with a field list and expect filtering.
4. **Server clock.** Use `GET /serverInfo` → `serverTime` (same format as issue timestamps) or the HTTP `Date` header (RFC 1123) to measure clock skew before computing a JQL watermark.
   The fake should run on an injectable clock and emit `Date`.
5. **Deleted issues vanish.** There is no tombstone in search or changelog: GET gives 404 and search omits the issue. Detect deletions by the full id-set diff (jira-api.md §16.17 has it). The **fake must actually drop deleted issues from search**.
6. **Changelog ids.** They are numeric strings, and increase site-wide in practice, but that is not documented. Order by `created` then numeric `id`, and dedupe by `id`. Do not use "max id seen" as a watermark.
7. **Moves.** A moved issue keeps its `id` and gets a new `key` and project. Search by old key works (`key = OLD-1`), and GET by old key returns the new key (spec).
8. **`reconcileIssues` takes numeric ids.** It must be the same list on every page of one search (spec). The fake should reject a list that differs between pages with 400.
9. **`maxResults` for `/search/jql` is capped at 5000, and is "advisory".** The fake should return short pages (e.g. min(requested, 37)) so the client never relies on a full page.
10. **410** on removed endpoints (C7), and (from general Jira behaviour, `[UNVERIFIED]`) **415** when `Content-Type` is missing on a POST/PUT with a body. Send `Content-Type: application/json` always.
11. **`X-AAccountId` response header** gives the caller's accountId on every response (v3 intro), a cheap identity check.
12. **Description and comment round trip: Jira normalises ADF.** A read-back may differ byte-wise from what was written (e.g. added `attrs`, `localId`, merged text nodes). Compare canonicalised ADF (or the derived Markdown) when deciding "did it change", or every sync will echo writes back.
13. **`updated` changes on our own writes.** After each write, record the issue's new `updated` (from `returnIssue=true` on PUT, or a follow-up GET). The next poll then recognises our own echo and does not 3-way-merge it as a remote edit.
14. **Anonymous calls.** Many operations "can be accessed anonymously"; a **missing** `Authorization` header (e.g. a dropped env var) runs the call as anonymous and may give **200 with empty results**. Bad credentials give 401 (e.g. search, user search).
    At start-up, call `/myself` and fail if it is not 200, so that bad auth is not mistaken for "no issues".
15. **`deleteIssue` with sub-tasks** is 400 unless `deleteSubtasks=true`. The sync should never delete on Jira without an explicit user decision.

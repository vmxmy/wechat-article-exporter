# Browser `/api/v1` contract

Status date: 2026-07-24. This document is the canonical, testable `/api/v1`
contract for the local browser workspace (`cli/internal/web`). It supersedes
informal descriptions elsewhere; when this document and code disagree, treat
the disagreement as a bug in one of them and reconcile before release. Every
claim below is grounded in `cli/internal/web/api.go`,
`cli/internal/web/api_control.go`, `cli/internal/web/api_query.go`, and
`cli/internal/application/workspace.go` as read on the status date.

All routes require the local browser session established by the bootstrap
token exchange (`GET`) or, for mutations, the same-origin `Origin` header plus
CSRF proof (`POST`/`PATCH`/`DELETE`). Authentication and CSRF failures are
returned by the shared envelopes defined below before any route-specific logic
runs.

## 1. Versioning

- The literal string `"v1"` is the only version in use. It is carried in
  every success and error envelope as `apiVersion` and in every route path as
  the `/api/v1/` prefix.
- There is exactly one version in flight at a time. A new incompatible
  version is introduced as `/api/v2/...` with its own envelope set; `v1`
  keeps behaving exactly as documented here until it is formally retired (see
  §6). Routes are never silently moved between versions.

## 2. Standard envelopes

### 2.1 Success envelope (single resource)

```json
{ "apiVersion": "v1", "data": { "...": "resource fields" } }
```

Produced by `writeAPI` (`cli/internal/web/api.go`). For backward
compatibility with the existing browser client, the same top-level fields
that appear inside `data` are also duplicated at the top level of the JSON
object. New clients MUST read resource fields from `data`; the top-level
duplication is a deprecated compatibility shim (see §6.3) and MUST NOT gain
new fields that are not also present in `data`.

### 2.2 Success envelope (page)

```json
{
  "apiVersion": "v1",
  "data": [ "...page items..." ],
  "pagination": { "page": 1, "pageSize": 50, "total": 0 },
  "items": [ "...page items, duplicate of data..." ],
  "total": 0,
  "offset": 0,
  "limit": 50
}
```

Produced by `writePage` (`cli/internal/web/api.go`). `data` and `items` are
the same array; `pagination` is the numbered-page projection of the same
`offset`/`limit`/`total` fields present at the top level. New clients MUST
read `data`, `offset`, `limit`, and `total`. `items` and `pagination` are the
deprecated compatibility shim (see §6.3).

### 2.3 Error envelope

```json
{ "apiVersion": "v1", "error": { "code": "invalid_argument", "message": "..." } }
```

Produced by `apiError`/`apiErrorEnvelope` (`cli/internal/web/api.go`).
`message` is a stable, human-readable, English sentence. It is intentionally
generic for internal failures (never includes paths, SQL, or upstream
response bodies — see `TestReadAPIErrorModelDoesNotLeakApplicationFailures`)
and specific for caller-fixable failures (invalid query, missing
confirmation, unsupported parameter).

### 2.4 Job-creation envelope

Persistent-job-creating routes return the shared job DTO wrapped in the
single-resource envelope (§2.1), so `data.id` is the stable job ID, except
`POST /api/v1/exports/start`, which returns
`{ "apiVersion": "v1", "data": { "jobId": "<stable persistent job id>" } }`.
This asymmetry is intentional and existing (`api_exports.go:178`); it is not
additive-safe to change without a version bump (see §5).

## 3. Pagination semantics

Two mutually exclusive request styles are accepted per list route
(`cli/internal/web/api_query.go:parsePageValues`). Supplying parameters from
both styles in the same request is a `400 invalid_argument`.

| Style | Query parameters | Semantics |
| --- | --- | --- |
| Offset/limit | `offset`, `limit` | `offset` is a zero-based non-negative integer item offset (default `0`). `limit` is the page size (default `50`, see `WorkspaceDefaultPageLimit`). |
| Numbered page | `page`, `page_size` | `page` is a required positive integer, one-based. `page_size` defaults to `50` if omitted. Internally converted to `offset = (page-1) * page_size`. |

Shared rules, enforced identically across every paginated route
(`accounts`, `accounts/search`, `articles`, `albums`, `saved-queries`,
`jobs`, `articles/{id}/detail`, `articles/{id}/comments`, and
`articles/{id}/comments/{commentId}/replies`):

- Default page size (no pagination parameters at all): **50** items
  (`application.WorkspaceDefaultPageLimit`).
- Maximum page size: **100** items (`application.WorkspaceMaximumPageLimit`).
  Any `limit`/`page_size` above 100, or `page_size` of `0`, is a `400
  invalid_argument`.
- `total` in the response is the total number of matching items server-side,
  independent of the requested page; clients page against it, they never
  infer it from `len(data)`.
- A page request beyond the available data returns an empty `data`/`items`
  array with `200 OK`, not an error.
- `offset`/`limit` are always present in a page response and are the
  authoritative source of the current window; `pagination.page` is derived
  as `offset/limit + 1` and exists only for the deprecated compatibility
  shim.

## 4. Stable error code vocabulary

This is the exhaustive, closed set of `error.code` values every `/api/v1`
route can currently return, confirmed by direct route inspection:

| Code | HTTP status | Meaning |
| --- | --- | --- |
| `authentication_required` | 401 | No valid local browser session. |
| `forbidden` | 403 | Session valid but mutation CSRF/Origin protection failed, or the operation is not permitted in the current state. |
| `invalid_argument` | 400 | Malformed, unsupported, out-of-range, or self-contradictory request input (query, JSON body, or path segment). |
| `not_found` | 404 | The referenced resource (article, job, export, path segment) does not exist or is not addressable via this route shape. |
| `method_not_allowed` | 405 | The route exists but not for the requested HTTP method; response sets `Allow`. |
| `confirmation_required` | 400 | A destructive/scoped mutation was attempted without, or with an incorrect, exact typed confirmation string. |
| `rate_limited` | 429 | A rate-limited operation (currently only export verification) exceeded its bounded limit. |
| `unavailable` | 503 | The underlying capability/service was not wired for this server instance (fail-closed), or is temporarily unavailable. |
| `cancelled` | 408 | The operation's context was cancelled or timed out. |
| `internal` | 500 | An unclassified application/internal failure. The message is always the fixed, generic string; no cause detail is ever serialized. |

`error.code` is part of the wire contract: it is intended for programmatic
branching by clients and MUST NOT change meaning for an existing route within
`v1`. `error.message` is not part of the machine-readable contract and MAY be
reworded within `v1` as long as `error.code` and the HTTP status are
unchanged.

Adding a new code to the vocabulary (for a route that previously could not
produce it) is additive and allowed within `v1`. Repurposing an existing code
for a different condition on an existing route, or changing which HTTP status
a code maps to, is a breaking change (see §5).

## 5. Additive-only evolution within v1

The following changes are considered additive and MAY ship within `v1`
without a version bump:

- Adding a new route.
- Adding a new optional query parameter to an existing route, as long as
  omitting it preserves the exact previous request behavior.
- Adding a new field to a success `data` object or page item, as long as
  existing fields keep their name, type, and meaning.
- Adding a new `error.code` value for a condition a route could not
  previously report.
- Loosening a validation rule (accepting input that was previously rejected)
  as long as the newly accepted input has an unambiguous, documented
  interpretation.

The following changes are breaking and MUST NOT ship within `v1`; they
require a new version (`/api/v2`) per §6:

- Removing or renaming a field, route, or query parameter.
- Changing the type, unit, or meaning of an existing field or parameter.
- Changing an `error.code` a route can return for an existing condition, or
  changing the HTTP status associated with a code on an existing route.
- Changing default pagination values (`50`/`100`) or default sort order for
  an existing route.
- Tightening a validation rule (rejecting input the route previously
  accepted), except when closing a security defect, which follows the
  security-exception process in §6.4.
- Changing which envelope shape a route uses (single-resource vs. page vs.
  the `exports/start` `jobId` shape).

### 5.1 Client behavior for unknown additive fields

Because §5 allows additive field growth, every `v1` client (including the
embedded browser SPA and any external MCP/automation client) MUST:

- Ignore unrecognized fields in any `data`, page item, or envelope object
  rather than treating them as an error.
- Never perform exhaustive/closed-set deserialization (e.g. `additionalProperties:
  false` JSON Schema validation, or Go `json.Decoder.DisallowUnknownFields`)
  against `v1` **responses**. `DisallowUnknownFields` remains correct and is
  used for `v1` **request bodies** (`decodeControl`,
  `cli/internal/web/api_control.go`) — the additive-only guarantee only
  applies to what the server returns, not to what it accepts.
- Treat an unrecognized `error.code` as an unclassified failure (safe to
  surface `error.message`) rather than crashing or silently retrying.

## 6. Breaking-change, version-bump, and deprecation process

### 6.1 Proposing a breaking change

1. Open an OpenSpec change describing the incompatible behavior and its
   motivation; it must explicitly enumerate which §5 breaking-change
   categories apply.
2. Design the `v2` route/envelope alongside the existing `v1` behavior; `v1`
   handlers are not modified except to add the deprecation notice in §6.2.
3. Add contract tests (see the route matrix in
   `docs/release/browser-api-route-matrix.md`) for the new `v2` shape before
   removing any `v1` test coverage.

### 6.2 Deprecation and support policy

- A `v1` route or field being replaced by `v2` is marked deprecated in this
  document and continues to function unchanged; deprecation is documentation
  only, never a behavior change to `v1`.
- `v1` is supported for the lifetime of the local, single-tenant desktop
  product's current major release line. Because the browser workspace, its
  embedded SPA, and the Go server ship as one versioned binary with no
  independent client update channel, there is no cross-version compatibility
  window to manage: the SPA embedded in a given binary always matches that
  binary's `v1` (or `v2`) contract exactly.
- `v1` is only removed when a new major release drops it entirely; this is
  itself a breaking change requiring the process in §6.1 and explicit
  release notes.

### 6.3 Existing deprecated compatibility shims

Two `v1` shims exist today purely to avoid an unnecessary breaking change
while the embedded browser client is transitioned to read only `data`:

- Single-resource responses duplicate `data`'s fields at the top level
  (§2.1).
- Page responses duplicate `data` as `items` and `offset`/`limit`/`total` as
  `pagination.page`/`pagination.pageSize`/`pagination.total` (§2.2).

Both are covered by `TestReadAPIAdaptsExistingBrowserClientDTO`
(`cli/internal/web/api_test.go`). They MAY be removed in a `v2` envelope but
MUST NOT be removed from `v1`.

### 6.4 Security exception

A validation tightening that closes an active security defect (e.g. a newly
discovered injection, traversal, or resource-exhaustion vector) MAY ship
within `v1` outside the normal additive-only rule, because shipping the fix
is safer than waiting for a version bump. Any such exception MUST be called
out explicitly in the release notes as a `v1` behavior change, distinct from
ordinary additive evolution.

## 7. Mutation classification (routes)

`data` is the versioned response payload inside the standard envelope from
§2.1/§2.4.

| Classification | Routes | Response | Stable identity / request lifetime |
| --- | --- | --- | --- |
| Synchronous, bounded session-account switch | `POST /session/accounts/{id}/switch` | `200 OK` with the safe workspace-session DTO | Creates no browser job. The authenticated mutation requires the exact loopback Host before routing, then an authenticated local session, exact loopback `Origin`, and CSRF proof. Its `{id}` path input is limited to 1–128 ASCII letters, digits, `-`, or `_`; an invalid identifier is `400 invalid_argument` and a malformed route shape is `404 not_found`. The response excludes session credentials. |
| Synchronous, bounded mutation | Login polling/completion and logout; account, saved-query, directory, credential, proxy, preference, backup, integrity/GC, diagnostic-bundle, manifest/credential upload/import, and restore controls | `200 OK`, `201 Created`, or `204 No Content`, according to the resource operation | Returns the bounded resource/result/opaque capability, or no body. It does not create a browser job unless the underlying application operation explicitly does so. |
| Persistent job creation | `POST /accounts/{id}/sync`; `/ingest/url`; `/articles/download`; `/articles/metadata`; `/articles/comments`; `/articles/resources`; `/albums/{id}/traverse`; `/albums/traverse`; `/exports/start` | `202 Accepted` | The first eight routes return the shared persistent job DTO with its stable `id`; `POST /albums/traverse` accepts 1–50 unique stable local `albumIds`, order, and optional download intent, then queues one durable album operation. Export start returns `{ "jobId": "<stable persistent job id>" }`. The handler only queues shared application work and never waits for job execution. |
| Persistent job control | `POST /jobs/{id}/pause`; `/resume`; `/retry`; `/cancel` | `200 OK` | Returns the shared job DTO after the permitted state transition. Its `id` remains the route job ID, so clients continue observing the same persistent job. |

`GET /api/v1/session/accounts` is an authenticated read (`401` without the
local browser session) that returns `200 OK` with the safe switchable-account
DTO: only `id`, `name`, and optional `alias`, plus the availability state. It
does not expose the upstream account payload, session credentials, resource
locations, or local storage references. The focused
`TestSessionAccountSwitchingAPIUsesSafeWorkspaceDTOs`
(`cli/internal/web/api_test.go`) exercises the read authentication and safe
DTO redaction, the switch's successful `200` response without the session
secret, invalid path input, and rejected cross-origin mutation.

The handler tests cover every route in the two persistent-job rows, asserting
both its status and the exact fixture job ID. This makes the classification
evidence independent of completion timing and prevents a handler from
substituting an ephemeral request-local identifier.

See `docs/release/browser-api-route-matrix.md` for the per-route-family
mapping to the actual Go tests that cover auth, valid response, invalid
input/error, redaction, and persistent-job-ID behavior.

## 8. Stored article comments and replies

`GET /api/v1/articles/{articleId}/comments` reads only locally stored comment
records. `GET /api/v1/articles/{articleId}/comments/{commentId}/replies`
reads only locally stored replies for one locally stored comment. Neither
route contacts WeChat, resumes a download, accesses credentials, or exposes
raw request/provenance state.

Both identifiers are strict opaque values: 1–256 ASCII letters, digits, `.`,
`_`, or `-`. Invalid identifiers are `400 invalid_argument`; malformed route
shapes remain `404 not_found`. Both routes use the bounded pagination rules in
§3 and deterministic ascending stored timestamp then opaque-ID order. They
require the authenticated local browser session and validated loopback Host;
GET reads do not require Origin or CSRF proof.

The comments route uses the single-resource envelope (§2.1). It returns
`articleId`, a bounded `comments` page, and a `pendingReplies` count. Each
comment projection contains only opaque `id`, `authorName`, `content`,
`createdAt`, `likeCount`, `replyCount`, and `replyStatus` (`complete` or
`pending`). The replies route uses the standard page envelope (§2.2), with
only opaque `id`, `authorName`, `content`, `createdAt`, and `likeCount`.
Neither route returns database IDs, object/resource digests or URLs,
filesystem paths, credentials/cookies/tokens, fetch times, upstream request
metadata, error text, or continuation buffers.

# Browser API mutation contract

Status date: 2026-07-24. This is the `/api/v1` mutation classification for
the local browser workspace. Every mutation first requires the local session,
same-origin `Origin`, and CSRF proof. `data` is the versioned response payload
inside the standard `{ "apiVersion": "v1", "data": ... }` envelope.

| Classification | Routes | Response | Stable identity / request lifetime |
| --- | --- | --- | --- |
| Synchronous, bounded mutation | Login polling/completion and logout; account, saved-query, directory, credential, proxy, preference, backup, integrity/GC, diagnostic-bundle, manifest/credential upload/import, and restore controls | `200 OK`, `201 Created`, or `204 No Content`, according to the resource operation | Returns the bounded resource/result/opaque capability, or no body. It does not create a browser job unless the underlying application operation explicitly does so. |
| Persistent job creation | `POST /accounts/{id}/sync`; `/ingest/url`; `/articles/download`; `/articles/metadata`; `/articles/comments`; `/articles/resources`; `/albums/{id}/traverse`; `/albums/traverse`; `/exports/start` | `202 Accepted` | The first eight routes return the shared persistent job DTO with its stable `id`; `POST /albums/traverse` accepts 1–50 unique stable local `albumIds`, order, and optional download intent, then queues one durable album operation. Export start returns `{ "jobId": "<stable persistent job id>" }`. The handler only queues shared application work and never waits for job execution. |
| Persistent job control | `POST /jobs/{id}/pause`; `/resume`; `/retry`; `/cancel` | `200 OK` | Returns the shared job DTO after the permitted state transition. Its `id` remains the route job ID, so clients continue observing the same persistent job. |

The handler tests cover every route in the two persistent-job rows, asserting
both its status and the exact fixture job ID. This makes the classification
evidence independent of completion timing and prevents a handler from
substituting an ephemeral request-local identifier.

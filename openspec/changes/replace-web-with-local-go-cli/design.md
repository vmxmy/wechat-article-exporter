## Context

The repository currently contains three product tiers:

1. A Nuxt SPA that owns most user workflows, browser preferences, IndexedDB caching, article processing, downloads, exports, and visualization.
2. Nitro/Cloudflare endpoints that own WeChat login sessions, account/article discovery, selected direct downloads, D1 mirroring, and PDF rendering.
3. A Cloudflare Worker MCP/OAuth adapter plus a Go remote client that delegates normal commands through both online domains.

The target is not merely a richer command wrapper. It is a product consolidation in which one Go binary becomes the executable product, while Cobra, Bubble Tea, and stdio MCP become adapters over shared local application modules. The binary remains an online client of WeChat and optionally user-configured proxies, but no normal workflow depends on project-operated runtime services.

The current TypeScript implementation remains the behavioral reference during migration. In particular, the `samples/` corpus, parser/renderer scripts, Dexie schema, synchronization rules, download classifications, export options, and public MCP command surface define compatibility expectations. WeChat endpoints are unofficial and may change; protocol locality and fixture-driven verification are therefore primary design constraints.

Primary stakeholders are existing Web users with browser-local data, current remote CLI/MCP users, privacy-conscious local users, maintainers of parsing/export behavior, and release operators currently responsible for Cloudflare deployments.

## Goals / Non-Goals

**Goals:**

- Produce one cross-platform local-first binary for macOS, Linux, and Windows.
- Preserve the useful account, article, album, download, metrics, comments, filtering, export, automation, and MCP capabilities of the current product.
- Remove all required runtime dependence on `mp.ziikoo.app`, `mptext.ziikoo.app`, Cloudflare KV, and Cloudflare D1.
- Make local data durable, queryable, migratable, auditable, and recoverable.
- Keep secrets local and prevent credential-bearing requests from traversing untrusted proxies.
- Concentrate business behavior behind small shared module interfaces used by Cobra, Bubble Tea, and MCP.
- Support a staged migration with objective parity gates and rollback.

**Non-Goals:**

- Maintaining a browser UI after the retirement gate.
- Providing hosted multi-tenant storage, hosted OAuth, remote MCP, or account pooling.
- Circumventing WeChat restrictions, CAPTCHA, paywalls, authorization, or platform rate controls.
- Guaranteeing that unofficial WeChat endpoints never change.
- Building a built-in full browser engine solely for PDF or HTML preview.
- Automatically extracting arbitrary IndexedDB contents from a user's browser profile without an explicit legacy export.
- Uploading legacy data, article content, sessions, or credentials to facilitate migration.

## Decisions

### 1. One core, three adapters

The application is organized around deep modules with these public roles:

```text
Cobra commands ──────┐
Bubble Tea workspace ├── Application ── domain modules ── adapters
stdio MCP tools ─────┘
```

The `Application` interface exposes task-oriented operations such as login, synchronize account, query articles, start download, start export, inspect job, backup, and restore. Cobra, Bubble Tea, and MCP translate input/output only; they do not implement protocol, persistence, download, parsing, or export rules.

Alternatives considered:

- Putting business behavior directly in Cobra commands would be quick but would recreate the current duplication and make TUI/MCP parity difficult.
- Treating MCP as the internal application interface would force local commands through a protocol-shaped, schema-heavy seam and make long-running jobs awkward.

### 2. Domain module layout

The Go module will be reorganized around the following internal modules:

| Module | Responsibility |
| --- | --- |
| `application` | Use cases, transaction/job orchestration, result types, policy checks |
| `wechat` | QR login, cookie jar, Official Account endpoints, response normalization |
| `library` | SQLite schema, repositories, queries, migrations, backup/import |
| `objects` | Content-addressed files, hashing, deduplication, integrity and GC |
| `jobs` | Persistent state machine, leases, scheduler, checkpoints, cancellation |
| `network` | Direct transport, route policy, proxy adapters, retries, redaction |
| `processor` | HTML validation, CGI-data parsing, normalized article model, rendering |
| `exporter` | Format adapters, manifests, atomic output, browser PDF integration |
| `secrets` | OS keychain and encrypted-vault adapters |
| `tui` | Bubble Tea views and presentation state only |
| `mcp` | stdio protocol and tool schemas only |

Internal seams are introduced only where two real adapters exist: secret store (OS keychain and encrypted vault), network route (direct and proxy), PDF renderer (local Chromium discovery variants), and clock/filesystem/process hooks needed for deterministic tests.

### 3. SQLite plus content-addressed object storage

SQLite is the metadata authority. Large HTML and binary resources are stored under an object root keyed by SHA-256. SQLite tracks ownership and references rather than storing large blobs.

This preserves transactional metadata, enables efficient local filters that replace AG Grid behavior, prevents duplicate images/styles, and keeps backup/integrity semantics explicit. SQLite runs in WAL mode where supported, with foreign keys enabled, bounded busy timeouts, and one migration coordinator.

Objects are written to a temporary file, hashed while streaming, fsynced/closed, and atomically renamed. An object becomes visible to callers only after the metadata transaction references a successfully committed object.

Alternatives considered:

- Storing every blob in SQLite simplifies backups but increases write amplification and makes large resource workloads and recovery less manageable.
- Recreating Dexie-like JSON files would lose robust querying, transactions, and migrations.

### 4. Explicit persistent job state machine

Long-running work uses a persistent job model rather than command-local goroutines. The common state machine is:

```text
queued → running → completed
                 ↘ partial
                 ↘ failed
                 ↘ cancelled
                 ↘ blocked_auth
                 ↘ paused
```

Items have independent states and attempt counters. Jobs acquire an execution lease so multiple processes can observe jobs while only one executor mutates a particular job. A process restart turns stale `running` items into resumable pending states according to operation-specific recovery rules.

This is required for TUI reattachment, MCP job IDs, crash recovery, and batch scale. Small read-only commands bypass jobs.

### 5. Direct-first, policy-controlled networking

The default route is local process to WeChat. CORS is irrelevant to a native Go client. Proxy use is explicit and classified by request type:

- Public article/resource requests can use eligible non-credential routes.
- Requests carrying cookies, pass tickets, keys, app message tokens, paid-content authorization, metrics, or comments can use only direct transport or a proxy explicitly trusted for credentials.
- Redirect destinations are validated at every hop.

The first implementation supports the existing URL-wrapper proxy contract for compatibility, behind a `Route` interface. Standard HTTP CONNECT/SOCKS support can be added if users need it, without changing callers.

Project-maintained public proxies are not embedded as trusted defaults. This intentionally changes current Web behavior in favor of local privacy.

### 6. Local WeChat session instead of server auth keys

QR login moves into the `wechat` module. It owns an `http.Client` with a persisted cookie jar and captures the WeChat management token. The local profile identity replaces `auth_key`, API token, Worker OAuth access token, and refresh token.

Session validity is checked before authenticated operations and when WeChat returns known expiration responses. Local logout clears local secrets even if best-effort upstream logout fails.

The remote OAuth packages remain temporarily only as a legacy migration/detection adapter and are not used by the local application path.

### 7. Secrets use OS credential storage with encrypted fallback

Secrets are addressed by profile and secret type. Preferred adapters are macOS Keychain, Windows Credential Manager, and Linux Secret Service. Because Linux headless systems frequently lack a usable credential service, the fallback is an explicitly initialized encrypted vault rather than plaintext `0600` JSON.

The vault derives a key from a user secret using a memory-hard KDF and stores authenticated ciphertext. Unlock behavior is explicit for interactive and automation contexts. Secrets are never included in default backups or diagnostic bundles.

### 8. Parser migration is behavior-first, not line-for-line

The Go processor defines a normalized article model independent of browser DOM types. It parses HTML without executing scripts, extracts supported embedded payloads with bounded decoders, classifies article state, discovers resources, and renders target representations.

Migration proceeds fixture-first:

1. Record approved semantic outputs from current TypeScript behavior for every fixture category.
2. Implement Go extraction and normalization.
3. Compare normalized model, text, key HTML structure, resource list, and special message types.
4. Retain TypeScript scripts only until the Go corpus gate passes.

Exact byte-for-byte HTML is not the universal compatibility target because serializers differ. Structural invariants and visual checks on a curated subset are the target.

### 9. Exporters consume one normalized model

All formats consume normalized article data and local resources, not raw endpoint responses. Shared selection, naming, manifest, overwrite, and atomic-write behavior lives above format adapters.

- HTML uses normalized safe documents and local relative assets.
- Markdown/text use shared semantic rendering.
- JSON has an explicit versioned schema.
- Excel uses streaming workbook writing where available.
- DOCX is generated as Open Packaging Convention/OpenXML, with structural validation.
- PDF uses a locally installed Chromium-family browser and self-contained local HTML. No remote rendering fallback is permitted.

The browser requirement for PDF is accepted because embedding a browser would make artifacts very large and pure-Go CSS layout would not preserve existing fidelity.

### 10. Bubble Tea is a workspace, not a Web clone

The TUI reproduces workflows, not AG Grid or browser layout. It has top-level areas for Home, Accounts, Articles, Albums, Jobs, Exports, Settings, Storage, and Diagnostics. Query and selection state is explicit and serializable. Tables page against SQLite rather than loading the entire library into memory.

Safe text/Markdown previews render in the terminal. Rich HTML preview is generated locally and opened in the user's browser only on request.

### 11. Local stdio MCP, asynchronous jobs

The MCP server runs over stdio and never binds a TCP port by default. Read operations return bounded results. Mutating or long-running tools create jobs and return job IDs. This avoids protocol timeouts and aligns MCP with TUI/Cobra job behavior.

Remote OAuth is removed because the client launches a local process under the user's OS account. Optional MCP read-only and tool allow/deny policies limit delegated authority.

### 12. Migration and retirement are gated

Nuxt/Nitro/MCP Worker code is retained while the local engine is built. The parity matrix is executable and maps each mandatory Web capability to an acceptance test or documented manual validation. Only after a stable compatibility release may removal begin.

Legacy browser migration uses an explicit versioned export archive. The CLI cannot safely or portably scrape arbitrary browser IndexedDB profiles. Legacy remote CLI OAuth tokens are preserved for rollback but deliberately not imported as local WeChat credentials.

### 13. Release and compatibility policy

Native release archives remain the delivery mechanism. Release CI additionally runs:

- unit, integration, race, and static checks;
- parser/export fixture corpus;
- database create/upgrade/backup/restore matrices;
- cross-platform builds and smoke tests;
- artifact checksums and software bill of materials;
- upgrade tests from every supported database compatibility baseline.

The database compatibility window and minimum supported version are release policy, not an accidental property of migrations.

## Data Model

The initial logical schema contains these groups; physical table names can evolve through migrations:

- Profiles and preferences
- Accounts and account synchronization cursors
- Articles and article-album relationships
- HTML/content versions and status history
- Engagement metric snapshots
- Comments and replies
- Resource objects and article-resource mappings
- Credentials metadata and secret references
- Network routes and health samples
- Jobs, job items, attempts, checkpoints, and logs
- Export manifests and output files
- Debug captures and protocol compatibility incidents
- Schema migrations and application metadata

Stable article identity is based on normalized WeChat identifiers when available, with canonical URL uniqueness as a fallback. Imported provisional single articles can later merge into the stable account/article identity without changing user-visible history.

## Security Model

- Local profile access follows OS account and filesystem permissions.
- Secrets are stored outside SQLite and referenced by opaque IDs.
- Sensitive networking is direct or explicitly trusted.
- Input URLs and every redirect are host/scheme validated.
- HTML is untrusted data; parsing does not execute scripts and local preview strips active content.
- Exports prevent path traversal and use atomic commits.
- Debug output is redacted before persistence, not only at presentation time.
- MCP defaults to local stdio and supports read-only/tool-policy restrictions.
- Destructive and credential-trust actions require exact, scoped confirmation.

## Testing Strategy

The highest shared seam is the `Application` interface. Acceptance tests drive use cases through it with fake WeChat, clock, secret store, filesystem, and route adapters. Adapter-specific integration tests cover SQLite, objects, keychains/vault, local Chromium, Cobra, Bubble Tea state transitions, and stdio MCP framing.

Major suites:

- WeChat protocol fixtures for login, account search, article pages, albums, comments, and expiration.
- Parser/renderer golden corpus derived from `samples/` and sanitized captures.
- Job crash/resume, lease, cancellation, retry, and idempotency tests.
- SQLite migration, corruption, backup/restore, and legacy import tests.
- Export structural tests plus visual HTML/PDF regression on a curated corpus.
- Secret redaction and SSRF/redirect policy tests.
- Cross-adapter contract tests proving Cobra, TUI, and MCP share query/use-case semantics.
- End-to-end smoke flows against a controlled test account only when explicitly configured; CI must not require live credentials.

## Risks / Trade-offs

- **[Unofficial WeChat endpoints change]** → Isolate protocol knowledge in `wechat`, retain redacted fixtures, classify compatibility failures, and avoid spreading response shapes into UI/export code.
- **[Go renderer initially differs from browser behavior]** → Use a semantic golden corpus, curated visual comparisons, and keep the legacy implementation until parity is approved.
- **[PDF requires an external browser]** → Provide deterministic discovery, clear installation guidance, pinned supported ranges where needed, and no remote fallback.
- **[Credential-store behavior differs by platform/headless environment]** → Test each adapter, expose diagnostics, and provide an explicit encrypted-vault fallback.
- **[SQLite plus object store can diverge after crashes]** → Use write-before-reference ordering, atomic rename, integrity checks, repair commands, and delayed garbage collection.
- **[Local direct WeChat access may be blocked for some users]** → Support explicit trusted proxies and clear route diagnostics; do not silently send credentials through public routes.
- **[Feature parity creates a large migration]** → Deliver vertical slices behind the shared application seam and keep the Web compatibility release available until the executable matrix passes.
- **[Terminal UI cannot match every Web visualization]** → Preserve workflows and query power, provide compact tables and safe previews, and use local browser handoff for rich HTML when needed.
- **[Multiple CLI processes race on jobs or migrations]** → Use database locks/leases, one migration coordinator, and observable read-only attachment.
- **[Legacy browser data is hard to extract]** → Provide a final Web exporter and versioned package before retirement; document manual migration clearly.

## Migration Plan

### Phase 0: Specification and baseline

- Freeze the capability matrix and approved fixture corpus.
- Record current Web behavior and export outputs with sanitized test data.
- Introduce the OpenSpec artifacts and release compatibility policy.

### Phase 1: Local foundation

- Reorganize the Go CLI around `Application` and module interfaces.
- Implement profiles, config, secrets, SQLite, object storage, migrations, diagnostics, and jobs.
- Keep current remote commands behind a clearly labeled legacy adapter while local functionality is incomplete.

### Phase 2: Local WeChat and discovery

- Implement QR login/session persistence, account search, account library, article synchronization, single article, albums, and local queries.
- Validate through protocol fixtures and controlled manual smoke tests.

### Phase 3: Download and processing

- Implement direct/trusted-proxy networking, article validation, HTML/content download, resources, metrics, comments, replies, and parser regression corpus.

### Phase 4: Export and terminal workspace

- Implement all export formats, local Chromium PDF, full Bubble Tea workflows, job controls, and safe previews.

### Phase 5: Local MCP and migration tooling

- Implement stdio MCP, legacy Web export/import, backup/restore, legacy remote CLI detection, and parity reporting.

### Phase 6: Compatibility release

- Ship at least one stable release with both the full local binary and the existing online services available.
- Publish migration instructions, retirement timeline, known differences, and rollback steps.

### Phase 7: Retirement

- Require a green mandatory parity matrix.
- Disable new remote OAuth authorization, provide migration responses, archive the last Web-capable tag, remove Web/Worker code and cloud bindings, rotate/remove secrets, and simplify CI/releases.

Rollback before Phase 7 redeploys the archived online services. Rollback never downgrades or mutates local databases. After final retirement, restoring online services is an operational emergency procedure, not a supported user workflow.

## Open Questions

- Which minimum Go version and CGO policy will be accepted? Pure-Go SQLite simplifies cross-builds; platform SQLite/CGO can offer different performance and operational trade-offs.
- Which OS credential-store library and encrypted-vault format will be standardized and independently reviewed?
- Should the first proxy implementation preserve only the current URL-wrapper contract, or include CONNECT/SOCKS5 in the first stable local release?
- Which Chromium-family browsers and minimum versions are officially supported for PDF on each platform?
- What exact normalized JSON export schema is the long-term compatibility contract?
- What database compatibility window will releases guarantee?
- Which Web capabilities are mandatory parity versus intentionally retired, especially public proxy monitoring, support/sponsor pages, embedded API documentation, and development-only pages?
- Should local MCP expose export file paths by default, or require an explicit allowed output root per profile?

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

### 14. Clean-room evidence is artifact-bound and fail-closed

Final release validation uses two versioned evidence documents:

- a platform receipt for one native `GOOS/GOARCH` target;
- a release receipt set that references exactly one passing platform receipt for every supported target.

The platform receipt binds the release tag and source commit to the checksum manifest, archive, extracted binary, build metadata, and per-target SBOM. Every product workflow records the extracted release binary as its executor and repeats the same binary SHA-256. Source-level tests, `go run`, cross-compiled execution, containers that emulate another architecture, and in-process test harnesses remain useful development evidence but cannot satisfy the native clean-room gate.

The receipt validator derives validity rather than trusting producer booleans. It rejects unknown or duplicate workflow IDs, missing required evidence, skipped workflows, inconsistent summary counts, target/provenance disagreement, incomplete network capture, non-release executors, malformed digests, and privacy leakage. Stable publication requires all five native platform receipts to pass against one release identity; a partial receipt set is always incomplete.

The version 1 workflow registry and minimum proof are:

| Workflow ID | Minimum stable evidence |
| --- | --- |
| `install.archive` | Checksum/SBOM/tag agreement, expected archive members, exact version, no external language/runtime dependency |
| `storage.clean-roots` | Config/data/cache/state roots absent or empty before launch, created inside the isolated root, resolved paths and permissions verified |
| `migration.legacy-web` | Versioned legacy archive fingerprint, inspect/import/verify reports, zero corrupt records or objects |
| `migration.database-baselines` | Every promised schema baseline upgraded once to the current schema with data preserved and newer-schema writes refused |
| `login.qr` | Real controlled-account QR decoded, authenticated state reached, QR artifact removed, no login payload retained |
| `session.restart-persistence` | Candidate process restarted and secure backend reused the authenticated session without exposing secrets |
| `sync.account` | Persistent job completed, bounded before/after counts recorded, expected local query succeeds |
| `download.article` | Job completed, normalized content object exists and its digest validates, no response body retained in evidence |
| `download.resources` | Job completed, expected mappings and object digests validate, missing/corrupt count is zero |
| `export.html` | Completed provenance manifest, strict local resources, output verification, network-free local load |
| `export.markdown` | Completed manifest, output count/bytes/checksum, structural verification |
| `export.text` | Completed manifest, UTF-8 output count/bytes/checksum, structural verification |
| `export.json` | Export schema version, record count, content option, provenance and checksum verification |
| `export.xlsx` | OOXML ZIP validation, stable sheet/row counts, manifest verification |
| `export.docx` | OOXML validation, media relationships, supported native-office open smoke where promised |
| `export.pdf` | Candidate-discovered Chromium family/version, PDF signature/page count, manifest verification |
| `automation.cobra` | Success/usage/runtime cases with exit codes `0`/`2`/`1`, exactly one v1 JSON document, progress confined to stderr |
| `ui.tui` | Extracted binary in a native PTY, first launch, navigation, resize, cached view, one local operation, clean exit |
| `automation.mcp` | Extracted binary stdio negotiation, tool schemas, Cobra parity, stdout purity, EOF shutdown, allowed-root and escape cases |
| `storage.backup-restore` | Verified backup with secrets omitted, independent empty restore root, table/object/query/export digest agreement |
| `offline.local-workflows` | OS-enforced deny-all egress while query, integrity, preview/export, TUI, MCP, backup and restore pass with zero network attempts |
| `network.no-retired-domain` | Complete candidate/browser process-tree observation, zero retired-domain matches, only policy-allowed online hosts |
| `security.no-receipt-leakage` | Receipt and bounded stream metadata scanned with zero secret, QR, session, body, HTML, URL-query, or absolute-path findings |
| `secrets.platform-persistence` | Native keyring round trip where available or encrypted-vault fallback, restart reuse, logout secret removal with library retained |

Workflow evidence is a closed, typed structure selected by workflow ID rather than an unrestricted string map. Captured streams are represented by bounded byte counts, SHA-256 digests, JSON-document counts, exit codes, and redaction results; raw live output is not embedded in a published receipt.

### 15. Fixture and live evidence are separate lanes

Deterministic loopback fixtures validate protocol shapes, failure modes, repeatability, and offline behavior without credentials. They can never be relabeled as live evidence. Final clean-room login, session restart, synchronization, article/resource download, and other account-dependent cases use a controlled real WeChat account on each supported native platform and record only bounded counts, classifications, and digests.

The receipt contains no QR payload, cookies, tokens, account identifiers, article identifiers, article body, HTML body, raw upstream response, absolute user path, or secret digest. Online network observation covers the candidate process tree and any browser subprocess used for PDF. The offline phase closes the fixture/live source and enforces deny-all egress at the operating-system boundary; local query, integrity, cached preview/export, TUI, MCP, and backup/restore must continue to work with zero DNS and connection attempts.

PDF evidence invokes an actually installed supported Chromium-family browser. TUI evidence launches the extracted candidate binary in a native PTY. Legacy migration evidence uses the versioned legacy Web archive, not the local backup format. Restore evidence targets an independently empty root and compares bounded metadata and object digests without serializing user content.

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
- Candidate-archive clean-room flows on all five native target tuples, with deterministic fixture receipts for continuous integration and separately authorized live receipts for the final stable-release gate.
- Receipt validator tests for provenance mismatch, missing/duplicate/skipped workflows, fixture-as-live substitution, summary disagreement, incomplete egress observation, unsupported target tuples, and secret or article-body leakage.

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
- **[A test harness can accidentally prove source code instead of the shipped artifact]** → Bind every mandatory workflow to the extracted binary digest and reject source/in-process executors in stable receipts.
- **[Fixtures can be mistaken for real account validation]** → Encode fixture and live phases as distinct evidence types and require controlled-account receipts for every account-dependent stable workflow.
- **[Cross-platform claims can be inferred from build metadata without native execution]** → Require one host/target-matched native receipt per supported tuple and an exact five-target aggregate receipt set.

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

## Finalized implementation choices

- The module requires Go 1.25 or newer. Release archives use `modernc.org/sqlite` with `CGO_ENABLED=0` for every supported target.
- OS secrets use `zalando/go-keyring`; the fallback is a versioned Argon2id plus XChaCha20-Poly1305 vault that must be explicitly initialized and unlocked.
- The first stable proxy adapter preserves the URL-wrapper contract. CONNECT/SOCKS5 remain future additions and do not change the route interface.
- PDF discovers local Google Chrome, Chromium, Microsoft Edge, or Brave. The release gate tests real Chromium rendering where the runner provides a supported browser; there is no remote fallback.
- CLI JSON uses the `wechat-article-cli/v1` success/error envelope; normalized export JSON uses its separately versioned exporter schema and compatibility tests.
- The first database compatibility window is schema 1 through current schema 8. The floor can advance only under the documented bridge-release policy.
- Mandatory parity is the signed 24-entry matrix. Hosted proxy monitoring, support/sponsor pages, embedded Web API docs, and development-only Web pages were intentionally retired.
- MCP exports resolve an explicit or configured default output root and enforce profile allowed roots, including traversal and symlink-escape checks.
- Clean-room evidence uses `wechat-article-clean-room-platform/v1` per native target and `wechat-article-clean-room-release-set/v1` for aggregate approval. Development fixture receipts may diagnose failures but cannot pass the stable gate.

## 1. Baseline and parity contract

- [x] 1.1 Create the executable parity matrix mapping every mandatory Web workflow to its specification requirement, acceptance test, fixture, and release gate.
- [x] 1.2 Capture sanitized baseline outputs for account search, article synchronization, albums, content states, metadata, comments, replies, and every export format.
- [x] 1.3 Classify existing Web pages and server endpoints as mandatory parity, intentional retirement, migration-only, or development-only.
- [x] 1.4 Add repository documentation describing the staged local replacement, compatibility window, and the rule that Web/MCP removal remains blocked until the parity matrix is green.

## 2. Go application architecture

- [x] 2.1 Introduce shared domain types and the task-oriented `Application` interface used by Cobra, Bubble Tea, and MCP.
- [x] 2.2 Split the existing Go CLI into presentation adapters and internal modules for WeChat, library, objects, jobs, network, processor, exporter, secrets, TUI, and MCP.
- [x] 2.3 Move current remote MCP/OAuth behavior behind an explicitly named legacy compatibility adapter without changing its existing command behavior.
- [x] 2.4 Add dependency injection for clock, filesystem roots, HTTP transport, browser discovery, secret store, and process signals.
- [x] 2.5 Add cross-adapter contract tests proving identical query and use-case results through Cobra-facing and application-facing paths.

## 3. Profiles, configuration, and secrets

- [x] 3.1 Implement platform-standard config/data/cache/state path resolution, explicit overrides, and validated portable mode.
- [x] 3.2 Implement versioned profile configuration with safe defaults, atomic writes, cross-process locking, migration backups, and effective-value diagnostics.
- [x] 3.3 Implement profile create/list/use/delete commands and isolation tests for state, jobs, preferences, and secrets.
- [x] 3.4 Implement macOS Keychain, Windows Credential Manager, and Linux Secret Service adapters behind one secret-store interface.
- [x] 3.5 Implement an explicitly initialized encrypted-vault fallback with secure KDF, authenticated encryption, unlock flows, and restrictive permissions.
- [x] 3.6 Implement centralized redaction for logs, errors, URLs, job records, JSON output, diagnostic bundles, and dry runs.
- [x] 3.7 Add secret leakage regression tests covering cookies, tokens, credentials, proxy authorization, query strings, and nested errors.

## 4. SQLite library and object storage

- [x] 4.1 Select and document the SQLite driver/CGO policy and add the database dependency to the Go module.
- [x] 4.2 Define the initial schema for profiles, accounts, articles, albums, content, metrics, comments, replies, resources, routes, jobs, exports, and debug incidents.
- [x] 4.3 Implement ordered database migrations, compatibility bounds, migration backups, downgrade refusal, and migration tests from every supported baseline.
- [x] 4.4 Implement repository transactions and typed query methods for accounts, articles, albums, jobs, exports, and storage status.
- [x] 4.5 Implement the SHA-256 content-addressed object store with streaming writes, atomic rename, deduplication, metadata, and integrity validation.
- [x] 4.6 Implement referential integrity checks between SQLite records and objects, plus incomplete-content marking and redownload recommendations.
- [x] 4.7 Implement backup creation, archive checksums, independent verification, and default secret omission.
- [x] 4.8 Implement validated transactional restore with staging, rollback, and conflict-safe profile handling.
- [x] 4.9 Implement garbage-collection dry run, confirmation, retention policies, and safe deletion of unreferenced objects and stale temporary data.

## 5. Persistent job engine

- [x] 5.1 Implement job and job-item schemas, typed state transitions, execution leases, checkpoints, attempts, and logs.
- [x] 5.2 Implement a bounded scheduler with global, per-operation, host, and credential-sensitive concurrency controls.
- [x] 5.3 Implement cancellation, pause, resume, retry, stale-running recovery, crash-safe checkpoints, and idempotent completed-item detection.
- [x] 5.4 Implement failure classification for network, throttling, authentication, deleted content, known unavailable states, parsing, and storage errors.
- [x] 5.5 Implement bounded exponential backoff with jitter and deterministic test controls.
- [x] 5.6 Add job engine tests for fairness, leases, cancellation, process restart, partial success, auth blocking, and duplicate prevention.

## 6. Direct and proxy networking

- [x] 6.1 Implement the direct HTTP route with cookie-jar integration, bounded timeouts, response-size limits, approved user agents, and request IDs.
- [x] 6.2 Implement allowed-host/scheme validation, redirect revalidation, DNS/IP policy checks, and SSRF protection.
- [x] 6.3 Implement the existing URL-wrapper proxy contract as a network-route adapter with separately stored authorization.
- [x] 6.4 Implement proxy add/list/remove/enable/disable/test commands, trust levels, request-class eligibility, priority, health, and cooldown.
- [x] 6.5 Implement direct-first and explicit fallback routing policies.
- [x] 6.6 Enforce that credential-bearing, paid-content, metrics, and comments requests use direct or explicitly credential-trusted routes only.
- [x] 6.7 Add network policy tests for redirects, private/link-local targets, untrusted proxies, redaction, cooldown, recovery probes, and route selection.

## 7. Local WeChat session

- [x] 7.1 Port the QR-code acquisition, polling, login completion, cookie parsing, token extraction, and account identity flow into the Go `wechat` module.
- [x] 7.2 Render QR codes in compatible terminals and support an explicit QR image output path for headless workflows.
- [x] 7.3 Persist the session in the secret store and implement a persistent cookie jar with expiry and domain/path behavior.
- [x] 7.4 Implement session validation, status, network-unknown state, expiry classification, and actionable relogin behavior.
- [x] 7.5 Implement switchable-account listing and account switching where the upstream session supports it.
- [x] 7.6 Implement best-effort upstream logout plus guaranteed atomic local secret removal.
- [x] 7.7 Add sanitized protocol fixtures and tests for success, waiting, scanned, confirmed, expired QR, missing cookies, invalid redirect, account switch, and expired session.

## 8. Account and article discovery

- [x] 8.1 Port account keyword search, pagination, normalization, and authenticated error mapping to Go.
- [x] 8.2 Port account-name resolution, account-by-article resolution, account details, and author information with strict URL validation.
- [x] 8.3 Implement local account add/update/delete/list/import/export operations and merge policies.
- [x] 8.4 Port article-list request parameter construction, response parsing, pagination completion, and normalized records.
- [x] 8.5 Implement full and incremental account synchronization with date boundaries, pacing, jitter, totals, last-sync time, and resumable checkpoints.
- [x] 8.6 Implement local compound article filtering, stable sorting, pagination, and saved queries for all current grid fields.
- [x] 8.7 Implement single-article URL normalization, provisional records, canonical deduplication, and real `fakeid` repair.
- [x] 8.8 Port album metadata, order selection, continuation paging, all-article traversal, and resumable album jobs.
- [x] 8.9 Add account/article/album protocol fixtures and acceptance tests for pagination, empty results, auth expiry, malformed payloads, and range stopping.

## 9. Go article processor

- [x] 9.1 Define the versioned normalized article model independent of browser DOM and upstream response structs.
- [x] 9.2 Implement bounded extraction of supported WeChat CGI-data and embedded payload variants without executing scripts.
- [x] 9.3 Implement article response classification for valid, deleted, known unavailable, risk-control, and parse-error states.
- [x] 9.4 Implement normalized metadata extraction for title, author, account, timestamps, message type, payment, media, albums, comments, and engagement fields.
- [x] 9.5 Implement resource discovery for images, stylesheets, backgrounds, audio, video, and protocol-relative URLs.
- [x] 9.6 Implement safe HTML normalization, script/ad/tracking removal, content visibility, metadata rendering, and local resource rewriting.
- [x] 9.7 Implement semantic text and Markdown rendering for headings, lists, links, images, code, quotes, tables, and media references.
- [x] 9.8 Implement standard, text-share, image-share, audio, video, paid-content, and known unavailable fixture coverage.
- [x] 9.9 Implement comments/replies rendering and privacy options across supported target representations.
- [x] 9.10 Build the approved semantic and structural golden suite from `samples/` and sanitized production fixtures.
- [x] 9.11 Add parser fuzzing and limit tests for oversized HTML, deep nesting, malformed data, resource explosions, and unsafe script content.

## 10. Download implementations

- [x] 10.1 Implement cache-aware article HTML download jobs with force refresh, validation, status mutation, debug capture, and object-store persistence.
- [x] 10.2 Implement resource download jobs with deduplication, MIME detection, per-article mappings, partial-cache reuse, and missing-resource reporting.
- [x] 10.3 Implement credential import, validation, account association, status, removal, and secure secret references.
- [x] 10.4 Implement engagement metadata download and persistence with capture timestamps and credential provenance.
- [x] 10.5 Implement comments pagination, continuation buffers, top-level comment deduplication, and persistent checkpoints.
- [x] 10.6 Implement incomplete reply-thread download, retry, partial completion, and resume.
- [x] 10.7 Implement paid-article content requests under sensitive-route policy.
- [x] 10.8 Add end-to-end fake-upstream tests for cached, successful, deleted, restricted, risk-controlled, partial-comment, proxy-failure, and credential-expiry flows.

## 11. Export framework and formats

- [x] 11.1 Implement stable export selection manifests for URL, account, album, query, explicit IDs, and all matching articles.
- [x] 11.2 Implement naming templates, invalid-character filtering, length limits, collision policy, path traversal prevention, and platform test cases.
- [x] 11.3 Implement common atomic output staging, cleanup, checksums, provenance manifests, warning aggregation, and verification.
- [x] 11.4 Implement self-contained HTML export with local resources, strict/best-effort modes, comments, and batch archive packaging.
- [x] 11.5 Implement UTF-8 text export with optional metadata headers.
- [x] 11.6 Implement Markdown export with optional front matter and safe embedded HTML policy.
- [x] 11.7 Define and implement the versioned JSON export schema with optional content, metrics, comments, replies, albums, and provenance.
- [x] 11.8 Implement streaming Excel export with stable columns and optional article content.
- [x] 11.9 Implement DOCX/OpenXML export, media embedding, comments, and structural validation tests.
- [x] 11.10 Implement local Chromium-family discovery, self-contained HTML rendering, PDF generation, timeout/cancellation, and actionable dependency errors.
- [x] 11.11 Add format-specific golden/structural tests and curated HTML/PDF visual regression checks.
- [x] 11.12 Add large-batch memory, throughput, interruption, collision, missing-resource, and resume tests.

## 12. Cobra command surface

- [x] 12.1 Replace remote login/status/logout commands with local profile and WeChat session behavior while retaining clear legacy migration messaging.
- [x] 12.2 Add complete `profile`, `account`, `article`, `album`, `sync`, `download`, `metadata`, `comments`, `credential`, `proxy`, `job`, `export`, `db`, and `diagnostics` command groups.
- [x] 12.3 Define exact flags, validation, confirmation values, exit codes, and versioned JSON envelopes for every command.
- [x] 12.4 Implement asynchronous job-start and wait/follow modes suitable for both humans and automation.
- [x] 12.5 Update shell completions and help examples for local-first workflows.
- [x] 12.6 Add CLI integration tests for usage errors, runtime errors, JSON purity, progress-to-stderr, redaction, interruption, and destructive confirmation.

## 13. Bubble Tea terminal workspace

- [x] 13.1 Define the workspace navigation model, shared table/query state, key map, responsive layouts, and no-color/Unicode fallback behavior.
- [x] 13.2 Implement onboarding, profile status, QR login, session status, and offline-library entry points.
- [x] 13.3 Implement account list/detail/search/add/import/export/sync/delete screens.
- [x] 13.4 Implement article list, compound filters, sorting, column selection, multi-selection, single-article ingestion, details, comments, metrics, and bulk actions.
- [x] 13.5 Implement album selection, ordering, pagination, all-link traversal, and batch download/export screens.
- [x] 13.6 Implement safe text/Markdown article previews and explicit local-browser HTML preview handoff.
- [x] 13.7 Implement persistent job dashboard, logs, route health, pause/resume/cancel/retry, and execution-lease visibility.
- [x] 13.8 Implement export configuration, progress, result manifest, and output-opening flows.
- [x] 13.9 Implement credentials, proxies, preferences, storage, backup/restore, integrity, garbage collection, and diagnostics screens.
- [x] 13.10 Add Bubble Tea model/update/view tests for major workflows, narrow terminals, cancellation, confirmation, resize, and non-TTY behavior.

## 14. Local stdio MCP

- [x] 14.1 Implement MCP stdio framing with stdout protocol isolation, stderr logging, bounded messages, EOF shutdown, and malformed-request handling.
- [x] 14.2 Define stable tool schemas for account discovery, local queries, synchronization, download, metadata, comments, local content, exports, jobs, and storage status.
- [x] 14.3 Implement long-running MCP tools as job creation plus status/cancel operations.
- [x] 14.4 Implement read-only mode, per-profile allow/deny policies, destructive confirmation, and sensitive-operation restrictions.
- [x] 14.5 Add MCP conformance and contract tests proving shared application results and absence of remote OAuth/MCP dependencies.
- [x] 14.6 Document Claude, Codex, Cursor, and generic MCP client stdio configuration.

## 15. Legacy data and remote CLI migration

- [x] 15.1 Define a versioned legacy Web export archive schema covering Dexie metadata, HTML, metrics, comments, replies, resource maps, and resource bytes.
- [x] 15.2 Add a final Web-side export flow that creates the legacy archive locally without uploading its contents.
- [x] 15.3 Implement CLI validation, staged import, object deduplication, record reconciliation, conflict policy, and import reports.
- [x] 15.4 Add migration tests from representative Dexie schema versions, partial archives, duplicate data, missing resources, and locally newer records.
- [x] 15.5 Detect legacy remote CLI configuration, preserve it for rollback, ignore OAuth tokens for local auth, and guide users through profile creation and QR login.
- [x] 15.6 Add a migration verification command comparing source manifest counts/checksums with the imported local library.

## 16. Release, documentation, and compatibility release

- [x] 16.1 Update release CI for full native binaries, checksums, SBOM, static checks, race tests, fixture corpus, database upgrades, backup/restore, and cross-platform smoke tests.
- [x] 16.2 Define the supported database compatibility window and add upgrade tests from every promised baseline.
- [x] 16.3 Publish installation and upgrade instructions for macOS, Linux, and Windows, including Go-free release installation.
- [x] 16.4 Rewrite primary documentation for local data paths, profiles, security, credentials, trusted proxies, backups, PDF browser requirements, Cobra, TUI, and stdio MCP.
- [x] 16.5 Publish the compatibility release with complete local functionality while retaining Web and remote MCP services.
- [x] 16.6 Publish deprecation notices and retirement dates in the Web UI, remote MCP authorization page, CLI status, README, and release notes.
- [x] 16.7 Run the complete mandatory parity matrix and record the signed-off results and known intentional differences.

## 17. Web and cloud retirement

- [x] 17.1 Block new remote OAuth authorization after the announced deadline and return actionable local migration responses during the grace period.
- [x] 17.2 Tag and archive the final Web-capable release, sanitized fixtures, schema references, and operational rollback instructions.
- [x] 17.3 Remove Nuxt pages, components, composables, browser-only state, AG Grid, Monaco, and obsolete frontend assets and dependencies.
- [x] 17.4 Remove Nitro APIs, server session/KV code, D1 mirroring, PDF server rendering, Web Docker artifacts, and Cloudflare Pages configuration.
- [x] 17.5 Remove the Cloudflare Worker MCP/OAuth implementation, bindings, secrets, deployment workflows, and remote client compatibility packages.
- [x] 17.6 Remove retired domain references from production configuration and leave them only in explicitly historical migration documentation.
- [x] 17.7 Rotate or delete cloud secrets and bindings, apply the log-retention plan, and verify that no user article or credential data remains beyond documented policy.
- [x] 17.8 Simplify the repository build, tests, release workflow, README, contribution guide, and AGENTS guidance around the Go product.
- [ ] 17.9 Run final clean-room installation, migration, login, synchronization, download, all-format export, TUI, Cobra automation, MCP, backup/restore, and offline tests on every supported platform.

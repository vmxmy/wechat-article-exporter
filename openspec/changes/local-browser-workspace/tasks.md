## 1. Architecture and delivery foundation

- [ ] 1.1 Record the adopted React + TypeScript + Vite + Astryx + TanStack Query/Table architecture in an ADR, including accessibility baseline, package manager, source directory, asset-size budget, and reproducible build contract.
- [ ] 1.2 Add a `web` Cobra command and a testable server lifecycle seam without changing default TUI startup behavior.
- [ ] 1.3 Create the `internal/web` presentation package and document its allowed dependencies on application/profile runtime seams.
- [ ] 1.4 Add deterministic front-end build, hashed asset manifest, `go:embed` packaging, stale-generated-asset CI check, and release build integration.
- [ ] 1.5 Establish versioned `/api/v1` response/error/pagination schemas and API compatibility rules.
- [ ] 1.6 Bootstrap Astryx integration with `ThemeProvider`, `LinkProvider`, strict `reset → astryx → theme` CSS layers, generated theme, and `astryx doctor` CI verification.

## 2. Loopback security and lifecycle

- [ ] 2.1 Bind a random IPv4 loopback port and reject wildcard, LAN, public, malformed Host, and unsupported listener configuration.
- [ ] 2.2 Implement high-entropy in-memory bootstrap token, token-to-HttpOnly-session exchange, URL token removal, session expiry, logout, and shutdown invalidation.
- [ ] 2.3 Implement same-origin Host/Origin/CSRF validation for mutations and request-size/content-type limits.
- [ ] 2.4 Add CSP, referrer, no-sniff, frame, cache-control, and error-redaction middleware with header regression tests.
- [ ] 2.5 Implement `--no-open`, browser auto-open, stdout URL/stderr log discipline, signal cancellation, and bounded graceful shutdown.
- [ ] 2.6 Add security tests for unauthorized access, guessed/reused token, cross-origin mutation, Host confusion, URL/log leakage, and retired-domain absence.

## 3. Shared browser API

- [ ] 3.1 Add runtime/session/status API endpoints backed by the shared application and profile runtime.
- [ ] 3.2 Add paginated account, article, album, export, job, log, lease, saved-query, storage, and diagnostics read endpoints with bounded responses. The implemented browser API includes bounded job detail, saved-query CRUD, safe article-preview handoff, and maintenance/diagnostics reads; this broad parity item remains open pending the remaining resource coverage.
- [ ] 3.3 Add mutation endpoints for login/logout, discovery, account management, sync, ingestion, download, metadata/comments, album traversal, and job control.
- [ ] 3.4 Add export, preview, manifest, verification, opening, preferences, credential, proxy, backup, restore, integrity, garbage-collection, and diagnostic endpoints. Export artifact streaming/opening, preview, maintenance, GC, diagnostic-bundle, and single-archive restore upload/prepare/commit endpoints are implemented; this combined parity item remains open pending the remaining endpoint coverage.
- [ ] 3.5 Ensure every long-running endpoint creates or controls a persistent job and returns a stable job ID.
- [ ] 3.6 Add event/polling API for QR/login and job state changes with reconnect-safe snapshot semantics.
- [ ] 3.7 Add cross-adapter contract tests comparing browser API outcomes with Cobra/TUI/MCP application seams.

## 4. File, export, and confirmation safety

- [ ] 4.1 Define service-validated export root and descendant directory-handle protocol using configured/default Downloads roots and existing output authorization.
- [ ] 4.2 Implement streaming download responses for safe generated artifacts and bounded upload staging for account manifests, credentials, backups, and restore archives. Safe generated export artifacts stream only through opaque artifact capabilities; restore archives have bounded private staging, but this combined item remains open because account-manifest, Credential, and backup browser upload staging is not implemented.
- [x] 4.3 Integrate uploaded restore archives with the existing staged transactional restore pipeline and cleanup guarantees.
- [ ] 4.4 Preserve existing exact scoped confirmation behavior for deletion, job cancellation, restore, GC, credential removal, and proxy trust operations.
- [ ] 4.5 Add path traversal, symlink/escape, oversized upload, archive abuse, failed-restore rollback, and secret non-echo regression tests.

## 5. Browser workspace user experience

- [ ] 5.1 Build the React/Astryx app shell, local connection/error states, profile/session home, local SPA navigation, layout responsiveness, and English/Simplified Chinese resource system.
- [ ] 5.1a Configure TanStack Query cache/polling/invalidation and TanStack Table server-side pagination, sorting, columns, and multi-selection without whole-library client loading.
- [ ] 5.2 Implement QR login/status/logout, account discovery/import/export/manage, and account synchronization views.
- [ ] 5.3 Implement paginated article grid, compound filters, saved queries, columns, multi-selection, single-URL ingestion, safe preview, local HTML handoff, downloads, comments, metrics, and resource completeness actions. Saved-query CRUD and safe local article preview are implemented; this broader resource-completeness item remains open.
- [ ] 5.4 Implement album browsing, ordering/traversal, batch download, and export workflows.
- [ ] 5.5 Implement persistent job dashboard, item/log/lease detail, refresh/event feedback, and permitted pause/resume/retry/cancel controls.
- [ ] 5.6 Implement every export format/options flow, default/custom permitted output selection, manifest/verification, and open-output interaction. Opaque artifact download streaming and exact-confirmed output opening are implemented; the every-format/options parity item remains open.
- [ ] 5.7 Implement credentials, proxies, preferences, storage backup/restore/integrity/GC, diagnostics, and diagnostic bundle flows. Real maintenance wiring, single-archive restore upload/staging/prepare/commit, integrity, GC plan/apply, diagnostics, and opaque diagnostic-bundle download are implemented; this combined item remains open pending the remaining parity flows, including browser file uploads for account manifests and Credentials.
- [ ] 5.8 Add keyboard navigation, focus management, labelled controls, live status announcements, destructive confirmation UX, and narrow-window coverage.

## 6. Verification, documentation, and release

- [ ] 6.1 Add Go handler/integration tests for all API resources, authorization, validation, file boundaries, job transitions, redaction, and graceful shutdown.
- [ ] 6.2 Add browser E2E tests with sanitized loopback fixtures for login UI, account/article selection, job observation/control, export, settings, storage, and failure states.
- [ ] 6.3 Run full `go test ./...`, race, vet, staticcheck, front-end lint/typecheck/test, embedded-asset integrity, and release-size checks in CI.
- [x] 6.4 Extend clean-room release receipt workflow with loopback browser workspace launch, security headers, no-retired-domain observation, and representative browser operation evidence.
- [x] 6.5 Document installation, `wechat-article web`, loopback-only privacy model, profile sharing, directory/upload behavior, troubleshooting, accessibility, and language switching.
- [x] 6.6 Publish a capability matrix showing browser parity with Cobra/TUI/MCP; do not mark the browser workspace complete until every required workflow is verified.

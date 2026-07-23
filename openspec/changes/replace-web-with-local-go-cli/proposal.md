## Why

The current Go CLI is only a remote MCP client: normal use traverses `mptext.ziikoo.app` and then `mp.ziikoo.app`, while the complete product still lives across a Nuxt SPA, Nitro APIs, Cloudflare KV/D1, a Worker OAuth/MCP layer, and browser-local IndexedDB. This split increases operational cost, exposes article activity and credentials to online infrastructure or public proxies, duplicates interfaces, and prevents the CLI from being a self-contained local product.

The project will consolidate the user-facing product into one local-first Go binary. The binary will preserve the useful Web features while directly accessing WeChat or an explicitly configured trusted proxy, storing the user's library locally, and offering Cobra, Bubble Tea, and local MCP interfaces over one shared core.

## What Changes

- **BREAKING** Replace the remote-only Go CLI with a local application engine; normal commands will no longer require `mptext.ziikoo.app`, `mp.ziikoo.app`, remote MCP OAuth, Cloudflare KV, or Cloudflare D1.
- Add local WeChat Official Account QR login, session persistence, session validation, account switching, and logout.
- Add direct account discovery, account import/export, article-list synchronization, single-article ingestion, album traversal, and date-bounded incremental synchronization.
- Add a durable local library backed by SQLite plus content-addressed object storage for article HTML, resources, comments, metadata, debug captures, task state, and preferences.
- Add resumable downloads for article HTML, images, styles, audio, video, metadata, comments, and comment replies, with bounded concurrency, retries, backoff, cancellation, and proxy health tracking.
- Add local querying, sorting, and filtering equivalent to the current account/article grids, exposed consistently through Cobra and Bubble Tea.
- Add export to HTML, Markdown, text, JSON, Excel, DOCX, and PDF, including comments, engagement metadata, local resources, deterministic naming, and batch packaging.
- Add credential import, validation, secure storage, and explicit trust controls for requests that contain WeChat credentials.
- Replace the current minimal Bubble Tea launcher with a full terminal workspace for login, accounts, articles, albums, jobs, exports, settings, storage, diagnostics, and previews.
- Replace the remote Streamable HTTP MCP dependency with an optional local stdio MCP server embedded in the same binary.
- Add backup, restore, garbage collection, integrity checking, legacy Web-data import, and machine-readable JSON output for automation.
- Preserve the existing Web, Nitro, and remote MCP deployments during a compatibility window; remove them only after the local binary passes the parity gate and a release has provided an explicit migration path.
- Replace the current release pipeline with cross-platform native binaries, checksums, release notes, database migration tests, and upgrade compatibility checks.

## Capabilities

### New Capabilities

- `local-runtime`: Single-binary startup, filesystem layout, configuration, profiles, command contracts, structured output, diagnostics, and lifecycle behavior.
- `wechat-session`: Local QR login, cookie/token persistence, validation, account switching, expiry handling, and logout.
- `content-discovery-sync`: Account discovery, account library management, article synchronization, single-article ingestion, album traversal, pagination, and date-bounded sync.
- `local-library`: SQLite metadata, content-addressed object storage, transactions, schema migrations, local queries, backup/restore, integrity checks, and legacy-data import.
- `download-jobs`: Resumable article/resource/metadata/comment downloads, task scheduling, concurrency, retries, proxy routing, cancellation, and status reporting.
- `article-processing`: WeChat HTML validation, CGI-data parsing, normalization, content rendering, resource discovery/rewrite, message-type support, and regression compatibility.
- `export-formats`: HTML, Markdown, text, JSON, Excel, DOCX, PDF, comments/metadata inclusion, deterministic naming, batch export, and overwrite policy.
- `credentials-proxies`: Credential import and validation, secret storage, proxy configuration and health, sensitive-request routing, and redaction.
- `terminal-workspace`: Bubble Tea navigation, tables, filters, selection, previews, job progress, settings, accessibility, and non-interactive fallback behavior.
- `local-mcp`: Embedded stdio MCP discovery and tool calls backed by the same local application modules without OAuth or a remote service.
- `web-retirement`: Parity gate, compatibility period, migration messaging, removal of Nuxt/Nitro/Cloudflare/MCP-Worker surfaces, and rollback/archive requirements.
- `release-distribution`: Native artifact matrix, checksums, SBOM/provenance, upgrade compatibility, release gates, and clean-room installation receipts.

### Modified Capabilities

No existing OpenSpec capabilities are present; this change establishes the initial product specification.

## Impact

- The Go module becomes the primary product and gains implementations for WeChat transport, local persistence, parsing/rendering, downloads, exports, secure secrets, TUI, and stdio MCP.
- The current `internal/mcpclient` and remote OAuth packages become transitional compatibility adapters and are removed after the parity gate.
- The Nuxt application, Nitro endpoints, Dexie/D1 stores, Cloudflare Pages configuration, Worker OAuth/MCP service, Docker-oriented Web deployment, and related workflows enter deprecation and eventual removal scope.
- Existing TypeScript parsing/rendering behavior and `samples/` fixtures remain authoritative migration references until equivalent Go golden tests pass.
- Release artifacts change from a remote client to a full local application; database and object-store compatibility become release-critical contracts.
- Security posture changes from server-managed sessions and optional public proxies to local secrets and direct/trusted-proxy networking.
- Existing users require a documented migration path for browser-local data and remote CLI configuration. No live credentials or article data may be uploaded during migration.

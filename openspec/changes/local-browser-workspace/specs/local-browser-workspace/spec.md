## ADDED Requirements

### Requirement: Loopback-only browser workspace lifecycle

The system SHALL provide `wechat-article web` as a local browser workspace command. It SHALL bind only an ephemeral IPv4 loopback listener, use the selected local profile and existing runtime, optionally open the local browser, and gracefully terminate the listener when its command context ends. It SHALL NOT bind a wildcard, LAN, public, or project-operated address.

#### Scenario: Start a local workspace

- **WHEN** a user runs `wechat-article web` with an available local profile
- **THEN** the command SHALL start a server on `127.0.0.1` with an ephemeral port and expose a single local workspace URL

#### Scenario: Reject non-loopback exposure

- **WHEN** configuration or a future command option requests a wildcard, LAN, or non-loopback listener
- **THEN** startup SHALL fail before listening and SHALL explain that browser workspace access is local-only

#### Scenario: Stop the workspace

- **WHEN** the command context is cancelled or the process receives an interrupt
- **THEN** the server SHALL stop accepting requests, complete bounded shutdown, and invalidate its in-memory browser session credentials

### Requirement: Local browser session authorization

The system SHALL protect every workspace and API request with a cryptographically random per-process browser session. Bootstrap authorization SHALL only be accepted for the URL emitted by the local command, SHALL be removed from the visible URL after establishing the local session, and SHALL NOT be logged, persisted, included in diagnostic bundles, or sent to a non-loopback origin.

#### Scenario: Authorize initial navigation

- **WHEN** a browser opens the exact URL emitted by a newly started workspace
- **THEN** the server SHALL establish an HttpOnly local browser session and redirect or replace the displayed URL without the bootstrap token

#### Scenario: Reject an unauthenticated request

- **WHEN** a request lacks a valid local browser session
- **THEN** the server SHALL return an authorization failure without returning profile, article, job, session, or secret data

#### Scenario: Reject a cross-origin mutation

- **WHEN** a state-changing API request has a missing or non-loopback same-origin Origin/CSRF proof
- **THEN** the server SHALL reject the request before invoking an application operation

### Requirement: Embedded React browser workspace assets

The browser workspace SHALL be delivered from a React + TypeScript + Vite application embedded in the released Go binary. It SHALL use Astryx for design-system primitives and accessible theme infrastructure, and TanStack Query/Table for local API state and server-paginated grids. Normal use SHALL NOT require Node.js, a front-end dev server, a CDN, external fonts, a project-operated Web service, or an additional database installation.

#### Scenario: Run from a release archive without front-end tooling

- **WHEN** a user runs a released `wechat-article` binary on a supported platform without Node.js installed
- **THEN** `wechat-article web` SHALL serve its initial document, scripts, styles, and icons locally from that binary

#### Scenario: Render an accessible themed application

- **WHEN** an authenticated browser loads the workspace
- **THEN** the React application SHALL initialize Astryx theme and link providers before rendering navigable workspace content

#### Scenario: Page a large local article library

- **WHEN** a browser user views, sorts, filters, changes visible columns, or selects articles in a large library
- **THEN** TanStack Query/Table SHALL request bounded server-side pages and SHALL NOT require the browser to load the entire library

#### Scenario: Serve an unknown browser route

- **WHEN** an authenticated browser navigates directly to a supported client-side workspace route
- **THEN** the server SHALL return the embedded application shell without treating the route as a filesystem path

### Requirement: Shared application semantics across local adapters

The browser workspace SHALL be a presentation adapter over the same application, profile runtime, SQLite metadata, object storage, secret store, network policy, and persistent jobs used by Cobra, Bubble Tea, and stdio MCP. Browser handlers and client code SHALL NOT duplicate domain persistence, WeChat protocol, scheduler, export, or secret rules.

#### Scenario: Observe data created by another adapter

- **WHEN** a user creates a download, synchronization, export, or local record through Cobra, TUI, or MCP
- **THEN** the browser workspace SHALL show the corresponding profile-scoped data and job state from the shared local runtime

#### Scenario: Start work in the browser

- **WHEN** a browser user starts a long-running synchronization, download, or export
- **THEN** the system SHALL create the same persistent job model and return its job identity rather than executing the work inside the HTTP request lifetime

### Requirement: Complete local product workflows in the browser

The browser workspace SHALL provide user interfaces for the existing local product workflows: QR login/session/logout; account discovery and management; article query, filtering, single-article ingestion, preview, content/resource download and metadata/comments actions; album traversal and batch work; all supported export formats; job inspection and controls; credentials, proxies, and preferences; storage backup/restore/integrity/garbage collection; and diagnostics.

#### Scenario: Manage an account and its articles

- **WHEN** an authenticated user searches or selects a saved account in the browser workspace
- **THEN** the user SHALL be able to start synchronization, inspect paginated local articles, apply supported filters, select articles, and start supported downloads or exports

#### Scenario: Manage an existing persistent job

- **WHEN** a browser user opens a queued, running, blocked, failed, partial, paused, cancelled, or completed job
- **THEN** the workspace SHALL show bounded item/log/lease status and expose only the state transitions permitted by the shared job engine

#### Scenario: Use a complete export workflow

- **WHEN** a browser user selects articles or an album and chooses a supported export format
- **THEN** the workspace SHALL collect format options and a permitted local output destination, create a persistent export job, and expose progress, result manifest, verification, and output-opening actions

### Requirement: Safe local file and destructive-operation handling

The browser workspace SHALL NOT accept an unrestricted host filesystem path from browser JavaScript. It SHALL use service-validated export roots or descendant directory handles, private staging for uploaded imports/restores, streamed downloads for generated archives, and the existing authorization/path traversal rules. Destructive, secret, restore, proxy-trust, and garbage-collection actions SHALL preserve existing scoped confirmation requirements.

#### Scenario: Choose a default export destination

- **WHEN** a browser user starts an export without configuring a custom root
- **THEN** the workspace SHALL offer the profile export root when set or the local Downloads export default, and the server SHALL normalize and authorize the resulting path before queuing the job

#### Scenario: Restore an uploaded backup

- **WHEN** a user uploads a backup archive and confirms restore in the browser workspace
- **THEN** the server SHALL stage and validate the upload privately before invoking the existing transactional restore flow

#### Scenario: Attempt path traversal

- **WHEN** a browser request supplies a path, filename, directory token, or archive member that escapes its authorized root or staging area
- **THEN** the server SHALL reject the request without modifying files outside the authorized local scope

### Requirement: Browser workspace security and privacy controls

The browser workspace SHALL serve strict security headers, prohibit sensitive response caching, redact sensitive failures, validate Host/Origin/request payload sizes, and retain the existing direct-or-explicitly-trusted network policy for WeChat credentials. It SHALL never contact retired project domains as part of startup, asset serving, browser operations, or API use.

#### Scenario: Inspect browser response headers

- **WHEN** the workspace serves its document or API response
- **THEN** it SHALL provide a restrictive Content Security Policy, no-referrer behavior, MIME sniffing protection, and no-store caching for sensitive state

#### Scenario: Trigger a credential-bearing action

- **WHEN** a browser user initiates an operation that carries WeChat or proxy credentials
- **THEN** the shared network layer SHALL continue to use direct transport or only an explicitly credential-trusted route

#### Scenario: Scan runtime traffic

- **WHEN** browser workspace workflows are executed under network observation
- **THEN** no request SHALL target `mp.ziikoo.app`, `mptext.ziikoo.app`, a retired remote OAuth service, or an undeclared non-local workspace origin

### Requirement: Accessible bilingual browser interaction

The browser workspace SHALL support the existing English and Simplified Chinese preference, keyboard-operable navigation, labelled controls, visible focus, status/error announcements, and responsive layouts for common desktop browser widths.

#### Scenario: Change display language

- **WHEN** a user changes the local display language in the browser workspace
- **THEN** the UI SHALL update to English or Simplified Chinese and persist the same `display.language` preference used by the TUI

#### Scenario: Operate without a pointer

- **WHEN** a keyboard-only user navigates account, article, action, confirmation, and job controls
- **THEN** the controls SHALL have a logical focus order, visible focus state, accessible name, and operable keyboard behavior

### Requirement: Verifiable local browser release quality

The system SHALL have deterministic unit/integration tests for listener and authorization boundaries, API/application parity, file-policy negative cases, and embedded asset serving; browser E2E tests for core workflows; and release evidence that the extracted native binary serves the workspace locally without project-operated Web dependencies.

#### Scenario: Run browser workspace regression tests

- **WHEN** CI runs the browser workspace test suite
- **THEN** it SHALL verify authorized and rejected requests, same-profile cross-adapter visibility, representative login/job/export workflows, and no-network local asset serving with sanitized fixtures

#### Scenario: Validate a release artifact

- **WHEN** release validation runs against an extracted candidate binary
- **THEN** it SHALL prove loopback-only service, embedded asset availability, browser workspace operation, security-header checks, and absence of retired-domain traffic

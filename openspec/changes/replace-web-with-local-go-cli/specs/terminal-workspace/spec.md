## ADDED Requirements

### Requirement: Full terminal workspace
Running `wechat-article` with no subcommand in an interactive terminal SHALL open a Bubble Tea workspace that exposes login, accounts, articles, single-article ingestion, albums, downloads, exports, jobs, credentials, proxies, settings, storage, and diagnostics.

#### Scenario: First launch
- **WHEN** an unauthenticated user opens the workspace
- **THEN** the login and local-library actions are discoverable, network-dependent actions explain their prerequisites, and offline cached actions remain usable

### Requirement: Consistent core behavior
Bubble Tea screens SHALL call the same application modules as Cobra and MCP and SHALL not reimplement WeChat, storage, query, download, processing, or export rules.

#### Scenario: Start export from TUI
- **WHEN** a user exports selected articles from the workspace
- **THEN** the same selection, validation, job, naming, and output rules apply as the equivalent Cobra command

### Requirement: Account and article tables
The workspace SHALL provide paged local account and article tables with search, sorting, compound filtering, selectable columns, multi-selection, and bulk actions corresponding to supported local queries and jobs.

#### Scenario: Filter and bulk download
- **WHEN** a user filters articles, selects the visible result set, and starts content download
- **THEN** the workspace shows the resolved item count before starting and creates one persistent job for the selected stable article IDs

### Requirement: Detail and preview views
The workspace SHALL display account details, article metadata, normalized text/Markdown previews, local HTML preview handoff, comments, engagement metrics, album membership, resource completeness, and recent errors without executing untrusted article scripts inside the terminal process.

#### Scenario: Preview cached article
- **WHEN** a user opens an article with local content
- **THEN** the terminal shows a safe textual preview and can open a generated local HTML preview only after an explicit action

### Requirement: Job dashboard
The workspace SHALL display persistent jobs and item progress, live throughput, route health, logs, warnings, and controls for pause, resume, cancel, and retry where supported.

#### Scenario: Reattach to running job
- **WHEN** a second TUI session opens while another process owns a running job
- **THEN** it can observe persisted progress and clearly indicates which controls are unavailable because another process holds the execution lease

### Requirement: Settings and secrets UX
The workspace SHALL edit non-secret preferences, launch secure secret entry flows, validate risky values, and distinguish saved values from effective runtime values.

#### Scenario: Configure unsafe sync interval
- **WHEN** the user enters a value below the recommended minimum
- **THEN** the workspace displays a risk warning and requires confirmation before saving

### Requirement: Terminal adaptability and accessibility
The workspace SHALL handle terminal resize, narrow layouts, keyboard-only use, documented key bindings, no-color mode, high-contrast-friendly styling, Unicode fallback, and screen-reader-oriented plain output mode.

#### Scenario: Narrow terminal
- **WHEN** terminal width is insufficient for the full article table
- **THEN** the workspace switches to a compact view without hiding navigation or destructive-action confirmations

### Requirement: Non-TTY behavior
The application SHALL never launch an interactive Bubble Tea program when stdin or stdout is not a TTY unless explicitly forced.

#### Scenario: No subcommand in a pipeline
- **WHEN** the binary is invoked without a subcommand in a non-interactive environment
- **THEN** it prints concise help or a structured usage result and exits without blocking for input

### Requirement: Destructive-action confirmation
The workspace SHALL identify destructive actions, show exact scope and recoverability, and require explicit confirmation that cannot be satisfied by an incidental key press.

#### Scenario: Delete account data
- **WHEN** a user requests deletion of selected accounts and their local data
- **THEN** the confirmation view shows account count, article count, estimated objects, and backup recommendation before accepting deletion

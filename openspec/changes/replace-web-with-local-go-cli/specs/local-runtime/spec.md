## ADDED Requirements

### Requirement: Standalone local operation
The released `wechat-article` binary SHALL provide its primary account, article, download, export, storage, and automation capabilities without requiring `mp.ziikoo.app`, `mptext.ziikoo.app`, Cloudflare KV, Cloudflare D1, or any other project-operated runtime service.

#### Scenario: Run with project services unavailable
- **WHEN** the project-operated Web and MCP domains are unreachable but WeChat and any configured trusted proxy remain reachable
- **THEN** the user can log in, synchronize articles, download content, query the local library, and export files from the binary

#### Scenario: Work entirely offline with cached data
- **WHEN** the host has no network connection and requested articles and resources are already present locally
- **THEN** local queries, previews, backups, integrity checks, and exports that do not require missing external resources remain available

### Requirement: Platform-native data layout
The system SHALL use platform-standard config, data, cache, and state directories, SHALL support explicit path overrides, and SHALL never write persistent application data beside the executable unless portable mode is explicitly selected.

#### Scenario: First launch
- **WHEN** a user starts the binary with no existing state
- **THEN** the system creates only the required directories and files with restrictive permissions and reports their resolved paths through `status`

#### Scenario: Portable mode
- **WHEN** a user starts the binary with portable mode and an explicit root directory
- **THEN** all non-keychain application state is stored below that validated root directory

### Requirement: Profile isolation
The system SHALL support multiple named local profiles and SHALL isolate each profile's WeChat session, credentials, database namespace, preferences, jobs, and exported defaults.

#### Scenario: Switch profiles
- **WHEN** a user selects a different profile
- **THEN** subsequent commands and TUI actions use only that profile's state without exposing data from the previously active profile

#### Scenario: Delete a profile
- **WHEN** a user confirms deletion of a non-active profile
- **THEN** the system removes the profile's secrets and local state without affecting other profiles

### Requirement: Stable command and output contract
The Cobra interface SHALL expose deterministic help, shell completion, documented exit codes, non-interactive flags, and a global `--json` mode whose stdout contains only one machine-readable result document.

#### Scenario: Successful JSON command
- **WHEN** an automation caller runs a supported command with `--json`
- **THEN** stdout contains exactly one UTF-8 JSON document with `schemaVersion="wechat-article-cli/v1"`, `success=true`, and a `data` value; progress is suppressed or written to stderr, and the process exits with code `0`

#### Scenario: Invalid command usage
- **WHEN** arguments or flags violate the command contract
- **THEN** the system emits `schemaVersion`, `success=false`, and an `error` object containing `kind="usage"`, a redacted `message`, and `exitCode=2`, then exits with code `2`

#### Scenario: Runtime failure
- **WHEN** a valid command fails because of network, authentication, storage, parsing, or export conditions
- **THEN** the system emits the same envelope with `kind="runtime"`, a redacted message, and `exitCode=1`, then exits with code `1`

#### Scenario: Compatible JSON evolution
- **WHEN** a v1 producer adds a field that is not required by the documented v1 envelope
- **THEN** existing consumers can ignore it, while deleting or renaming required fields or changing their types requires a new incompatible schema version

#### Scenario: Stable pagination
- **WHEN** an automation caller traverses account, article, album, job, or export pages without changing the underlying query
- **THEN** results use stable ordering with deterministic tie breakers and do not duplicate or omit items between pages

### Requirement: Observability and diagnostics
The system SHALL provide human-readable and structured status, version, configuration, storage, dependency, migration, session, proxy, and recent-job diagnostics without exposing secrets.

#### Scenario: Debug logging
- **WHEN** a user enables debug logging
- **THEN** logs include request IDs, durations, retry decisions, storage operations, and job transitions while redacting cookies, tokens, credentials, authorization headers, and sensitive query parameters

#### Scenario: Diagnostic bundle
- **WHEN** a user creates a diagnostic bundle
- **THEN** the private archive contains system metadata, redacted configuration, database/configuration schema versions, recent job metadata, and integrity results, excludes article bodies and all secret-store bytes, refuses to overwrite an existing file, and reports its SHA-256

### Requirement: Graceful interruption
Long-running operations SHALL respond to cancellation signals, persist recoverable progress, close files and database transactions, and return a distinct interrupted result.

#### Scenario: Interrupt active synchronization
- **WHEN** the process receives Ctrl-C during a synchronization job
- **THEN** new work stops, active requests are cancelled where possible, completed items remain committed, the job becomes resumable, and the database remains consistent

### Requirement: Versioned configuration
Configuration SHALL have an explicit schema version, safe defaults, atomic writes, cross-process locking, migration support, and unknown-field preservation where forward compatibility permits it.

#### Scenario: Upgrade configuration
- **WHEN** a newer binary opens an older supported configuration version
- **THEN** it creates a recoverable backup, migrates atomically, and reports the applied migration

#### Scenario: Concurrent writers
- **WHEN** two processes attempt to update the same profile configuration
- **THEN** updates are serialized or one process fails safely without corrupting the configuration

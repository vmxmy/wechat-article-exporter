## ADDED Requirements

### Requirement: Embedded stdio MCP server
The binary SHALL provide `wechat-article mcp serve --transport stdio` and SHALL implement MCP over stdin/stdout without requiring network listening, remote OAuth, or a project-operated service.

#### Scenario: Start stdio server
- **WHEN** an MCP client starts the command
- **THEN** stdout is reserved for protocol messages, logs are written to stderr, and the server advertises its implementation and supported capabilities

#### Scenario: Protocol negotiation
- **WHEN** a client initializes with the supported protocol version
- **THEN** the server returns that exact version and its local-only capabilities; a missing or unsupported version receives a structured protocol error and cannot continue as initialized

### Requirement: Shared application modules
MCP tools SHALL invoke the same session, discovery, library, download, processing, and export modules used by Cobra and Bubble Tea.

#### Scenario: MCP article query
- **WHEN** an MCP client lists local articles with filters
- **THEN** the result matches the equivalent Cobra query and does not contact a remote MCP adapter

### Requirement: Tool surface
The local MCP server SHALL expose account search and resolution, local account and article queries, article and album synchronization, article download, metadata and comments jobs, local content retrieval, export initiation, job status, and storage status with explicit schemas.

#### Scenario: Discover tools
- **WHEN** the client requests the tool list
- **THEN** each tool includes a stable name, description, input schema, output contract, and read-only or destructive annotation where supported

### Requirement: Long-running operation behavior
MCP requests that start long-running work SHALL return a persistent job identifier promptly and SHALL expose separate status and cancellation tools instead of holding the protocol request open for the entire operation.

#### Scenario: Start batch download
- **WHEN** an MCP client requests download for many articles
- **THEN** the server validates the selection, creates a job, returns its identifier, and permits later progress queries

### Requirement: Local authorization policy
The stdio server SHALL rely on operating-system process access and profile selection, SHALL not implement remote OAuth, and SHALL require explicit confirmation arguments for destructive tools and sensitive secret operations.

#### Scenario: Destructive tool without confirmation
- **WHEN** an MCP client calls a destructive tool without the exact required confirmation value
- **THEN** the tool refuses to mutate local state and returns the required confirmation contract

### Requirement: Protocol-safe output
The MCP adapter SHALL bound message sizes, reject malformed JSON-RPC, avoid writing non-protocol data to stdout, redact errors, and terminate cleanly on EOF or cancellation.

#### Scenario: Malformed request
- **WHEN** stdin contains an invalid or oversized protocol message
- **THEN** the server returns a protocol error when possible and does not execute a partially decoded tool call

#### Scenario: EOF or cancellation
- **WHEN** the MCP client closes stdin or cancels the server context
- **THEN** the server terminates without protocol pollution, releases its input wait, and does not leave an untracked in-memory operation running

### Requirement: Bounded results and file roots
MCP list/query results SHALL use explicit limits and stable pagination, and file-producing tools SHALL write only below profile-configured allowed roots after path traversal and symlink-escape validation.

#### Scenario: Export within allowed root
- **WHEN** `exports.start` resolves an explicit or configured default output directory below an allowed root
- **THEN** the canonical path is passed to the shared export application and a persistent job ID is returned

#### Scenario: Export path escape
- **WHEN** an MCP caller supplies an absolute path outside all allowed roots, a `..` escape, or a symlink path whose existing ancestor resolves outside a root
- **THEN** the request is rejected before an export job or output file is created

### Requirement: Optional tool policy
Users SHALL be able to configure read-only mode and explicit allow/deny lists for MCP tools per profile.

#### Scenario: Read-only mode
- **WHEN** MCP read-only mode is enabled
- **THEN** synchronization, download, export, credential, deletion, restore, and other mutating tools are not executable

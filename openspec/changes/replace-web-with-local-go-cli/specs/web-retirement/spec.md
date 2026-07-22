## ADDED Requirements

### Requirement: Feature-parity gate
The project SHALL not remove the Nuxt Web application or remote MCP service until the local binary passes an approved parity matrix covering authentication, account discovery, article synchronization, single article, albums, content/resources, metrics, comments/replies, filters, all export formats, local storage operations, CLI/TUI automation, and local MCP.

#### Scenario: Missing parity item
- **WHEN** any mandatory parity case is failing or untested
- **THEN** Web and remote MCP removal tasks remain blocked

### Requirement: Compatibility release window
At least one stable release SHALL ship the complete local replacement, migration tooling, and deprecation notices while the existing Web and remote MCP deployments remain available for rollback and data export.

#### Scenario: Compatibility release
- **WHEN** the replacement first reaches stable parity
- **THEN** release notes explain changed network paths, local data locations, migration commands, secret handling, and the planned retirement milestone

### Requirement: User data migration
The Web retirement process SHALL provide a legacy browser export and local CLI import path and SHALL document that browser-local IndexedDB cannot be migrated automatically without user action.

#### Scenario: User migrates Web data
- **WHEN** a user exports their legacy browser library and imports it locally
- **THEN** the CLI provides counts, conflicts, skipped records, missing resources, and a verification result before the user retires the Web copy

### Requirement: Remote CLI migration
The project SHALL detect legacy remote-only CLI configuration, explain that remote OAuth/MCP is deprecated, and provide an explicit migration path to a local profile without copying OAuth tokens into the new session store.

#### Scenario: Legacy config detected
- **WHEN** the new binary finds a prior `server` and OAuth token configuration
- **THEN** it preserves the file for rollback, ignores the token for local WeChat authentication, and guides the user through local login

### Requirement: Removal scope
After the parity gate and compatibility window, the repository SHALL remove the Nuxt pages/components/composables, Nitro APIs, Dexie/D1 adapters, Cloudflare Pages and Worker configurations, remote OAuth/MCP implementation, Web Docker artifacts, and obsolete JavaScript dependencies and workflows.

#### Scenario: Retirement commit
- **WHEN** the final retirement change is prepared
- **THEN** no released binary command depends on removed online services and repository searches find no production references to the retired domains except historical migration documentation

### Requirement: Historical preservation
Before removal, the project SHALL tag or archive the final Web-capable release and preserve sanitized fixtures, migration documentation, schema descriptions, and behavior notes needed to maintain imported data and parser compatibility.

#### Scenario: Investigate legacy behavior
- **WHEN** a future maintainer needs to compare a migrated result with the Web implementation
- **THEN** the archived tag and retained fixtures provide a reproducible reference without restoring live project-operated services

### Requirement: Operational shutdown
The retirement plan SHALL include disabling new remote authorization, revoking or expiring Worker OAuth material, retaining only the minimum legally and operationally required logs, removing Cloudflare bindings and secrets, and communicating shutdown dates.

#### Scenario: Shut down remote MCP
- **WHEN** the retirement date is reached
- **THEN** new authorization is disabled, existing clients receive a migration response for the announced grace period, and project secrets are removed after rollback needs expire

### Requirement: Rollback boundary
Rollback SHALL restore a tagged Web/MCP release and its compatible infrastructure without modifying or downgrading the user's local binary database.

#### Scenario: Emergency rollback during compatibility window
- **WHEN** a critical local replacement defect requires temporary service restoration
- **THEN** operators can redeploy the archived compatible services while local users retain their independently stored libraries

### Requirement: Documentation replacement
Primary documentation SHALL describe the local binary as the product, SHALL no longer instruct normal users to use the retired Web domains, and SHALL include installation, security, storage, backup, proxy, browser-PDF, TUI, Cobra, and MCP guidance.

#### Scenario: Fresh installation documentation
- **WHEN** a new user follows the main README after retirement
- **THEN** every required step can be completed with the released binary and external WeChat access without deploying the old Web stack

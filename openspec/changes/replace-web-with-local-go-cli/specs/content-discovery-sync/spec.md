## ADDED Requirements

### Requirement: Account search and resolution
The system SHALL search WeChat Official Accounts by keyword, paginate results, resolve an account from a valid WeChat article URL, and retrieve available account details and author metadata.

#### Scenario: Search accounts
- **WHEN** an authenticated user searches with a keyword, offset, and page size
- **THEN** the system returns normalized account records including stable identifiers and preserves the upstream pagination result

#### Scenario: Resolve account from article URL
- **WHEN** a user provides a supported WeChat article URL
- **THEN** the system extracts the published account identity and resolves the corresponding account record when the authenticated search endpoint permits it

#### Scenario: Reject unsupported URL
- **WHEN** the supplied URL is not HTTPS or is outside the approved WeChat host set
- **THEN** the system rejects it before making an external request

### Requirement: Local account library
The system SHALL let users add search results to the local library, list and filter saved accounts, update account details, delete selected accounts, and import or export account manifests.

#### Scenario: Add an account
- **WHEN** a user adds a discovered account
- **THEN** the account is upserted by stable `fakeid` and its prior synchronized data is preserved

#### Scenario: Delete account data
- **WHEN** a user confirms deletion of a saved account and its data
- **THEN** all account-scoped metadata and object references are removed transactionally while shared unreferenced objects become eligible for garbage collection

#### Scenario: Import account manifest
- **WHEN** a valid manifest contains new and existing accounts
- **THEN** records are validated and upserted without deleting locally richer metadata

### Requirement: Article-list synchronization
The system SHALL synchronize article lists for one or many saved accounts, handle WeChat pagination, persist progress, update account totals and last-sync time, and resume interrupted synchronization.

#### Scenario: Initial full synchronization
- **WHEN** a saved account has no local article-list state and the user requests full synchronization
- **THEN** pages are fetched until the upstream completion condition or configured date boundary is reached and each page is committed atomically

#### Scenario: Incremental synchronization
- **WHEN** an account has prior synchronization state
- **THEN** the system refreshes new or changed articles and avoids needless traversal beyond the configured boundary

#### Scenario: Session expires mid-sync
- **WHEN** WeChat expires the session after some pages were committed
- **THEN** committed pages remain available, the job becomes blocked on authentication, and a later resume continues from a safe checkpoint

### Requirement: Synchronization range and pacing
The system SHALL support the current range choices, an explicit timestamp/date boundary, an all-history mode, configurable page pacing, jitter, and a minimum safe delay warning.

#### Scenario: Date-bounded sync
- **WHEN** the user chooses a date boundary
- **THEN** synchronization stops once all subsequently fetched article timestamps are older than the boundary

#### Scenario: Aggressive pacing
- **WHEN** a user configures a delay below the recommended safety threshold
- **THEN** the system warns about account risk and requires explicit confirmation for persistent use

### Requirement: Article querying and filtering
The system SHALL query locally stored articles by account, keyword, author, publication range, deletion state, download state, comment state, originality, payment state, message type, album, and engagement ranges, with stable sorting and pagination.

#### Scenario: Compound article filter
- **WHEN** a user filters original articles by author, date range, and content-download state
- **THEN** the result is evaluated locally and returned in stable requested order without contacting WeChat

### Requirement: Single-article ingestion
The system SHALL accept one or many article URLs without a pre-existing saved account, normalize supported URL variants, create provisional records, download and parse the article, and repair the provisional account identifier from article data.

#### Scenario: Add a single article
- **WHEN** a user supplies a valid WeChat article URL
- **THEN** the system stores a canonical URL, creates or updates an article record, resolves its real account identifier when possible, and avoids duplicate records

### Requirement: Album discovery and traversal
The system SHALL list an account's known albums, retrieve album metadata and articles in forward or reverse order, paginate through all pages, and add or download selected album articles.

#### Scenario: Traverse an entire album
- **WHEN** the user requests all articles in an album
- **THEN** the system follows continuation identifiers until completion, deduplicates articles, and stores a resumable traversal checkpoint

### Requirement: Upstream response preservation
The discovery layer SHALL retain redacted raw response fixtures for failed or unknown response variants when debug capture is enabled, while presenting normalized domain records to callers.

#### Scenario: Unknown upstream shape
- **WHEN** a WeChat endpoint returns a successful response whose shape cannot be normalized
- **THEN** the system records a redacted diagnostic artifact, does not partially corrupt the library, and reports a protocol compatibility error

## ADDED Requirements

### Requirement: Persistent job model
The system SHALL represent synchronization, article download, resource download, metadata download, comment download, export, backup, restore, import, integrity check, and garbage collection as persistent jobs with typed states and item-level progress.

#### Scenario: Create a job
- **WHEN** a user starts a multi-item operation
- **THEN** the system persists job intent and item identifiers before performing external side effects

#### Scenario: Restart after crash
- **WHEN** the process terminates unexpectedly during a resumable job
- **THEN** the next launch marks abandoned active items safely and offers resume from committed checkpoints

### Requirement: Bounded scheduling
The scheduler SHALL enforce configurable global and per-operation concurrency, host-level rate limits, credential-sensitive concurrency limits, queue fairness, timeouts, and cancellation.

#### Scenario: Credential-sensitive job
- **WHEN** a metadata or comments job is scheduled with a general concurrency higher than the credential safety limit
- **THEN** the scheduler applies the lower sensitive-request limit and reports the effective value

#### Scenario: Multiple job types
- **WHEN** article and resource jobs are queued together
- **THEN** the scheduler prevents one large job from permanently starving the other

### Requirement: Retry and backoff
The system SHALL classify transient network errors, upstream throttling, authentication errors, permanent article states, parser failures, and local storage failures, and SHALL retry only retryable failures with bounded exponential backoff and jitter.

#### Scenario: Transient proxy failure
- **WHEN** a proxy request times out and retry budget remains
- **THEN** the item is retried after backoff using the next eligible route

#### Scenario: Deleted article
- **WHEN** downloaded content is positively identified as deleted
- **THEN** the article is marked deleted and the item is not retried as a network failure

### Requirement: Cache-aware article HTML download
The downloader SHALL skip already valid HTML unless force-download is enabled, SHALL validate every new response, SHALL persist successful HTML and extracted comment identifiers, and SHALL record abnormal responses for diagnosis when configured.

#### Scenario: Cached article
- **WHEN** valid HTML is already stored and force-download is disabled
- **THEN** the job marks the item complete without a network request

#### Scenario: Risk-control response
- **WHEN** WeChat or a proxy returns an HTML response that indicates risk control or cannot be parsed as an article
- **THEN** the response is not stored as valid content, a redacted debug object can be retained, and retry policy is applied according to classification

### Requirement: Resource download
The downloader SHALL discover and fetch article images, stylesheets, background images, audio, video, and other supported resources, normalize protocol-relative URLs, deduplicate objects, and record per-article resource mappings.

#### Scenario: Partially cached resources
- **WHEN** an article references both stored and missing resources
- **THEN** only missing or corrupt objects are downloaded and the final mapping includes both sets

### Requirement: Engagement metadata download
The system SHALL use a valid account credential to retrieve and persist read, like, old-like, share, and comment counts supported by the article payload.

#### Scenario: Metadata extraction
- **WHEN** an authenticated article response contains engagement fields
- **THEN** the normalized metrics are stored with capture time and source credential identity

#### Scenario: Missing credential
- **WHEN** metadata is requested for an account without a valid credential
- **THEN** the item fails before sending the article request and provides a credential setup action

### Requirement: Comments and replies download
The system SHALL page through top-level comments, persist all successful pages, retrieve incomplete reply threads, and resume from saved continuation identifiers.

#### Scenario: Multi-page comments
- **WHEN** a comments response sets a continuation flag and buffer
- **THEN** subsequent pages are requested until completion and duplicate comments are removed by stable identifiers

#### Scenario: Partial reply failure
- **WHEN** some reply threads succeed and one exhausts its retry budget
- **THEN** successful replies remain committed, the job is partial, and resume targets only incomplete threads

### Requirement: Progress and control
The system SHALL report queued, running, completed, skipped, deleted, failed, cancelled, and blocked counts; current throughput; estimated remaining work when meaningful; and per-route health. Users SHALL be able to pause, resume, cancel, and retry supported jobs.

#### Scenario: Cancel a job
- **WHEN** a user cancels an active job
- **THEN** no new items start, active cancellable requests stop, safe completed writes remain, and the job reaches a terminal cancelled state

### Requirement: Idempotent job execution
Re-running or resuming an operation with the same target set SHALL not create duplicate article, comment, resource, or export records and SHALL not repeat completed external work unless the user requests refresh or force behavior.

#### Scenario: Resume completed items
- **WHEN** a resumed job includes items already committed before interruption
- **THEN** those items are recognized and skipped without duplicate writes

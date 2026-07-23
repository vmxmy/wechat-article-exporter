## ADDED Requirements

### Requirement: SQLite metadata store
The system SHALL use SQLite as the authoritative local metadata store for profiles, accounts, articles, albums, HTML records, resource references, engagement metadata, comments, replies, credentials metadata, jobs, checkpoints, exports, and debug records.

#### Scenario: Atomic article-page commit
- **WHEN** one synchronized page contains account, article, and album updates
- **THEN** all related metadata is committed in one transaction or none of it is visible

#### Scenario: Concurrent readers and writer
- **WHEN** the TUI reads the library while a background job writes new records
- **THEN** readers observe a consistent snapshot and neither operation corrupts the database

### Requirement: Content-addressed object storage
The system SHALL store large HTML and binary resources outside SQLite by cryptographic digest, SHALL deduplicate identical content, and SHALL track object size, media type, integrity hash, creation time, and reference ownership.

#### Scenario: Duplicate resource
- **WHEN** multiple articles download the same binary resource
- **THEN** the object is stored once and referenced by each article

#### Scenario: Object hash mismatch
- **WHEN** a stored object's bytes do not match its recorded digest
- **THEN** integrity checking reports corruption and the object is not used for export without explicit override

### Requirement: Schema migration safety
Every persistent schema change SHALL use ordered migrations, a pre-migration backup or snapshot strategy, transactional application where SQLite permits it, and the exact documented compatibility window. For the first local stable line the window is schema 1 through the current schema 8, with upgrade fixtures for every baseline.

#### Scenario: Supported upgrade
- **WHEN** a binary opens a database created by an older supported release
- **THEN** migrations run once, retain user data, and update the schema version only after success

#### Scenario: Newer unsupported database
- **WHEN** an older binary encounters a database schema newer than it supports
- **THEN** it refuses writes and explains how to upgrade or restore instead of attempting a downgrade

#### Scenario: Interrupted or failed migration
- **WHEN** a migration is cancelled or fails before commit
- **THEN** the schema version, user records, and object references remain at the previous valid state and the pre-migration snapshot remains readable

#### Scenario: Concurrent migration open
- **WHEN** two processes open one older supported database at the same time
- **THEN** one migration coordinator applies each migration once while the other process waits or fails safely without partial schema changes

### Requirement: Local query interface
The library SHALL expose one typed query interface to Cobra, Bubble Tea, and MCP for account, article, album, job, export, and storage views.

#### Scenario: Shared query semantics
- **WHEN** the same article filters are requested through Cobra and Bubble Tea
- **THEN** both adapters return the same records, ordering, and totals from the shared library query

### Requirement: Backup and restore
The system SHALL create portable backups containing a manifest, SQLite snapshot, referenced objects, configuration excluding unrecoverable keychain secrets by default, and checksums; restore SHALL validate the archive before changing live state.

#### Scenario: Verified backup
- **WHEN** a user creates a full backup
- **THEN** the archive can be verified independently and reports included profiles, data counts, byte size, and omitted secrets

#### Scenario: Failed restore validation
- **WHEN** a restore archive has a missing object or invalid checksum
- **THEN** live state remains unchanged and the system reports every detected validation failure

#### Scenario: Successful restore round trip
- **WHEN** a verified backup is restored into a clean destination
- **THEN** profile identities, table counts, object hashes, local query results, and cached-data exports match the source manifest subject to the chosen conflict policy

#### Scenario: Restore interruption
- **WHEN** restore is interrupted before the staged commit or a commit verification fails
- **THEN** no partially restored database, object tree, or configuration becomes live, and staging or rollback files can be safely removed

#### Scenario: Restore conflict
- **WHEN** the archive conflicts with an existing profile identity or name
- **THEN** the explicit `refuse` policy leaves live state unchanged or the explicit `rename` policy rewrites all profile-owned records consistently and reports the resolution

#### Scenario: Unsafe backup archive
- **WHEN** an archive contains path traversal, absolute paths, duplicate entries, symlink entries, undeclared files, invalid checksums, or entries beyond documented bounds
- **THEN** verification and restore reject it before live state changes

### Requirement: Garbage collection and retention
The system SHALL identify unreferenced objects, expired debug captures, completed-job logs, and obsolete temporary files, present a dry-run summary, and require explicit confirmation before deletion.

#### Scenario: Garbage-collection dry run
- **WHEN** a user runs garbage collection without confirmation
- **THEN** no data is deleted and the result lists reclaimable counts and bytes by category

### Requirement: Integrity checking and repair
The system SHALL check database invariants, foreign references, object presence and hashes, migration state, and export manifests, and SHALL separate automatically repairable conditions from data-loss risks.

#### Scenario: Missing shared resource
- **WHEN** an article references a resource object that is absent
- **THEN** the check marks the article incomplete and offers redownload rather than deleting the article

### Requirement: Legacy Web-data import
The system SHALL support importing a versioned export produced from the legacy Dexie/Web application, including accounts, articles, HTML, metadata, comments, replies, resource maps, and resources when present.

#### Scenario: Import legacy browser export
- **WHEN** a user supplies a valid legacy export package
- **THEN** the system validates the package, maps records into the local schema, deduplicates objects, and produces a reconciliation report

#### Scenario: Legacy import conflict
- **WHEN** an imported record conflicts with a locally newer record
- **THEN** the system follows a documented merge policy and records the decision without silently overwriting newer local data

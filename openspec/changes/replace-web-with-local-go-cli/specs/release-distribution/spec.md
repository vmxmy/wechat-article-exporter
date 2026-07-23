## ADDED Requirements

### Requirement: Native release matrix
Every stable release SHALL publish `CGO_ENABLED=0` archives for macOS arm64/amd64, Linux arm64/amd64, and Windows amd64, with deterministic target metadata, exact-version binaries, per-target CycloneDX SBOMs, and one checksum manifest covering every published asset.

#### Scenario: Verify a release asset
- **WHEN** a user downloads an archive, its SBOM, and `checksums.txt` from the same verified release tag
- **THEN** the checksum matches, the archive contains the expected binary, README, license, and build metadata, and the binary reports the release version and target platform without requiring Go, Node.js, Docker, or a database service

#### Scenario: Mismatched artifact provenance
- **WHEN** an archive checksum, target metadata, version, SBOM component, or release-tag provenance does not match
- **THEN** release automation refuses publication and installation documentation instructs the user not to run the artifact

### Requirement: Upgrade compatibility
Release installation SHALL replace only the executable unless the user explicitly performs a data operation, and every stable release SHALL open or safely migrate every database/configuration baseline promised by the documented compatibility window.

#### Scenario: Upgrade existing local installation
- **WHEN** a user verifies and installs a newer binary over an existing executable
- **THEN** profiles, configuration, SQLite records, object bytes, jobs, exports, and secret references remain available, migrations create recoverable backups, and unsupported downgrade writes are refused

### Requirement: Release gates
Publication SHALL be gated by formatting, vet, static analysis, unit/integration tests, race tests where supported, parser/export fixtures, every promised database baseline, backup/restore/integrity tests, native PTY tests, archive/SBOM metadata checks, and native clean-room smoke tests.

#### Scenario: Supported-platform clean room
- **WHEN** the candidate archive is extracted on a clean native runner
- **THEN** the binary starts without a language runtime, creates profile-isolated local storage, emits a valid status envelope, and passes the platform's release smoke receipt before publication

#### Scenario: Gate is skipped or fails
- **WHEN** any required target, test, fixture, migration baseline, native PTY run, or archive verification is skipped or fails
- **THEN** the stable release is not published

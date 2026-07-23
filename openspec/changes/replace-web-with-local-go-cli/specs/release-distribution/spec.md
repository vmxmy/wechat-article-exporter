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

### Requirement: Candidate-binary execution boundary
Every mandatory clean-room product workflow SHALL execute through the binary extracted from the recorded release archive for the receipt target, SHALL bind its evidence to that binary's SHA-256, and SHALL not require source code, `go run`, a Go or Node.js runtime, Docker, an external database service, an in-process application harness, or architecture emulation.

#### Scenario: Source harness produces a passing result
- **WHEN** a test invokes application packages in-process or launches a source-built helper instead of the extracted candidate binary
- **THEN** the result may be retained as development evidence but cannot satisfy a mandatory stable clean-room workflow

#### Scenario: Wrong binary is executed
- **WHEN** a workflow executor digest differs from the binary digest proven to be extracted from the release archive
- **THEN** receipt validation fails even if the command behavior otherwise appears correct

### Requirement: Platform clean-room receipt
Each supported native target SHALL produce one `wechat-article-clean-room-platform/v1` receipt that records the release identity; host and target tuple; archive, checksum manifest, binary, build metadata, and SBOM provenance; clean-root observations; exact workflow outcomes; bounded command-stream digests; process-tree network evidence; privacy scan results; and a derived summary.

#### Scenario: Valid platform receipt
- **WHEN** every required workflow passes on a clean native host using one verified candidate artifact and every mandatory evidence field is internally consistent
- **THEN** the validator derives a passing platform gate without relying on a producer-supplied `valid` flag

#### Scenario: Receipt contains an unknown or duplicate workflow
- **WHEN** a receipt omits, duplicates, renames, or adds a workflow outside the versioned registry
- **THEN** validation fails closed and reports only a bounded reason code

#### Scenario: Clean roots are pre-populated
- **WHEN** any configured config, data, cache, or state root existed with entries before first launch, resolves outside the isolated work root, or has unsafe permissions
- **THEN** the clean-storage workflow fails and the receipt cannot pass

### Requirement: Mandatory clean-room workflow registry
The platform receipt SHALL contain exactly one result for each version 1 workflow ID: `install.archive`, `storage.clean-roots`, `migration.legacy-web`, `migration.database-baselines`, `login.qr`, `session.restart-persistence`, `sync.account`, `download.article`, `download.resources`, `export.html`, `export.markdown`, `export.text`, `export.json`, `export.xlsx`, `export.docx`, `export.pdf`, `automation.cobra`, `ui.tui`, `automation.mcp`, `storage.backup-restore`, `offline.local-workflows`, `network.no-retired-domain`, `security.no-receipt-leakage`, and `secrets.platform-persistence`.

#### Scenario: Required workflow is skipped
- **WHEN** a required workflow is marked skipped, not applicable, unexecuted, or absent because a dependency or runner capability is unavailable
- **THEN** the platform gate remains incomplete or failed and stable publication is blocked until it is rerun on a suitable native environment

#### Scenario: Legacy migration uses a normal backup
- **WHEN** migration evidence imports the local backup format instead of the versioned legacy Web archive with its source schema and manifest
- **THEN** the legacy migration workflow fails regardless of whether restore succeeds

#### Scenario: PDF uses a fixture renderer
- **WHEN** PDF evidence is produced by a fake runner or static byte fixture rather than an installed supported Chromium-family browser launched by the candidate binary
- **THEN** the PDF workflow fails

#### Scenario: TUI uses a source test
- **WHEN** TUI evidence comes from a model test or `go test` instead of the extracted binary running in a real native PTY
- **THEN** the TUI workflow fails

### Requirement: Workflow result semantics
Each clean-room workflow SHALL use only `pass`, `fail`, or `skip`; `pass` SHALL mean the workflow executed through the required candidate binary and all workflow-specific assertions passed, `fail` SHALL mean execution or an assertion failed, and `skip` SHALL mean the workflow was not executed and SHALL include a bounded reason code. Mandatory workflows SHALL NOT use `not_applicable`.

#### Scenario: Infrastructure is unavailable
- **WHEN** a native runner lacks a required browser, credential backend or encrypted-vault setup, PTY capability, controlled-account authorization, or network-observation mechanism
- **THEN** the affected workflow is skipped rather than passed, the receipt remains diagnostic, and stable publication is blocked until a suitable environment reruns it

#### Scenario: Independent workflows continue after failure
- **WHEN** one clean-room workflow fails without invalidating the safety of unrelated checks
- **THEN** the runner continues bounded independent workflows, emits a diagnostically complete receipt, exits nonzero, and derives a failing gate

#### Scenario: Development receipt contains skips
- **WHEN** a development or fixture receipt contains one or more skipped mandatory workflows
- **THEN** its derived gate status is incomplete or failed and never pass

### Requirement: Fixture and live evidence separation
Deterministic loopback fixture execution SHALL be identified as fixture evidence and SHALL never satisfy live QR login, live session persistence, live account synchronization, or live article/resource download requirements. Stable clean-room approval SHALL use an explicitly authorized controlled WeChat account for those workflows on every supported native target.

#### Scenario: Fixture mode is relabeled live
- **WHEN** loopback origin overrides, fixture transports, synthetic QR responses, or fixture article payloads are active while a producer labels the workflow live
- **THEN** receipt validation rejects the evidence as fixture substitution

#### Scenario: Controlled live workflow succeeds
- **WHEN** the candidate binary authenticates to the controlled account, survives a process restart, synchronizes the expected bounded dataset, and downloads and validates article content and resources
- **THEN** the live workflows may pass while recording only non-sensitive counts, classifications, and digests

### Requirement: Artifact provenance agreement
The clean-room validator SHALL prove that the candidate binary is an archive member and that the release tag, source commit, archive name and digest, checksum-manifest entry, exact version output, target metadata, build metadata, `CGO_ENABLED=0` value, and per-target SBOM all describe the same release and supported target tuple.

#### Scenario: Cross-compiled artifact runs under emulation
- **WHEN** the host operating system or architecture does not natively match the receipt target or any artifact metadata disagrees with that target
- **THEN** the platform receipt fails native validation

#### Scenario: Checksum or SBOM mismatch
- **WHEN** the archive is absent from the recorded checksum manifest or its SBOM version, target, component, or digest differs from the candidate artifact
- **THEN** the artifact cannot enter product workflow validation

### Requirement: Network and offline proof
Clean-room evidence SHALL observe or enforce network behavior for the complete candidate process tree, including browser subprocesses, SHALL report zero contact with retired project domains, and SHALL run offline local workflows under operating-system-level deny-all egress with zero DNS queries and zero connection attempts.

#### Scenario: In-process transport sees no retired domain
- **WHEN** only an injected HTTP client is observed while subprocess, DNS, redirect, or browser traffic is outside capture scope
- **THEN** network evidence is incomplete and cannot pass the stable gate

#### Scenario: Offline cached operations
- **WHEN** all external sources are unavailable and egress is denied
- **THEN** local query, integrity, cached preview/export, TUI, MCP, backup, verification, and independent restore succeed without any DNS or connection attempt

### Requirement: Receipt privacy and fail-closed validation
Receipts and retained command evidence SHALL exclude QR payloads, cookies, tokens, authorization values, account and article identifiers, article or HTML bodies, raw upstream payloads, absolute user paths, secret digests, and unbounded stdout, stderr, errors, or URLs. Validation SHALL reject unknown fields where the schema requires closure, malformed or non-lowercase digests, inconsistent timestamps or counts, missing redaction evidence, and any leakage finding.

#### Scenario: Receipt contains sensitive content
- **WHEN** a privacy scan finds a session value, QR data, credential, article-body canary, raw HTML body, absolute user path, or sensitive URL query
- **THEN** the receipt is invalid and must not be published as release evidence

#### Scenario: Producer summary claims success
- **WHEN** producer-supplied counts or status claim success but recomputed workflows include a missing, failed, skipped, malformed, or privacy-failing entry
- **THEN** the validator reports a failing gate based on recomputed state

### Requirement: Aggregate five-target receipt set
A stable release SHALL produce one `wechat-article-clean-room-release-set/v1` document referencing exactly one validated platform receipt for each of `darwin/arm64`, `darwin/amd64`, `linux/arm64`, `linux/amd64`, and `windows/amd64`, with all receipts bound to the same release tag, version, source commit, checksum manifest, and release-asset set.

#### Scenario: Complete release receipt set
- **WHEN** all five unique native platform receipts pass and their release identities and referenced receipt digests agree
- **THEN** the aggregate validator derives a passing stable clean-room gate

#### Scenario: Target receipt is missing or duplicated
- **WHEN** any supported target is absent, duplicated, replaced by cross-compilation evidence, or references a different release identity
- **THEN** the aggregate receipt set is incomplete and stable publication remains blocked

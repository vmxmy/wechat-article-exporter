## ADDED Requirements

### Requirement: Common export selection
The export system SHALL accept article selections by URL, account, album, saved query, explicit ID list, or all matching local records and SHALL resolve the selection to a stable manifest before writing output.

#### Scenario: Export a saved query
- **WHEN** a user exports a filtered article query
- **THEN** the manifest records the exact article IDs, ordering, filter summary, format options, and creation time used for the export

### Requirement: Deterministic output naming
The system SHALL support configurable naming templates, invalid-character removal, maximum length, collision handling, and stable fallback names across supported platforms.

#### Scenario: Filename collision
- **WHEN** two selected articles resolve to the same output name
- **THEN** the system applies a deterministic suffix or follows the chosen overwrite policy without silently replacing a different article

### Requirement: HTML export
The system SHALL export normalized article HTML with local assets, preserve supported layout and styles, optionally include comments, and create either per-article directories or a portable batch archive.

#### Scenario: Self-contained HTML article
- **WHEN** all referenced resources are present
- **THEN** the exported `index.html` loads its images, styles, audio, and video from the export directory without network access

#### Scenario: Strict resource export
- **WHEN** strict mode is enabled and a required resource is missing
- **THEN** the article export fails without publishing a falsely complete result

### Requirement: Text and Markdown export
The system SHALL export UTF-8 text and Markdown using the shared processor and SHALL optionally write a front matter or metadata header according to explicit options.

#### Scenario: Markdown batch export
- **WHEN** multiple articles are exported as Markdown
- **THEN** each file has deterministic content and naming and the batch manifest reports all successes, skips, and failures

### Requirement: JSON export
The system SHALL export a versioned JSON schema containing normalized article metadata and optional rendered content, engagement metadata, comments, replies, album data, and provenance.

#### Scenario: Metadata-only JSON
- **WHEN** content and comments are disabled
- **THEN** the JSON omits their payloads while retaining fields needed to identify and reconcile each article

### Requirement: Excel export
The system SHALL export an `.xlsx` workbook with stable columns for account, article, publication, type, status, download state, albums, engagement metrics, and optional rendered content.

#### Scenario: Large Excel export
- **WHEN** the selection is large
- **THEN** rows are streamed or bounded so the process does not require holding every article body in memory simultaneously

### Requirement: DOCX export
The system SHALL produce valid `.docx` files with headings, paragraphs, links, images, lists, tables, quotes, supported media references, metadata, and optional comments.

#### Scenario: DOCX validation
- **WHEN** a DOCX export completes
- **THEN** the package passes structural validation and opens in at least the documented supported office applications

### Requirement: PDF export
The system SHALL provide a high-fidelity PDF path through a locally installed supported Chromium-family browser and SHALL clearly report browser discovery and rendering failures.

#### Scenario: Local browser available
- **WHEN** a supported browser is found
- **THEN** the system renders the normalized self-contained HTML locally and writes a PDF without sending article content to a project-operated service

#### Scenario: Browser unavailable
- **WHEN** PDF is requested but no supported browser is available
- **THEN** the system returns an actionable dependency error and does not upload content or silently substitute a lower-fidelity renderer

### Requirement: Atomic and safe output
Exports SHALL write through temporary paths, fsync or close successfully before rename, support fail/skip/replace collision policies, prevent path traversal, and clean abandoned temporary output.

#### Scenario: Export interrupted before commit
- **WHEN** the process stops while writing an article file
- **THEN** the prior destination remains intact and partial output is clearly marked or removed on the next cleanup

### Requirement: Export provenance
Every batch export SHALL include or record a manifest with application version, schema version, format options, source article IDs and hashes, missing resources, warnings, output checksums, and completion status.

#### Scenario: Verify an export
- **WHEN** a user verifies a prior export manifest
- **THEN** the system detects changed or missing output files and reports affected articles

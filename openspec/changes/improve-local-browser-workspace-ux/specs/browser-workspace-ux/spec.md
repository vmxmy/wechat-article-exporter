## ADDED Requirements

### Requirement: Task-oriented workspace navigation and overview

The browser workspace SHALL organize navigation and overview content around user tasks rather than adapter internals. It SHALL present Home, Content, Work, and System destinations; expose login/session control globally; and recommend the next useful action from authentication, account, article, export, and failed-job state. It SHALL describe the workspace as local-only rather than globally read-only when mutations are available.

#### Scenario: Guide a first-time unauthenticated user

- **WHEN** a user opens the workspace without an authenticated session
- **THEN** the overview SHALL present login as the primary next action and SHALL explain that the workspace and data remain on the local device

#### Scenario: Guide a user with an empty library

- **WHEN** a user is authenticated but has no saved account or local article
- **THEN** the overview SHALL recommend adding an account and then synchronizing content instead of prioritizing runtime diagnostics

#### Scenario: Continue useful work

- **WHEN** local articles or failed jobs exist
- **THEN** the overview SHALL prioritize browsing/exporting available articles and inspecting failed work as applicable

#### Scenario: Use a compatible legacy route

- **WHEN** a user opens a previously supported login or saved-query deep link
- **THEN** the workspace SHALL render a compatible destination or an explicit client-side redirect without losing session authorization

### Requirement: Human-readable resource identity

The workspace SHALL use names, aliases, titles, and localized labels as the primary identity shown in normal workflows. Stable internal IDs SHALL continue to identify API actions but SHALL only be shown in an explicit technical detail, copy affordance, or explained fallback when no display label exists.

#### Scenario: Select an account by name

- **WHEN** a user filters, exports, or starts an operation for an account
- **THEN** the control SHALL support bounded name/alias search, display a human-readable account option, and submit the selected stable account ID

#### Scenario: Select an album by name

- **WHEN** a user filters, traverses, or exports an album
- **THEN** the control SHALL support bounded album-name search, display the owning account name, and submit the stable album and account IDs

#### Scenario: Show account ownership in a resource list

- **WHEN** an article, album, credential, or related resource has an associated account
- **THEN** its normal presentation SHALL show the account name or explained fallback rather than an unexplained account ID

#### Scenario: Access an exact identifier for diagnostics

- **WHEN** a user opens technical details for a resource or job
- **THEN** the full stable identifier SHALL be available to inspect and copy without becoming the primary visible label

### Requirement: Layered article discovery and saved views

The article workspace SHALL expose keyword, account, publication date range, and state as common filters and place other supported criteria in a discoverable more-filters region. Applied criteria SHALL have a human-readable summary with individual removal and clear-all controls. Saved queries SHALL be created and edited through the visual filter model, with raw JSON available only in an explicit advanced mode.

#### Scenario: Use common article filters

- **WHEN** a user opens the article page
- **THEN** keyword, account, date range, and state controls SHALL be immediately available without exposing all advanced fields

#### Scenario: Apply an advanced filter

- **WHEN** a user opens more filters and selects an interaction metric, content flag, author, album, or message type
- **THEN** the result request SHALL use the supported query semantics and the active condition SHALL appear in the filter summary

#### Scenario: Enter a publication range

- **WHEN** a user chooses a publication start or end
- **THEN** the workspace SHALL provide localized date/date-time controls and SHALL NOT require manual RFC3339 input

#### Scenario: Save and understand a query

- **WHEN** a user saves the current visual filters
- **THEN** the saved view SHALL display a localized readable summary and SHALL be reusable without requiring raw JSON editing

#### Scenario: Use advanced query JSON

- **WHEN** an expert explicitly enables the saved-query technical mode
- **THEN** the workspace SHALL allow inspection or editing of raw JSON with validation while preserving a human-readable summary

### Requirement: Contextual resource actions

Selecting resources SHALL create a contextual action surface adjacent to the resource results. It SHALL show selected count, frequent valid actions, and a more-actions disclosure; hide or minimize unavailable action panels when no object is selected; and separate dangerous actions from routine actions. Single-resource details SHALL open without discarding list context.

#### Scenario: Select resources for a bulk action

- **WHEN** a user selects one or more compatible rows or mobile resource items
- **THEN** a sticky contextual action bar SHALL expose the selection count and valid frequent actions while the user scrolls

#### Scenario: View a single resource detail

- **WHEN** a user invokes detail or preview for one resource
- **THEN** the workspace SHALL open a side panel, row expansion, or equivalent contextual detail and SHALL preserve the list query and selection

#### Scenario: Keep dangerous actions separate

- **WHEN** a selection supports both routine and destructive operations
- **THEN** destructive operations SHALL be visually and semantically separated and SHALL preserve their existing exact scoped confirmation

#### Scenario: Discover and save an account

- **WHEN** a user starts account discovery
- **THEN** discovery and save fields SHALL appear in a dedicated entry flow or drawer rather than permanently consuming the account list layout

### Requirement: Guided export workflow

The browser export workflow SHALL use three ordered stages: select scope, select format and permitted options, and confirm an authorized destination before starting the persistent export job. Normal scope selection SHALL use human-readable resources or current query state rather than requiring raw IDs. The safe default destination SHALL be usable without manual path entry.

#### Scenario: Export with the default destination

- **WHEN** a user accepts the available default export destination
- **THEN** the workspace SHALL allow progression without typing a path while the server still authorizes an opaque directory capability

#### Scenario: Export a human-readable selection

- **WHEN** a user chooses an account, album, saved view, current matching result, or selected articles
- **THEN** the workspace SHALL summarize that scope with names and counts while submitting stable IDs or the validated query contract

#### Scenario: Configure a format

- **WHEN** a user selects an export format
- **THEN** the workspace SHALL show only the options accepted by that format and SHALL preserve the shared export option allowlist

#### Scenario: Start an export from a narrow viewport

- **WHEN** a user completes export stages at a narrow viewport width
- **THEN** the current stage's primary action SHALL remain reachable without horizontal page scrolling

#### Scenario: Observe the queued export

- **WHEN** the export endpoint returns a persistent job ID
- **THEN** the workspace SHALL show a localized job label and short reference, with the full job ID available in technical details

### Requirement: Categorized settings and protected maintenance

Settings SHALL be organized into General/Preferences, Download/Export Defaults, Credentials, Network/Proxy, Storage Maintenance, and Diagnostics categories. Routine settings SHALL use section hierarchy rather than equal-weight cards. Dangerous maintenance and trust actions SHALL be isolated, explain impact and recoverability, and retain all existing confirmations and secret boundaries.

#### Scenario: Navigate settings categories

- **WHEN** a user opens settings or selects a settings category
- **THEN** the workspace SHALL show a secondary category navigation, one current section, and visible save or operation feedback

#### Scenario: Inspect credentials

- **WHEN** saved credential metadata is listed
- **THEN** the list SHALL show the associated account name, credential kind, localized status, and update time without echoing secret material

#### Scenario: Use a dangerous maintenance action

- **WHEN** a user prepares restore, garbage collection, credential removal, or proxy trust/removal
- **THEN** the workspace SHALL explain scope and recovery, isolate the action from routine settings, and require the unchanged application confirmation contract

### Requirement: Unified human-readable data presentation

The workspace SHALL use one presentation layer for statuses, job kinds, dates and times, durations, bytes, counts, empty values, identifiers, paths, and hashes. Numeric values SHALL use locale formatting, tabular numerals, and numeric alignment. Unknown or unavailable values SHALL render as an em dash and SHALL remain distinguishable from zero. Backend snake_case and raw enums SHALL NOT appear as normal localized labels.

#### Scenario: Compare numeric resource values

- **WHEN** a table or list displays article metrics, file sizes, durations, or counts
- **THEN** numeric values SHALL use locale-aware formatting, consistent units, tabular numerals, and comparison-friendly alignment

#### Scenario: Distinguish unavailable from zero

- **WHEN** a numeric or temporal value is unavailable
- **THEN** the workspace SHALL show an em dash while a known zero value SHALL remain displayed as zero

#### Scenario: Display a status or job kind

- **WHEN** a supported backend state or job kind is rendered
- **THEN** it SHALL use the shared semantic status component and localized human label rather than an untranslated enum

#### Scenario: Display a long technical value

- **WHEN** a path, hash, or stable identifier is needed in a normal summary
- **THEN** the workspace SHALL shorten it without ambiguity and SHALL provide the full copyable value through technical details

#### Scenario: Display article metrics

- **WHEN** article metrics are available
- **THEN** the workspace SHALL render labelled statistics or a definition layout rather than one concatenated sentence

### Requirement: Semantic resource tables and responsive lists

Resource columns SHALL declare a presentation role from primary text, secondary text, numeric, date-time, status, actions, identifier, or description. Desktop layouts SHALL derive width, alignment, truncation, and access to full values from those roles. At narrow widths, resource tables SHALL become readable list items with primary identity, secondary metadata, status, selection, and actions; column-visibility controls SHALL be hidden. Whole-page horizontal overflow SHALL be prohibited except for locally contained intrinsically two-dimensional data.

#### Scenario: Truncate a long desktop title

- **WHEN** a primary article title exceeds the available desktop column width
- **THEN** it SHALL be visually truncated while the complete title remains available by link, focusable disclosure, or accessible tooltip

#### Scenario: Read a long mobile title

- **WHEN** the same title is rendered in a narrow resource list
- **THEN** it SHALL allow up to two readable lines before truncation and retain access to the full title

#### Scenario: Browse resources at 390 pixels

- **WHEN** a user opens an account, article, album, job, or export resource page at a 390-pixel viewport
- **THEN** each resource SHALL be presented as a readable list item and the document SHALL NOT require horizontal scrolling

#### Scenario: View intrinsically two-dimensional data

- **WHEN** a manifest or comparable data set cannot be meaningfully converted to a resource list
- **THEN** horizontal scrolling SHALL be contained within that data region and SHALL NOT overflow the full page

### Requirement: Consistent form controls and progressive technical disclosure

Normal forms SHALL use consistent Astryx controls for selection, files, multi-line text, date/time, numbers, labels, help text, required/optional state, and validation errors. Internal IDs, raw query JSON, fakeid, hashes, paths, leases, and backend diagnostic values SHALL appear only in an explicit advanced or technical disclosure unless required by an existing exact confirmation contract.

#### Scenario: Complete a normal form

- **WHEN** a user enters a date, number, file, option, or multi-line value
- **THEN** the field SHALL use the corresponding accessible Astryx control with consistent label, help, requirement, and error placement

#### Scenario: Open technical details

- **WHEN** a user requests technical details for a resource, query, export, or job
- **THEN** the workspace SHALL reveal relevant exact values with copy affordances and a clear distinction from normal editable fields

#### Scenario: Explain an unavoidable technical term

- **WHEN** a security or protocol concept such as credential trust must be shown
- **THEN** the workspace SHALL provide a localized user-oriented explanation of its consequence rather than only the internal term

### Requirement: Guided state, error, and empty-state behavior

Every major workspace flow SHALL distinguish initial loading, background refresh, local server unavailability, partial data failure, first-use empty state, and filtered-empty state. Empty and error states SHALL provide the safest relevant next action without clearing valid user input or hiding available local data.

#### Scenario: Show a first-use empty state

- **WHEN** an account, article, export, or job collection has never contained data
- **THEN** the workspace SHALL explain the source of that collection and link to adding, synchronizing, exporting, or starting work as appropriate

#### Scenario: Show a filtered-empty state

- **WHEN** data exists but the current filters return no records
- **THEN** the workspace SHALL explain that no results match and offer to edit or clear filters

#### Scenario: Refresh existing data

- **WHEN** a background refresh is in progress and previous safe data exists
- **THEN** the workspace SHALL retain the data, indicate refreshing, and SHALL NOT replace the page with an initial loading state

#### Scenario: Handle a partial failure

- **WHEN** one detail or secondary request fails while primary local data remains available
- **THEN** the workspace SHALL preserve usable data and present a scoped retry or explanation for the failed region

### Requirement: Accessible bilingual responsive operation

All restructured workflows SHALL remain equivalent in English and Simplified Chinese and operable by keyboard and assistive technology. The workspace SHALL have one main heading per route, labelled navigation and mobile drawer controls, visible focus, managed focus for route/dialog/drawer/detail transitions, live status feedback, and no whole-page horizontal overflow at 200 percent zoom for primary workflows.

#### Scenario: Open mobile navigation

- **WHEN** a keyboard or screen-reader user opens the narrow-screen navigation drawer
- **THEN** the drawer SHALL announce its purpose, expose labelled destinations and the current page, trap or manage focus correctly, and return focus when closed

#### Scenario: Complete a selection action by keyboard

- **WHEN** a keyboard user selects resources and invokes a contextual action
- **THEN** selection, action controls, dialogs, and focus restoration SHALL be fully keyboard operable with visible focus

### Requirement: Restorable browser view and workflow state

The workspace SHALL represent safe, shareable article view state in the browser URL and SHALL restore safe export workflow progress after navigation or reload. URL and browser-session persistence SHALL NOT contain credentials, local paths, directory capabilities, exact confirmations, article content, or other secret material.

#### Scenario: Restore an article view from its URL

- **WHEN** a user applies article filters, changes sorting or opens another result page and then reloads, shares the URL, or uses browser back and forward
- **THEN** the workspace SHALL restore the applied query, sort and page, SHALL omit default values from the canonical URL, and SHALL safely discard invalid owned parameters

#### Scenario: Keep article draft state private to the current view

- **WHEN** a user edits filters without applying them, selects rows, or opens a detail dialog
- **THEN** those ephemeral states SHALL remain outside the URL and SHALL NOT become part of a shared article link

#### Scenario: Restore a safe export workflow draft

- **WHEN** a user advances through export scope and format selection and then reloads or navigates through browser history
- **THEN** the workspace SHALL restore the latest valid non-sensitive stage and options, or SHALL fall back to the earliest valid stage when prerequisites are missing

#### Scenario: Exclude export capabilities and secrets from browser persistence

- **WHEN** an export destination is authorized or an exact confirmation is displayed
- **THEN** directory tokens, paths, confirmation strings, credentials and article content SHALL NOT be written to the URL or browser storage and completion SHALL clear the recoverable draft

### Requirement: Protected unsaved settings navigation

The workspace SHALL detect unsaved editable preference changes and SHALL protect them across internal links, programmatic navigation, browser history traversal, reload and tab closure. It SHALL preserve accessible focus behavior and SHALL NOT warn after a successful save or after the draft returns to its saved value.

#### Scenario: Warn before internal navigation with unsaved preferences

- **WHEN** a user changes an editable preference and attempts to leave settings through workspace navigation or browser history
- **THEN** an accessible confirmation SHALL allow the user to stay or discard changes, cancelling SHALL restore focus, and discarding SHALL resume the requested destination exactly once

#### Scenario: Warn before closing or reloading a dirty settings page

- **WHEN** unsaved preferences exist and the user reloads, closes the tab or exits the browser
- **THEN** the workspace SHALL register the native unload warning without relying on custom browser-controlled warning text

#### Scenario: Clear or retain dirty state correctly

- **WHEN** a save succeeds or all edited values are restored to the saved baseline
- **THEN** the workspace SHALL clear the dirty state, while a failed save SHALL preserve both the draft and its dirty state

### Requirement: Complete native form and document semantics

The workspace SHALL expose meaningful native names, input types, input modes and explicit autocomplete behavior for URL, credential and proxy inputs where the underlying control supports them. It SHALL reserve intrinsic QR image space, provide light and dark browser theme colors, protect exact technical values from translation, and use the active application locale for numeric formatting and ongoing-state copy.

#### Scenario: Enter a URL in an appropriate native control

- **WHEN** a user imports an article URL or configures a proxy endpoint
- **THEN** the control SHALL expose a stable native name and URL-appropriate type/input behavior without abandoning the shared Astryx field presentation

#### Scenario: Enter write-only credential or authorization values

- **WHEN** a user fills a credential or proxy authorization form
- **THEN** every field SHALL expose a stable native name and an explicit autocomplete policy that does not misrepresent imported WeChat material as an ordinary website login

#### Scenario: Render stable document and technical metadata

- **WHEN** the QR code, browser chrome, or an exact technical value is rendered
- **THEN** the QR image SHALL declare intrinsic dimensions, light and dark theme colors SHALL match the workspace canvas, and identifiers, hashes, paths, tokens and confirmations SHALL be marked as non-translatable

#### Scenario: Format copy with the active language

- **WHEN** the application formats byte/count values or displays an ongoing check or placeholder example
- **THEN** formatting SHALL use the selected application locale and English and Simplified Chinese copy SHALL follow the same ellipsis convention

#### Scenario: Switch language

- **WHEN** a user changes between English and Simplified Chinese
- **THEN** navigation, statuses, filters, forms, empty states, confirmations, and technical-detail labels SHALL update with equivalent meaning

#### Scenario: Zoom a primary workflow

- **WHEN** a user zooms a primary workspace flow to 200 percent at a supported desktop viewport
- **THEN** controls and content SHALL reflow without requiring whole-page horizontal scrolling or hiding the primary action

### Requirement: Local architecture and release constraints remain intact

The UX implementation SHALL remain an embedded React presentation adapter over the shared application and bounded `/api/v1` contracts. It SHALL preserve loopback session security, file and secret boundaries, exact confirmations, server pagination, static asset embedding, absence of remote runtime dependencies, and compatibility with Cobra, Bubble Tea, and stdio MCP data semantics.

#### Scenario: Execute a human-readable selection

- **WHEN** a user chooses a named account, album, article, export, credential, or job action
- **THEN** the browser SHALL invoke the shared application operation using its stable identity and SHALL NOT implement the domain action in client code

#### Scenario: Build the release binary

- **WHEN** release verification builds and extracts the CLI without Node.js installed at runtime
- **THEN** the human-readable responsive workspace assets SHALL be served from the binary and SHALL pass the established embedded-asset and size checks

#### Scenario: Observe workspace network traffic

- **WHEN** representative redesigned workflows run under request observation
- **THEN** browser application traffic SHALL remain same-origin loopback and SHALL NOT load a CDN, remote font, analytics endpoint, or retired project domain

#### Scenario: Preserve a protected operation

- **WHEN** a redesigned flow performs a file, secret, restore, deletion, garbage-collection, proxy-trust, or output-opening operation
- **THEN** the existing authorization, staging, opaque-capability, path-validation, redaction, and exact-confirmation requirements SHALL remain enforced

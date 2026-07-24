## 1. Shared presentation foundation

- [x] 1.1 Inventory every normal-flow occurrence of raw IDs, fakeid, RFC3339, JSON, snake_case enums, paths, hashes, duplicate local/read-only badges, and page-specific formatters. See [the browser workspace humanization inventory](../../../docs/release/browser-ux-humanization-inventory.md) for the surface-by-surface findings, formatter consolidation targets, responsive gaps, and shared-file integration order.
- [x] 1.2 Add shared locale-aware formatters for status, job kind, date/time, duration, bytes, counts, empty values, identifiers, paths, and hashes with unit tests.
- [x] 1.3 Add shared semantic status and technical-details components with copyable exact values and accessible labels.
- [x] 1.4 Define resource column roles and derive alignment, truncation, responsive metadata, and accessible full-value behavior from them.
- [x] 1.5 Add shared PageHeader, EmptyState, FilterBar/MoreFilters, ActiveFilterSummary, SelectionActionBar, DetailPanel, and mobile resource-row patterns.
- [x] 1.6 Standardize Astryx field wrappers for selector, file, text area, date/time, number, help, required/optional, and error behavior while preserving CSS layer order.

## 2. Human-readable Workspace and API projections

- [x] 2.1 Audit existing Workspace DTOs and list endpoints for account/album display names, query summaries, job labels, and credential account names.
- [x] 2.2 Add only the bounded, additive Workspace projections or selector endpoints required for searchable account and album options.
- [x] 2.3 Preserve stable IDs for every action contract and add explained fallback behavior when a display name is unavailable.
- [x] 2.4 Extend API contract tests for pagination bounds, additive response compatibility, display-name projection, unknown-name fallback, and secret/internal-field non-disclosure.
- [x] 2.5 Extend application tests proving name projections derive from local profile data without changing persistence or cross-adapter semantics.

## 3. Navigation, overview, and session

- [x] 3.1 Reorganize navigation into Home, Content, Work, and System groups with labelled destinations.
- [x] 3.2 Move login/logout/account switching into a global session control while keeping the legacy login route compatible.
- [x] 3.3 Replace global read-only wording with accurate local-only/privacy wording and remove duplicate per-page badges.
- [x] 3.4 Implement overview next-action logic for unauthenticated, no-account, no-article, article-ready, and failed-job states.
- [x] 3.5 Add desktop, narrow-screen, keyboard, and legacy-deep-link Playwright coverage for navigation and overview behavior.

## 4. Accounts and albums

- [x] 4.1 Move account discovery/save/import into a dedicated entry flow or accessible drawer instead of a permanent list panel.
- [x] 4.2 Add searchable name/alias account selectors for filters and actions while submitting stable account IDs.
- [x] 4.3 Add searchable album selectors showing album name and owning account name while submitting stable album/account IDs.
- [x] 4.4 Replace account IDs with account names in album and credential normal presentations, retaining technical details for exact IDs.
- [x] 4.5 Add sticky contextual account/album selection actions with dangerous actions separated and exact confirmations unchanged.
- [x] 4.6 Add browser tests for discovery, name selection, duplicate-name disambiguation, sync/traversal, detail, and deletion confirmation.

## 5. Article discovery and saved views

- [x] 5.1 Reduce the default article filters to keyword, account, localized date range, and state.
- [x] 5.2 Move all remaining supported conditions into More Filters using localized selectors, date/time inputs, numeric inputs, and message-type multi-select.
- [x] 5.3 Add active-filter summaries with individual removal and clear-all behavior.
- [x] 5.4 Distinguish first-use empty article libraries from filtered-empty result sets and provide the appropriate sync or clear-filter action.
- [x] 5.5 Integrate saved queries into article filters with visual editing and human-readable summaries.
- [x] 5.6 Keep raw query JSON in an explicit validated technical mode and preserve the legacy saved-query deep link.
- [x] 5.7 Present article account names, linked/previewable titles, labelled metrics, localized statuses, and accessible full-title disclosure.
- [x] 5.8 Add sticky article selection actions and contextual detail without losing query, page, or selection state.
- [x] 5.9 Extend Playwright coverage for common/advanced filters, saved views, title access, selection actions, empty states, and query export handoff.

## 6. Export workflow and jobs

- [x] 6.1 Refactor export into scope, format/options, and destination/confirmation stages with per-stage validation.
- [x] 6.2 Replace raw account, album, article, and query ID entry with named selectors, current result handoff, and article selection/search.
- [x] 6.3 Make the authorized default output destination the no-input default while preserving opaque directory capabilities and server path checks.
- [x] 6.4 Render only the option controls valid for the selected export format and preserve the server allowlist.
- [x] 6.5 Add a narrow-screen persistent primary action for the current export stage.
- [x] 6.6 Humanize job labels, kinds, statuses, counts, durations, failures, and short references while retaining full IDs in technical details.
- [x] 6.7 Open single-job details contextually and keep only engine-permitted actions visible.
- [x] 6.8 Add browser tests for all export stages/formats, default destination, job handoff, failure guidance, technical ID copy, and exact output-opening confirmation.

## 7. Settings information architecture

- [x] 7.1 Split settings into General/Preferences, Download/Export Defaults, Credentials, Network/Proxy, Storage Maintenance, and Diagnostics.
- [x] 7.2 Add accessible secondary settings navigation with current-section and save/operation feedback.
- [x] 7.3 Replace equal-weight card grids with section hierarchy and reserve cards for independent status, warning, permission, or dangerous-action boundaries.
- [x] 7.4 Humanize credentials, proxy trust, integrity, GC, diagnostics, paths, hashes, sizes, and statuses without exposing secrets.
- [x] 7.5 Isolate dangerous maintenance actions, explain scope/recovery, and preserve every existing exact confirmation and terminal restore behavior.
- [x] 7.6 Add Playwright coverage for category navigation, credential account names, proxy explanations, maintenance confirmations, diagnostics, and partial failures.

## 8. Responsive and accessible resource presentation

- [x] 8.1 Convert account, article, album, job, and export tables into semantic mobile resource lists at the narrow breakpoint using shared data/action state.
- [x] 8.2 Hide column-visibility controls in mobile list mode and contain horizontal scrolling only within intrinsically two-dimensional data regions.
- [x] 8.3 Add a labelled mobile navigation drawer that exposes the current page and restores focus on close.
- [x] 8.4 Implement desktop one-line and mobile two-line title truncation with keyboard/focus access to the full title.
- [x] 8.5 Verify sticky action bars, detail panels, dialogs, route transitions, and live status messages have logical keyboard focus behavior.
- [x] 8.6 Extend accessibility E2E for keyboard-only flows, screen-reader names/landmarks, 390px layouts, and 200-percent zoom without whole-page overflow.

## 9. Localization and state consistency

- [x] 9.1 Expand typed English and Simplified Chinese catalogs for every new navigation, filter, status, empty-state, technical-detail, and settings label.
- [x] 9.2 Add exhaustive supported-enum localization with safe unknown-value fallbacks and tests preventing normal snake_case rendering.
- [x] 9.3 Standardize loading, refreshing, first-use empty, filtered-empty, partial error, full unavailable, success, and retry behavior across routes.
- [x] 9.4 Verify language switching updates the complete active route without losing filters, selection, wizard progress, or focus context.

## 10. Verification, documentation, and release

- [x] 10.1 Run front-end unit tests and checks, sanitized desktop/mobile Playwright, real-Go browser E2E, fresh build, asset sync/verification, and release-size gate. Final evidence: 46 unit tests, 58 sanitized Chromium flows, one real-Go browser flow, and 202,346 bytes of initial JavaScript gzip under the 256,000-byte budget.
- [x] 10.2 Run full Go tests, vet, and trimmed CLI build after all additive Workspace/API projections are complete. Final evidence: unit/integration and race suites, vet, staticcheck, and a 27 MiB `CGO_ENABLED=0 -trimpath` binary all passed.
- [x] 10.3 Verify representative redesigned workflows make only same-origin loopback requests and preserve security headers, CSRF, file, secret, and confirmation boundaries.
- [x] 10.4 Update the browser workspace guide, capability matrix, API contract, screenshots, and release verification evidence for the new information architecture.
- [x] 10.5 Run strict OpenSpec validation and record the final requirement/scenario/task counts and any intentionally deferred phase items. Final inventory: 12 requirements, 49 scenarios, 60 completed tasks, and no deferred phase items; all three active local-product changes validate in strict mode.

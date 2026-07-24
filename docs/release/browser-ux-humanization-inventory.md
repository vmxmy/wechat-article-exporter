# Browser workspace humanization inventory

Status date: 2026-07-24. This inventory is the implementation baseline for
OpenSpec change `improve-local-browser-workspace-ux`, task 1.1. It records
normal-flow presentation that still exposes adapter or storage vocabulary and
the shared presentation behavior that must replace it.

## User-visible internal vocabulary

| Surface | Current exposure | Required replacement | Technical fallback |
| --- | --- | --- | --- |
| Article filters | Account ID, album ID, RFC3339 date strings, comma-separated message type numbers | Searchable named account/album selectors, localized date controls, localized multi-select | Exact IDs and serialized query in Advanced technical details |
| Album filters | Account ID text input | Searchable account selector showing name and alias | Selected stable account ID in Technical details |
| Export scope | Account ID, album ID, album-ID list, article-ID list, raw matching-query JSON | Named resource selectors, selected/current-result handoff, visual saved query | Raw IDs and query JSON in explicit advanced mode |
| Saved queries | Raw query JSON as the primary editor and backend keys in the summary | Visual article-filter editor and localized readable summary | Validated JSON editor in explicit advanced mode |
| Account discovery | `fakeid` presented as a normal required field | Discovery result selection and account name/alias first | `fakeid` only in technical account details or an explained recovery flow |
| Credential list | `accountId`, raw kind, raw status | Account name, localized kind, semantic status | Exact credential/account IDs in Technical details |
| Jobs | Raw kind/state/count keys and full job IDs in notices/confirmations | Localized task name/state/count summary and short reference | Full job ID and raw state in Technical details; exact confirmation remains unchanged |
| Export records/manifests | Full export ID, raw state/provenance, full SHA-256 and paths | Short reference, semantic status, readable provenance, shortened checksum/path | Full values with copy affordances |
| Proxy and diagnostics | Raw trust, request-class, health, check and error-class enums | Localized consequence-oriented labels and explanations | Raw enums in Technical details |
| Integrity and storage maintenance | Raw issue kind, byte/count sentence fragments, hashes and handles | Semantic issue/status rows and shared byte/count formatting | Exact values in Technical details |

## Duplicate and misleading chrome

| Location | Current behavior | Required change |
| --- | --- | --- |
| Application shell | Global `Beta · read-only` badge despite available mutations | Use accurate local-only/privacy wording in one global location |
| Article page | Repeats a local badge in the page header | Remove duplicate chrome; page header contains title, concise description and primary action only |
| Resource pages | Repeated eyebrow and equal-weight action panels | Keep eyebrows only when they add orientation; actions become contextual to selection |
| Settings | Every capability is an equal bordered card | Add category navigation and reserve cards for warnings, permissions and dangerous operations |

## Page-specific formatter duplication

The following local helpers must migrate to the shared presentation layer:

- Article list/detail date and status helpers in the article feature.
- Resource page date, count, state and saved-query summary helpers.
- Export date, byte, state and provenance helpers.
- Settings date and count/byte sentence helpers.

The shared layer must distinguish missing from zero, use locale-aware numeric
formatting, use tabular numerals for comparable values, localize known enums,
provide a safe unknown-enum fallback, and shorten identifiers without removing
access to the exact value.

## Layout and action coupling

| Surface | Current coupling problem | Target pattern |
| --- | --- | --- |
| Accounts | Discovery/save/edit/sync/delete controls permanently occupy the list page | Dedicated discovery entry/drawer plus contextual selected-account actions |
| Articles | Advanced filters dominate the page and the full action panel remains below the table | Common filters + More filters, active-filter summary, sticky selection action bar, contextual detail |
| Albums | Filter and action panels are separated from selected rows | Named filter selector and sticky selected-album actions |
| Jobs | Action panel and detail are separate blocks below the list | Permitted contextual actions and side/expanded detail |
| Exports | Directory, scope, format, job records, manifest and output actions compete on one page | Three-step creation flow; records and detail remain a separate resource section |
| Settings | Unrelated categories render simultaneously | Secondary category navigation with one current section |

## Responsive gaps

- Account, article, album, job and export data currently remain HTML tables at
  narrow widths; they need semantic resource-list projections.
- Column-visibility controls remain meaningful only for desktop tables and must
  be hidden in mobile list mode.
- The current media query stacks containers but does not define mobile title,
  metadata, status, selection and action priority.
- Long article titles use one-line truncation everywhere; mobile needs a
  two-line limit and keyboard/focus access to the full title.
- Manifest-like intrinsically two-dimensional data may keep a locally contained
  scroller; the document itself must not overflow at 390 px or 200% zoom.

## Shared-file integration order

To avoid parallel implementation conflicts, shared integration is serialized:

1. Establish presentation formatters/components and safe Workspace/API fields.
2. Integrate navigation and application-level wording.
3. Integrate account/album and article features.
4. Integrate export/jobs and settings.
5. Consolidate message-catalog types and translations.
6. Apply global responsive styles and update the shared loopback E2E fixture.
7. Rebuild and synchronize embedded Go assets only after all feature waves pass.

## Baseline verification

The baseline before implementation passed:

```text
webui: 2 test files, 17 tests
webui: theme build, ESLint, TypeScript, Astryx doctor
cli: internal/application, internal/web, internal/app tests
OpenSpec: 4/4 artifacts complete and strict validation successful
```

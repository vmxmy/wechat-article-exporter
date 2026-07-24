# Browser workspace release verification

Status date: 2026-07-24. This document records the release-CI evidence for
the verification classes named in OpenSpec task 6.3 and the browser UX
evidence added by `improve-local-browser-workspace-ux` task 10.4. The
authoritative workflow is [release-cli.yml](../../.github/workflows/release-cli.yml).

## Required verification matrix

| OpenSpec 6.3 requirement | CI job and step | Command(s) |
| --- | --- | --- |
| Go unit and integration tests | `test` / `Unit and integration tests` | `cd cli && go test -count=1 ./...` |
| Go race tests | `test` / `Race tests` | `cd cli && CGO_ENABLED=1 go test -count=1 -race ./...` |
| Go vet | `static` / `Vet` | `cd cli && go vet ./...` |
| Staticcheck | `static` / `Staticcheck` | `cd cli && go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 -checks='SA*,S1*,QF*' ./...` |
| Front-end lint, typecheck, and Astryx integration check | `webui` / `Verify browser workspace and checked-in Go assets` | `cd webui && pnpm run check` |
| Front-end unit tests | `webui` / `Verify browser workspace and checked-in Go assets` | `cd webui && pnpm run test` |
| Fresh front-end build and embedded-asset integrity | `webui` / `Verify browser workspace and checked-in Go assets` | `cd webui && pnpm run build && pnpm run verify:go-assets` |
| Embedded initial-bundle size budget | `webui` / `Verify browser workspace and checked-in Go assets` | `cd webui && pnpm run check:release-size` |

`pnpm run check` is intentionally a composite front-end gate: it runs the
generated-theme build, ESLint, TypeScript typechecking, and `astryx doctor`.
`pnpm run build` also verifies the Vite output before the checked-in
`go:embed` asset tree is compared by `verify:go-assets`.

## Related browser-specific evidence

The same `webui` job additionally runs these independent browser checks after
installing pinned Chromium:

- `pnpm run e2e` in `Sanitized loopback browser E2E`;
- `pnpm run e2e:real` in `Real Go loopback browser E2E`.

The sanitized suite is the targeted UX acceptance layer. It includes
`e2e/navigation.spec.ts`, `e2e/article-ux.spec.ts`,
`e2e/settings-ux.spec.ts`, `e2e/accessibility.spec.ts`, and the browser
workflow coverage in `e2e/workspace.spec.ts`. Together these tests exercise:

- Home/Content/Work/System navigation, global session control, Home next
  actions, legacy `/login` and `/saved-queries` deep links, and mobile drawer
  focus restoration;
- human-readable account/album selectors, common and More article filters,
  active-filter removal, visual saved queries with an explicit technical JSON
  mode, selection actions, and contextual article details;
- staged export scope → format/options → destination, authorization of the
  default opaque destination, format-specific option allowlists, job handoff
  with technical full ID, artifact capability download, and exact output-open
  confirmation;
- categorized settings navigation, credential account-name projection,
  proxy/maintenance explanations, write-only Credential behavior, and exact
  destructive-maintenance confirmation;
- keyboard-only navigation/confirmation, labelled landmarks and controls,
  route focus, 390px resource-list layout, 200% export reflow, bilingual
  category labels, same-origin loopback requests, and no whole-document
  horizontal overflow for the asserted flows.

`pnpm run e2e:real` runs `e2e-real/loopback-server.spec.ts` against a freshly
built Go binary in a temporary portable profile. It verifies one-time
bootstrap token exchange to an HttpOnly local session, token removal from the
address bar, embedded SPA/deep-route delivery, security/cache headers,
preference mutation with the server's CSRF/session checks, and that observed
browser traffic remains on the same random `127.0.0.1` origin. This is the
full browser E2E boundary check; it complements, rather than replaces, the
sanitized UX suite.

The API/application evidence for the additive display projections and
selector bounds is in `TestSelectorAndReadableProjectionAPIContracts`,
`TestArticleSelectorAPIForwardsBoundedQueryAndExposesOnlyHumanReadableOptions`,
`TestWorkspaceSelectorOptionsAreBoundedSafeAndKeepStableIDs`,
`TestWorkspaceArticleOptionsAreBoundedHumanReadableAndKeepStableIDs`, and
`TestMaintenanceCredentialProjectionAddsLocalAccountNamesWithoutSecrets`.
Those tests assert selector pagination bounds, validated article-query
forwarding, stable IDs, unavailable-name fallback flags, display projections,
and absence of secrets/internal fields. The sanitized browser suite additionally
proves remote article searching beyond the initial list page and that changing
an export scope cannot queue an invisible prior selection.
The route-level mapping remains in
[browser API route verification matrix](./browser-api-route-matrix.md).

The `parity-and-retirement` job separately validates every active browser
workspace change with strict mode, including:

- `openspec validate local-browser-workspace --strict`;
- `openspec validate improve-local-browser-workspace-ux --strict`.

For the UX change, the strict OpenSpec inventory is **12 requirements, 49
scenarios, and 60 implementation tasks**. The final local verification recorded
on 2026-07-24 passed the WebUI checks and 46 unit tests, 58 sanitized Chromium
browser flows, the real-Go browser flow, asset synchronization/integrity, and
the 256 KiB initial-JavaScript gzip budget (202,346 bytes); it also passed Go
unit tests, race tests, vet, staticcheck, and the trimmed `CGO_ENABLED=0`
binary build (27 MiB). All three related OpenSpec changes passed
strict validation, and there are no intentionally deferred phase items in this
change.
Sanitized browser tests are the reproducible UI
evidence; screenshots are deliberately generated only as failed-test artifacts
so no runtime token, local account, or local-path image is committed.

## Trigger scope

`Release Go CLI` runs for pull requests targeting `main` or `master` when a
listed CLI, WebUI, OpenSpec, documentation, fixture, sample, or workflow path
changes. It may also be started with `workflow_dispatch`, and it runs for
`wechat-article-v*` tags. The downstream release build waits for the `static`,
`webui`, and cross-platform `test` jobs, among other release gates.

## Boundary of this evidence

This is evidence that CI executes the listed release, targeted UX, and real
loopback browser checks. It does not assert that every browser product
workflow has reached parity with Cobra, TUI, or MCP, nor that browser uploads
are arbitrary. The current workflow-level and product-level limits—including
bounded uploads, opaque directory/artifact capabilities, and exact
confirmations—remain documented in the
[browser capability matrix](./browser-capability-matrix.md).

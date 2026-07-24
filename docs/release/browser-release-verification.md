# Browser workspace release verification

Status date: 2026-07-24. This document records the release-CI evidence for
the verification classes named in OpenSpec task 6.3. The authoritative
workflow is [release-cli.yml](../../.github/workflows/release-cli.yml).

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

The `parity-and-retirement` job separately validates both OpenSpec changes
with strict mode, including `openspec validate local-browser-workspace
--strict`.

## Trigger scope

`Release Go CLI` runs for pull requests targeting `main` or `master` when a
listed CLI, WebUI, OpenSpec, documentation, fixture, sample, or workflow path
changes. It may also be started with `workflow_dispatch`, and it runs for
`wechat-article-v*` tags. The downstream release build waits for the `static`,
`webui`, and cross-platform `test` jobs, among other release gates.

## Boundary of this evidence

This is evidence that CI executes every check class named by OpenSpec task
6.3. It does not assert that every browser product workflow has reached parity
with Cobra, TUI, or MCP. The current workflow-level and product-level limits
remain documented in the [browser capability matrix](./browser-capability-matrix.md).

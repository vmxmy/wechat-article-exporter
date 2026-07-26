# Browser workspace frontend

This directory builds the local browser workspace embedded by the Go release. It is a static React + TypeScript + Vite SPA; it has no external runtime, CDN, font, analytics, or project-operated service dependency.

## Commands

```sh
pnpm install --frozen-lockfile
pnpm run lint
pnpm run typecheck
pnpm run build
pnpm run sync:go-assets
pnpm run verify:go-assets
pnpm exec playwright install --with-deps chromium
pnpm run e2e
```

`pnpm run build` writes fingerprinted files to `dist/assets/` and Vite's embed-facing manifest to `dist/.vite/manifest.json`. `pnpm run sync:go-assets` atomically copies that exact tree to the version-controlled `cli/internal/web/assets/` directory. `pnpm run verify:go-assets` fails when the checked-in embed tree is missing or differs from `dist`; run it after every frontend build before committing assets.

Release builds compile only the committed Go asset tree with `//go:embed`; Node and pnpm are not required by an end user or release archive consumer. CI rebuilds the WebUI from the lockfile and rejects stale generated assets before it builds Go binaries.

`pnpm run e2e` serves the SPA only on `127.0.0.1` and intercepts every API call with deterministic sanitized fixtures. The fixture route blocks non-loopback requests, contains no real WeChat account, article, QR, cookie, or filesystem data, and verifies the login, account/article selection, job control, export, settings/storage, and failure-state journeys.

`pnpm run e2e:real` builds the local Go CLI, starts `wechat-article web --no-open` with a disposable memory-backed portable profile, and opens its one-time URL in Chromium. It performs no API interception: the test verifies bootstrap-token removal, observable HttpOnly/session cookies, embedded SPA deep-route behavior, security headers, same-origin requests, and a real preference save against the temporary profile. It needs Go as well as the pinned Chromium browser, but never contacts WeChat.

## API boundary

The SPA only calls same-origin `/api/v1/*` endpoints through `src/lib/api.ts`. It sends cookies (`credentials: 'same-origin'`) and adds the required same-origin CSRF header for mutations. Domain persistence, WeChat protocol, scheduling, export, and secret handling remain behind the local HTTP adapter.

## Architecture record

See [ADR-0001](docs/adr/0001-local-browser-workspace-frontend.md) for the reproducible-build, accessibility, bundle-budget, and source-layout contract.

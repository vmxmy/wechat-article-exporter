# Browser workspace frontend

This directory builds the local browser workspace embedded by the Go release. It is a static React + TypeScript + Vite SPA; it has no external runtime, CDN, font, analytics, or project-operated service dependency.

## Commands

```sh
pnpm install --frozen-lockfile
pnpm run lint
pnpm run typecheck
pnpm run astryx:doctor
pnpm run build
```

`pnpm run build` writes fingerprinted files to `dist/assets/` and Vite's embed-facing manifest to `dist/.vite/manifest.json`. The Go integration is responsible for copying/embedding that output and failing stale-asset checks.

## API boundary

The SPA only calls same-origin `/api/v1/*` endpoints through `src/lib/api.ts`. It sends cookies (`credentials: 'same-origin'`) and adds the required same-origin CSRF header for mutations. Domain persistence, WeChat protocol, scheduling, export, and secret handling remain behind the local HTTP adapter.

## Architecture record

See [ADR-0001](docs/adr/0001-local-browser-workspace-frontend.md) for the reproducible-build, accessibility, bundle-budget, and source-layout contract.

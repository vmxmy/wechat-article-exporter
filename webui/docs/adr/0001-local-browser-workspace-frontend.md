# ADR-0001: Local browser workspace frontend foundation

## Status

Accepted — 2026-07-23.

## Decision

Build the browser presentation adapter in `webui/` with React 19, TypeScript (strict), Vite, Astryx, TanStack Query, and TanStack Table. `pnpm@10.32.1` is the package manager and `pnpm-lock.yaml` is committed. Exact dependency versions and `pnpm install --frozen-lockfile` make installations reproducible.

The source layout is deliberately small and adapter-oriented:

- `src/app/`: providers, shell, routes, resource rendering;
- `src/features/`: independently testable workspace slices such as the paginated article grid;
- `src/lib/`: same-origin API transport and shared types only;
- `src/i18n/`: English and Simplified Chinese resources;
- `src/styles/`: layer order and app-only styling;
- `src/theme/`: the Astryx generated theme source and its generated static CSS.

The app initializes Astryx `Theme` and `LinkProvider` before navigable content. `src/styles/index.css` fixes CSS order as `reset → astryx → theme`; no remote stylesheet, font, image, or CDN is allowed. System font stacks are used.

TanStack Query owns cache keys, polling, and invalidation for local API resources. TanStack Table controls column visibility, sorting intent, and multi-selection for bounded pages only; it never fetches an entire library. The client is intentionally an HTTP presentation adapter: it does not read SQLite, persist secrets, or implement domain rules.

Vite emits a static `dist/` tree with hashed JS/CSS asset names and `dist/.vite/manifest.json`. Release integration must embed exactly this tree, reject a missing/stale manifest, and remain runnable without Node.js. The normal target is a gzip-compressed initial JavaScript payload below 250 KiB; a release-size check should fail on regression once Go embedding is introduced.

## Accessibility baseline

The shell uses a semantic `main` supplied by Astryx `AppShell`, skip navigation, visible focus, labelled controls, live connection/status announcements, keyboard-operable navigation, and responsive reflow for narrow windows. Color is never the only status indicator. New views must retain these properties and use Astryx contracts before creating custom controls.

## Consequences

The frontend can be constructed and verified independently of the Go HTTP implementation. Its unavailable/unauthorized API states are explicit until the `/api/v1` server seams land. The binary embedding, HTTP security, and runtime lifecycle stay outside this directory by design.

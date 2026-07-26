# ADR-0002: Replace Astryx/StyleX with shadcn (base-nova) on Base UI + Tailwind v4

## Status

Accepted — 2026-07-25. Supersedes [ADR-0001](0001-local-browser-workspace-frontend.md).

## Decision

Replace the Astryx design system and StyleX with the UI layer from
[Kiranism/next-shadcn-dashboard-starter](https://github.com/Kiranism/next-shadcn-dashboard-starter)
(commit `06e83c0b`): shadcn components in the `base-nova` style on
`@base-ui/react@^1.6.0`, Tailwind v4 CSS-first, the Vercel palette (OKLCH) with
`next-themes` dark mode, and `@tabler/icons-react` via the `@/components/icons`
barrel. The app stays a Vite SPA with no Next.js, Node server, Clerk, or remote
OAuth — the constraints from the project `CLAUDE.md` and ADR-0001 still hold.

The port keeps three architectural invariants intact:

1. **Value-first control adapters.** `src/components/controls/*` wrap the shadcn
   primitives but preserve the Astryx `onChange(value)` signature, so feature
   code swapped imports without logic changes.
2. **URL-state layer untouched.** The custom history-index router and
   `browserViewState.ts` still own query/sort/page/selection state; the starter's
   `useDataTable`/`nuqs` layer was not ported. The E2E URL-state contract is
   preserved.
3. **Embedded-asset contract untouched.** `id="astryx-app-shell-main"` (a stable
   id referenced by E2E and skip-link focus logic) is retained verbatim — it is
   now an opaque identifier, not an Astryx dependency.

The source layout is unchanged from ADR-0001 except:
- `src/components/ui/*` — shadcn primitives (Base UI) copied in-repo;
- `src/components/controls/*` — value-first adapters over `ui/*`;
- `src/styles/globals.css` + `app-theme.css` — Tailwind v4 entry, shadcn tokens,
  and aliases that map the prior flat CSS variable names onto the new palette so
  the existing feature CSS keeps working;
- `src/theme/` and the generated `wechat-article-workspace.*` / `theme.css` are
  deleted; there is no build-time theme step.

## Accessibility baseline

The shell still uses a semantic `<main id="astryx-app-shell-main">`, skip
navigation with focus restoration, visible focus, labelled controls, live
connection/status announcements, keyboard-operable navigation, and responsive
reflow. Color is never the only status indicator. Base UI primitives provide the
combobox/listbox/dialog focus management; the typed-confirmation and detail
dialogs stop Escape propagation so an ancestor dialog does not also close.

## Consequences

The initial gzip budget remains ≤256 KiB (`check:release-size`). The build no
longer runs `theme:build`; `pnpm run build` is `typecheck → vite build →
verify:assets`. Dependencies `@astryxdesign/*`, `@astryxdesign/cli`, and
`@stylexjs/stylex` are removed.

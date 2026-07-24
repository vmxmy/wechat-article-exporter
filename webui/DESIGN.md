# WeChat Article Local Workspace Design System

## 1. Visual theme and atmosphere

The browser UI is a local-first professional workspace: quiet, exact, and operational. It should feel like a dependable desktop tool rather than a hosted SaaS dashboard. The visual memory is a cool paper-like canvas, a compact command rail, and crisp data surfaces that gain depth only when interaction or hierarchy requires it.

## 2. Color palette and roles

- Canvas: cool off-white/charcoal body background; never pure white or pure black.
- Surface: neutral content surfaces one luminance step above the canvas.
- Primary text: off-black or soft near-white.
- Secondary text: cool neutral with readable contrast.
- Accent: restrained workspace blue; used for primary actions, focus, links, and selection only.
- Success, warning, and danger colors are semantic and never decorative.
- Selection uses a full-row tint and complete inset ring; never a left-side color bar.

## 3. Typography rules

- Chinese UI uses the native CJK stack: PingFang SC, Hiragino Sans GB, Microsoft YaHei, Noto Sans SC.
- Western glyphs prefer Avenir Next/Segoe UI where available; code and identifiers use SFMono-Regular/Menlo/Consolas.
- Weights are limited to 400, 500, and 600.
- Page title: fluid 28–34 px, 600 weight, balanced wrapping.
- Section title: 16–19 px, 600 weight.
- Body: 14–16 px with 1.55–1.7 line height.
- Metadata: 12–13 px, secondary color.
- Dates, counts, byte sizes, and pagination use tabular numerals.
- No italics and no negative tracking on Chinese copy.

## 4. Component styling

- Buttons and inputs use a 6 px functional radius.
- Standard content surfaces use an 8 px radius; featured task panels use 12 px.
- Cards are not the default grouping mechanism. Prefer spacing, hairlines, and shared surfaces.
- Elevated surfaces use a Vercel-inspired shadow stack: a 1 px shadow ring, 2 px soft lift, and an 8 px ambient shadow with negative spread.
- Tables use sticky headers, quiet row dividers, full-row hover, focus-within, and selected states.
- Status elements stay compact and semantic; routine success/loading copy does not permanently consume layout space.

## 5. Layout principles

- Desktop content width is capped at 1280 px and aligned to one consistent grid.
- Main content uses 24–32 px desktop gutters and 16 px mobile gutters.
- The header is a compact command rail integrated with the page canvas.
- The home page has one primary task panel followed by a compact workspace facts strip.
- Data pages prioritize the title column and compress metadata, numeric, date, and status columns to content width.
- Settings use persistent local navigation on desktop and horizontal overflow navigation on narrow screens.

## 6. Depth and hierarchy

- Level 0: page canvas and navigation.
- Level 1: shadow-ring bounded tables and functional panels.
- Level 2: the current primary task, sticky selection bar, menus, and dialogs.
- Only one Level 2 focal surface is allowed per page.
- Borders remain low contrast; emphasis comes from type, spacing, surface tint, and complete rings.

## 7. Do and do not

Do:

- Make the local connection state and next action visible within three seconds.
- Keep human-readable names and titles dominant over internal identifiers.
- Preserve explicit empty, loading, error, disabled, and recovery states.
- Use full-surface selection and focus treatments.
- Keep technical detail progressively disclosed.

Do not:

- Add gradients, glassmorphism, neon glow, decorative color, or remote fonts.
- Turn every section into a same-weight card.
- Use left-side vertical bars for selected, warning, or emphasized states.
- Animate high-frequency keyboard or navigation actions.
- Hide task-critical information inside tooltips.

## 8. Responsive behavior

- Below 768 px, asymmetric grids collapse to one column.
- Dense desktop tables switch to the existing semantic mobile resource rows.
- Header actions wrap without horizontal scrolling.
- Sticky action surfaces respect safe-area insets.
- Long titles clamp to two lines on mobile and remain accessible through their full accessible name/title.

## 9. Motion philosophy

- Motion is feedback, not decoration.
- Page content may enter once with 4 px upward travel and opacity over 160 ms using `cubic-bezier(0.23, 1, 0.32, 1)`.
- Row hover and focus transitions run for 100–120 ms.
- Sticky selection surfaces enter over 180 ms.
- Buttons provide a 0.98 press scale when the component implementation permits it.
- Hover motion is gated to fine pointers; reduced-motion removes spatial movement.

## Reference DNA

- From Vercel: 6 px functional radii, 8/12 px surface radii, 1200–1280 px content discipline, and the shadow-as-border stack (`0 0 0 1px`, `0 2px 2px`, `0 8px 8px -8px`).
- From Linear: 13 px metadata, 16 px body rhythm, 8 px base spacing, restrained single-accent usage, and near-invisible structural borders.

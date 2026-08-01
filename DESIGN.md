---
schema: babysit-design/v1
project: reallongnguyen-babysit (babysit dashboard)
product_type: developer tool / operations dashboard
ref: https://getdesign.md/what-is-design-md
tokens:
  colors:
    primary: "oklch(0.58 0.18 268)"
    on_primary: "oklch(1 0 0)"
    secondary: "oklch(0.48 0.012 240)"
    accent: "oklch(0.58 0.18 268)"
    background: "oklch(0.99 0 0)"
    foreground: "oklch(0.14 0.008 240)"
    muted: "oklch(0.97 0.003 240)"
    muted_foreground: "oklch(0.60 0.010 240)"
    border: "oklch(0.94 0.005 240)"
    destructive: "oklch(0.48 0.17 25)"
    ring: "oklch(0.60 0.15 220 / 0.55)"
  brand_scale:
    gray_00: "oklch(0.99 0 0)"
    gray_05: "oklch(0.97 0.003 240)"
    gray_10: "oklch(0.94 0.005 240)"
    gray_15: "oklch(0.90 0.006 240)"
    gray_30: "oklch(0.78 0.008 240)"
    gray_50: "oklch(0.60 0.010 240)"
    gray_65: "oklch(0.48 0.012 240)"
    gray_80: "oklch(0.30 0.012 240)"
    gray_90: "oklch(0.20 0.010 240)"
    gray_95: "oklch(0.14 0.008 240)"
    accent: "oklch(0.58 0.18 268)"
  typography:
    body: { family: "Inter var, Inter, system-ui", subsets: [latin], var: "--font-body" }
    heading: { family: "Inter Display, Inter var, Inter, system-ui", var: "--font-display" }
    mono: { family: "Berkeley Mono, JetBrains Mono, ui-monospace", var: "--font-mono" }
    scale: [11, 12, 13, 14, 18]
    tracking: { display: "-0.018em", title: "-0.011em", body: "0", caption: "0.04em" }
  spacing:
    base: 4
    page: 24
    section: 16
    row: 12
    row_h_dense: 32
    row_h_roomy: 40
    content_max: 960
  layout: { radius: 6, radius_scale: [4, 6, 8, 9999] }
  motion:
    easing: { enter: "cubic-bezier(0.16, 1, 0.3, 1)", exit: "cubic-bezier(0.7, 0, 0.84, 0)", spring: "cubic-bezier(0.5, 1.25, 0.5, 1)" }
    duration_ms: { instant: 80, micro: 120, short: 180, medium: 280 }
---

# DESIGN.md — babysit dashboard

> **Read this before any UI change under `web/`.** Tokens are defined once, in
> [`web/src/styles.css`](web/src/styles.css), as a three-tier cascade:
> primitives (`--p-*`) → semantic (`--surface-*`, `--text-*`, `--border-*`,
> `--accent*`) → status (`--status-<bucket>-*`). Components live in
> [`web/src/components/`](web/src/components/). **Never hardcode a color, radius,
> duration, or spacing value** — reference the CSS variable. **Never rebuild a
> primitive** this file lists in the inventory. Dark theme is not optional:
> every value has a `:root[data-theme="dark"]` counterpart, so styling against
> the variable is what makes a surface theme-correct for free.

## Product Context

The babysit dashboard is the read + control surface over `~/.babysit` state:
tickets, foremen, sessions, decisions, skill telemetry. Its user is a single
developer supervising autonomous Claude Code runs — often watching several
workers at once, often glancing rather than reading. It ships two ways: a static
`file://` snapshot (`data.js`) and an HTTP-served build with a mutation API.

Density beats decoration. The user is scanning for *what changed* and *what
needs me*, so information per pixel is the metric, and every screen should be
legible at a glance from across the desk.

## Aesthetic Direction

**Direction:** Linear-DNA — quiet, dense, monochrome-with-one-accent. Structure
comes from hairline borders and type hierarchy, not from color, shadow, or
cards. Decoration level: minimal.

**Mood:** precise, calm, professional. The UI should feel like an instrument
panel, not a marketing page.

**Anti-patterns (banned):**
- Card shadows for ordinary content — elevation is reserved for popovers and
  modals (`--shadow-popover`, `--shadow-modal`) only.
- Emoji as icons. Use `lucide-react` at 12–16px, `strokeWidth` 1.75.
- Filled/colored buttons for anything but the single primary action on a
  surface.
- Saturated background fills. Status backgrounds cap at chroma ≤ 0.03; the
  *text* carries the saturation.
- Large hero type, gradients, illustrations, marketing copy.
- Hardcoded hex/rgb values, hardcoded px radii, hardcoded transition durations.
- Placeholder-only form fields — every input carries a visible label.

## Typography

One family (Inter) for everything except identifiers. Hierarchy comes from
size + weight + color, never from a second display face.

| Role | Size | Weight | Token |
|---|---|---|---|
| Page title (TopBar) | 18px | 500 | `--tracking-display` |
| Section heading | 11px uppercase | 500 | `--tracking-caption`, `--text-muted` |
| Body / row text | 13–14px | 400 | `--tracking-body` |
| Meta / secondary | 12px | 400 | `--text-secondary` |
| Caption / column head | 11–12px uppercase | 500 | `--tracking-caption`, `--text-muted` |

Identifiers — ticket ids, branches, paths, workspace refs, skill names — are
**always** `var(--font-mono)`. `font-variant-numeric: tabular-nums` is set on
`body`, so numeric columns align without extra work.

## Color

Semantic tokens only; never a primitive (`--p-*`) directly in a component.

- Surfaces: `--surface-bg` (page/rows), `--surface-elevated` (header rows,
  inline chips), `--surface-hover`, `--surface-sunken`, `--surface-nav`.
- Text: `--text-primary` (content), `--text-secondary` (meta),
  `--text-muted` (labels, captions), `--text-nav*` (sidebar).
- Borders: `--border-hairline` (default rule between rows and around frames),
  `--border-emphasis` (header rows, secondary button outline).
- Accent: `--accent` for links, active nav, focus, and the one primary action.
  `--accent-bg-subtle` for accent-tinted fills. Accent is never decorative.
- Status buckets — `backlog | unstarted | started | blocked | completed |
  cancelled` — each with `-bg` and `-text`. Map a raw status to a bucket via
  `web/src/lib/priority.ts` (`STATUS_BUCKET`); never invent a per-status color.

Contrast floor is 4.5:1 for all text on its own surface, in both themes.

## Spacing & Layout

4px base. `--pad-page: 24px` horizontal page padding, `--pad-section: 16px`,
`--pad-row: 12px` (applied as `px-3` inside grid cells). Dense list rows are
32px (`--row-h-dense`); roomy rows 40px. Prose column caps at
`--content-max: 960px`.

Lists are CSS-grid rows (`DenseRow`), not tables: one `grid-template-columns`
string per list, hairline `border-bottom` between rows, one 1px frame with
`--radius-md` around the whole list. Sidebar nav is a fixed 240px column;
ticket detail is `1fr + 280px`, collapsing below 1024px.

Radii: `--radius-sm: 4px` (buttons, chips, inputs), `--radius-md: 6px` (list
frames, panels), `--radius-lg: 8px` (modals), `--radius-pill` (pills only).

## Motion

Transitions are 80–180ms with `--ease-out`; only `background-color`,
`border-color`, `color`, `opacity`, `transform`. Popovers use
`bbs-popover-in` (fast), modals `bbs-modal-in` (spring), route changes
`.animate-fade-in`. **Reduced-motion is already wired:** the duration tokens
collapse to `0ms` under `prefers-reduced-motion: reduce`, so using the tokens
is the whole guard.

## Components Inventory

Import from `web/src/components/<Name>`.

**Layout & chrome**
- ⭐ `Layout` — app shell: sidebar nav, keyboard scope, palette, theme toggle.
- ⭐ `TopBar` — sticky 44px page header: title/breadcrumb, count, `actions` slot.
- `SectionHeader` — 11px uppercase group header, optional count badge,
  collapsible body, right-aligned `action` slot.
- `ProjectSwitcher`, `CommandPalette`, `ShortcutsHelp`, `ThemeToggle`, `Kbd`.

**Data display**
- ⭐ `DenseRow` — the list-row primitive (grid, 32px, hover, focus ring).
  `header` renders the column-head variant. Every list uses this.
- ⭐ `Tag` — text-only label, uppercase 12px. `status="…"` derives tone from
  the bucket; `tone="ok|warn|err|muted|info|accent"` otherwise. No background.
- ⭐ `StatusArc` — 14px SVG, one shape per status bucket, colored by
  `currentColor` from `--status-<bucket>-text`.
- `PriorityDot` — 3-level priority glyph. `BarChart` — inline bars.
- ⭐ `Markdown` — renders artifact markdown into `.md` prose styles.

**Interaction & feedback**
- ⭐ `Button` — `variant="primary|secondary|ghost"`, `size="sm|md"` (24/28px).
  One `primary` per surface, at most.
- ⭐ `EmptyState` — title + body + optional icon and action, for zero-rows.
- ⭐ `ErrorBox` — failure panel with title + body.
- `FiltersPopover` — faceted filter chips over a list.

## Reuse Policy

1. **Reuse before building.** If a component above does the job, use it. A new
   component is a decision to be justified in a design spec, not an accident.
2. **On an existing page, the page wins.** Sibling sections outrank this global
   inventory: match the host page's actual wrappers, headings, and spacing.
   Diverging from local patterns is a `NEW:` flag needing a stated reason.
3. **Tokens, not literals.** No hex, no px radii, no ms durations in component
   code. If a needed value has no token, add the token to `styles.css` first.
4. **Both themes or it isn't done.** Style against semantic variables so
   `data-theme="dark"` is automatic; check any new surface in both.
5. **Icons are `lucide-react`**, 12–16px, `strokeWidth` 1.75, `aria-hidden`
   when decorative. Never emoji.
6. **Identifiers are monospace.** Ticket ids, branches, paths, refs, skill names.
7. **Every interactive element is keyboard-reachable** with a visible
   `--focus-ring` and a real accessible name.
8. **Destructive or state-changing actions state their reversibility** in the
   UI, not just in a tooltip.

## Decisions Log

| Date | Decision | Rationale |
|---|---|---|
| 2026-08-02 | Authored this file by deriving it from the shipping system (`web/src/styles.css` token cascade + `web/src/components/` inventory), not by drafting a new direction. | The dashboard already had a coherent Linear-DNA design system with no DESIGN.md, so `bbs design tokens` returned nothing for its own web app. Codifying what ships — no rebrand, no new tokens. |
| 2026-08-02 | Control state (pause/cancel/assignment) is specified as a separate visual axis from the lifecycle status ladder. | Lifecycle status is a derived rung (`reconcile`); control state is a human override. Rendering both through `StatusArc` would conflate a derived signal with an explicit one. See `tickets/bs-bfq34gq0/design.md`. |

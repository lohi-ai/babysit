# Design System — babysit dashboard

Linear-tier polish for `web/`. This document is the spec for a follow-up
implementation pass after `bs-rlenq5ev`. It assumes the existing token
cascade (oklch primitives → semantic → status), Inter family, status arcs,
priority dots, dense rows, command palette, and theme toggle are already in
place. Everything below is the gap to Linear-tier feel.

## Product context

- **What this is:** dashboard surfacing babysit autopilot state — tickets, decisions, skill events, live status, analytics.
- **Who it's for:** developers running babysit unattended; primary user is the operator at-the-keyboard reading reports.
- **Project type:** internal dashboard, data-dense, keyboard-first.
- **Reference:** [linear.app](https://linear.app). The North Star is *calm density* — every pixel earns its place, motion is whisper-quiet, color is restrained, typography does the heavy lifting.

## Aesthetic direction

- **Direction:** industrial/utilitarian × refined.
- **Decoration level:** minimal — no gradients, no shadows except popovers, no decorative borders.
- **Mood:** code editor, not consumer SaaS. Quiet. Confident. Boring on purpose.
- **The single guiding rule:** *if a pixel doesn't carry information, it shouldn't carry contrast.*

---

# 1. Design system

## 1.1 Color — fix the light-mode sidebar

The current `--surface-nav` is `oklch(0.14)` in *both* themes. That's the
single biggest "this is not Linear" tell. In Linear's light mode the
sidebar is barely-elevated from the page (~`gray-25`), with dark text.
The always-dark sidebar is a Notion/Slack pattern.

**Token changes (light theme):**

```css
/* before */
--surface-nav: var(--p-gray-95);          /* near-black */
--surface-nav-elevated: var(--p-gray-90);
--text-nav: var(--p-gray-30);             /* light gray on dark */
--text-nav-active: var(--p-white);
--border-nav: oklch(0.25 0.010 240);

/* after */
--surface-nav: oklch(0.985 0.002 240);    /* barely off-white */
--surface-nav-elevated: oklch(0.955 0.004 240);
--text-nav: var(--p-gray-65);             /* mid gray */
--text-nav-active: var(--p-gray-95);      /* near-black */
--border-nav: var(--border-hairline);
```

Dark theme keeps the dark sidebar (it's already correct).

## 1.2 Hairline opacity — softer everywhere

Current `--border-hairline: oklch(0.92)` is ~8% on white — too visible.
Linear hairlines read as ~5-6%. Drop to `oklch(0.94)`. In dark mode,
drop from `oklch(0.26)` to `oklch(0.22)`.

## 1.3 Accent — pull in saturation

`oklch(0.65 0.22 260)` is a generic blue-violet. Linear's signature
indigo is `#5E6AD2` — slightly more violet, slightly less saturated. We
don't need to *copy* it, but the current value reads SaaS-default.

```css
--accent: oklch(0.58 0.18 268);     /* indigo with restraint */
--accent-hover: oklch(0.54 0.18 268);
--accent-bg-subtle: oklch(0.96 0.04 268);   /* light selection bg */
```

In dark: `--accent-bg-subtle: oklch(0.28 0.06 268)`.

## 1.4 Status saturation — calm down

Current status backgrounds are `oklch(0.96 0.04..0.05)` — visible color
washes. Linear status backgrounds are nearly invisible (3-4% chroma).
Drop to `oklch(0.97 0.025)`. The *text* tokens stay saturated for
readability. Result: status cells look monochrome at a glance, color
appears on focus/hover.

## 1.5 Typography — the 60% gain

Three changes, all in `styles.css` and a font load in `index.html`:

**Load Inter with feature flags:**

```html
<!-- index.html — replace any current Inter load -->
<link rel="preconnect" href="https://rsms.me/">
<link rel="stylesheet" href="https://rsms.me/inter/inter.css">
```

```css
:root {
  --font-body: "Inter var", Inter, system-ui, sans-serif;
  --font-display: "Inter Display", "Inter var", Inter, system-ui, sans-serif;
  --font-mono: "Berkeley Mono", "JetBrains Mono", ui-monospace, "SF Mono", Menlo, monospace;
}

body {
  font-family: var(--font-body);
  font-feature-settings: "cv11", "ss01", "ss03", "cv02";  /* Inter alternates */
  font-variant-numeric: tabular-nums;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
  text-rendering: optimizeLegibility;
}

/* Tight tracking on every heading — this alone reads as 'Linear' */
h1, h2, h3, h4 { letter-spacing: -0.011em; font-weight: 500; }
h1 { letter-spacing: -0.018em; }

/* Default body line-height is too loose */
body { line-height: 1.5; }
```

**Type scale (replace ad-hoc Tailwind sizes):**

| Token | Size / line / weight / tracking | Use |
|-------|---------------------------------|-----|
| `--type-display` | 18 / 24 / 500 / -0.018em | Page title in top bar |
| `--type-h1`      | 15 / 20 / 600 / -0.011em | Section title (rare) |
| `--type-h2`      | 13 / 18 / 600 / -0.005em | Card / group header |
| `--type-body`    | 13 / 20 / 400            | Default UI text |
| `--type-body-sm` | 12 / 16 / 400            | Tables, dense rows |
| `--type-caption` | 11 / 14 / 500 / +0.04em / uppercase | Section labels |
| `--type-code`    | 12 / 18 / 400 / `--font-mono` | IDs, paths |

The current `text-2xl font-semibold` page titles → become
`--type-display` (18px, not 24px), inline with the top bar (see §2.2).

## 1.6 Spacing — tighter chrome, denser content

Current main padding is `p-8` (32px) with `max-w-6xl` (~1152px). That's
generous SaaS spacing. Linear uses `p-6` (24px) with content max ~960px.

```css
:root {
  --pad-page: 24px;
  --pad-section: 16px;
  --pad-row: 12px;          /* horizontal padding inside dense rows */
  --row-h-dense: 32px;      /* tickets, decisions, skill events */
  --row-h-roomy: 40px;      /* settings rows, top-level lists */
  --content-max: 960px;
}
```

Apply via `Layout.tsx`: `p-6 max-w-[960px]`.

## 1.7 Radius — kill rounded-full

Current `FilterChips` use `rounded-full`. That's social-app shape. Linear
uses 4-6px squircles for everything except avatars.

```css
:root {
  --radius-sm: 4px;       /* chips, buttons, inputs */
  --radius-md: 6px;       /* cards, popovers */
  --radius-lg: 8px;       /* modals */
  --radius-pill: 9999px;  /* avatars, count badges only */
}
```

Replace every `rounded-full` (chips) with `rounded-[4px]` or use the
`--radius-sm` token via inline style.

## 1.8 Motion — calm micro-motion

```css
:root {
  --ease-out: cubic-bezier(0.16, 1, 0.3, 1);
  --ease-in:  cubic-bezier(0.7, 0, 0.84, 0);
  --ease-spring: cubic-bezier(0.5, 1.25, 0.5, 1);
  --dur-instant: 80ms;
  --dur-fast: 120ms;       /* hover, focus */
  --dur-base: 180ms;       /* route transitions, popovers */
  --dur-slow: 280ms;       /* modal open */
}
```

Three rules to apply globally:

1. Every interactive element: `transition: background-color var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out), border-color var(--dur-fast) var(--ease-out);`
2. Route transitions in main content: `<main>` gets a 120ms opacity fade-in on hash change (key on `active` route).
3. Popovers (palette, help): scale-95 → scale-100 + opacity 0 → 1 over `--dur-base`.

No bouncy easings on data rows. Spring is for popovers and modals only.

## 1.9 Iconography — adopt lucide-react

Add `lucide-react` (~30kb gzipped, tree-shaken). All icons stroke-1.5,
size 14/16/20.

```bash
npm i lucide-react
```

| Use | Icon | Size |
|-----|------|------|
| Nav items | `Home`, `Activity`, `ListTodo`, `GitBranch`, `Sparkles`, `Calendar`, `BarChart3` | 16px |
| Filter button | `SlidersHorizontal` | 14px |
| Sort | `ArrowUpDown` | 14px |
| New / + | `Plus` | 14px |
| Empty states | `Inbox`, `SearchX`, `CircleOff` | 32px (muted) |
| Table header glyphs (replacing `·` and `○`) | `Flag`, `CircleDashed` | 12px (muted) |
| Toast / inline error | `AlertCircle`, `AlertTriangle`, `Info` | 14px |

## 1.10 Elevation — popovers only

```css
:root {
  --shadow-popover: 0 1px 2px rgb(0 0 0 / 0.04), 0 4px 12px rgb(0 0 0 / 0.06);
  --shadow-modal:   0 8px 32px rgb(0 0 0 / 0.12);
}
:root[data-theme="dark"] {
  --shadow-popover: 0 1px 2px rgb(0 0 0 / 0.4), 0 4px 12px rgb(0 0 0 / 0.5);
  --shadow-modal:   0 8px 32px rgb(0 0 0 / 0.6);
}
```

No shadows on cards. Cards use hairline border + `--surface-elevated`.

---

# 2. Layout

## 2.1 Sidebar — parity, icons, visual hierarchy

**Width:** 240px (was 224px = `w-56`). Linear uses 240px so the project
switcher's full name fits without truncating.

**Structure (top → bottom):**

```
┌─────────────────────────┐
│  [avatar] Project ▾     │   ← project switcher, 36px row
├─────────────────────────┤
│  · NAVIGATION           │   ← caption, 11px tracked, optional
│  ◯ Home          G H    │   ← icon + label + kbd
│  ◯ Live          G L    │
│  ◯ Tickets       G T    │   ← active = surface-nav-elevated bg
│  ◯ Decisions     G D    │
│  ◯ Skill events  G S    │
│  ◯ Timeline      G M    │
│  ◯ Analytics     G A    │
│                         │
│   (flex spacer)         │
│                         │
├─────────────────────────┤   ← hairline divider
│  ⌘K  Search...          │   ← 28px button, hover bg
│  ?   Shortcuts          │
│  ◐   Light / Dark       │
│  ─                      │
│  babysit v1.11.0        │   ← caption, muted
│  Snapshot 2d ago        │
└─────────────────────────┘
```

Nav items: `paddingX: 10px, paddingY: 6px, height: 28px, gap: 8px,
fontSize: 13px, weight: 500`. Active: `bg: surface-nav-elevated, color:
text-nav-active`. Hover: `bg: surface-nav-elevated alpha 50%`.

The Kbd hint is right-aligned, 11px, `--text-muted`, only visible on
`group-hover` (use Tailwind's `group` utility) — Linear shows shortcuts
on hover, not always.

## 2.2 Top bar — the missing chrome (NEW)

Add a **44px** sticky top bar above the page content. Without it, every
page reads as a static doc.

```
┌───────────────────────────────────────────────────────────────────────┐
│ Tickets · 6                          [⌘ Filters] [Sort] [Options] [+] │
└───────────────────────────────────────────────────────────────────────┘
```

- **Left:** breadcrumb / page title at `--type-display` (18px, weight 500). Format: `<Page>` for index pages, `<Page> / <id>` for detail pages, `<Page> · <count>` when there's a meaningful count.
- **Right:** view-specific action cluster. Default actions: Filters (icon + label), Sort, Options menu, Primary action button. Hidden if not applicable.
- **Style:** `bg: surface-bg`, `borderBottom: 1px solid var(--border-hairline)`, `h: 44px`, `paddingX: 24px`, `display: flex, align-items: center, justify-content: space-between`. **Sticky:** `position: sticky; top: 0; z-index: 10`.

Implementation: extract a `<TopBar>` component that takes
`{ title, count?, breadcrumb?, actions? }`. Each route component renders
its own `<TopBar>` at the top of `children` — Layout just slots them.

## 2.3 Page padding

Drop main padding from `p-8 max-w-6xl` to `p-6 max-w-[960px]`. Page
title is no longer floating in the content area (it's in the top bar),
so the content can start tight against the top.

---

# 3. Shared components

## 3.1 DenseRow — softer hover, hairline only on bottom

```css
.dense-row--body {
  min-height: 32px;
  padding-block: 0;
  border-bottom: 1px solid var(--border-hairline);
  transition: background-color var(--dur-fast) var(--ease-out);
}
.dense-row--body:hover {
  background-color: oklch(from var(--text-primary) l c h / 0.025);
}
.dense-row--body:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: -1px;
  background-color: oklch(from var(--accent) l c h / 0.06);
}
.dense-row--header {
  background-color: var(--surface-bg);    /* same as body — invisible */
  border-bottom: 1px solid var(--border-hairline);
}
.dense-row--header span {
  color: var(--text-muted);
  font: 500 11px/14px var(--font-body);
  letter-spacing: 0.04em;
  text-transform: uppercase;
}
```

The header background change is significant: Linear table headers are
*the same color as the body bg*, separated only by a hairline. The
elevated stripe at the top reads as "this is a card" — we don't want
that.

## 3.2 StatusArc / PriorityDot — keep, breathe

The arc/dot ideas are right. Two adjustments:

- **Container:** 20px wide column (was 24px). The arc itself is 8-10px; the column gives it air.
- **Color source:** keep `currentColor` driven by `bucketTextVar`. Drop saturation by 5-10% so they don't pop in light mode (they already feel right in dark).

## 3.3 StatusPill — replace with `<Tag>` (text-only)

Pills with bg+text feel heavy. Linear uses text-only "tags" — colored
text on transparent bg, optional dot prefix.

```tsx
<Tag tone="started">in_progress</Tag>
// renders: <span class="text-xs" style={{color: 'var(--status-started-text)'}}>● in_progress</span>
```

The `●` is 6px, same color, gives the tag a visual anchor without a
filled bg. For things that *must* have bg (e.g., outcome counts), use
the existing pill style sparingly.

## 3.4 FilterChips — squircle, no border

```css
.filter-chip {
  height: 24px;
  padding-inline: 8px;
  border-radius: var(--radius-sm);   /* 4px, NOT pill */
  border: none;
  background-color: var(--surface-elevated);
  color: var(--text-secondary);
  font: 500 12px/16px var(--font-body);
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  transition: background-color var(--dur-fast) var(--ease-out);
}
.filter-chip:hover { background-color: var(--surface-sunken); }
.filter-chip[aria-pressed="true"] {
  background-color: var(--accent-bg-subtle);
  color: var(--accent);
}
```

Optional: prefix the chip with the facet kind icon (Flag for status, etc).

## 3.5 EmptyState — borderless

Drop the current bordered card. Use icon + copy on plain bg, centered.

```tsx
<div className="flex flex-col items-center gap-3 py-12 text-center">
  <Icon size={32} className="opacity-40" />
  <div>
    <div className="text-sm font-medium">No tickets</div>
    <div className="text-xs mt-0.5" style={{color: 'var(--text-muted)'}}>
      No tickets in this project yet.
    </div>
  </div>
  {action && <Button size="sm" variant="secondary">{action}</Button>}
</div>
```

Particularly: **kill the dashed-border ACTIVE PAIR empty state on Home.**
That dashed box is the most un-Linear element in the app.

## 3.6 ErrorBox / inline error

Current "Ticket not found" red banner is too heavy (saturated bg + border + heading). Replace with inline style:

```tsx
<div className="flex items-start gap-2 py-2">
  <AlertCircle size={14} className="mt-0.5" style={{color: 'var(--status-blocked-text)'}} />
  <div>
    <div className="text-sm font-medium">Ticket not found</div>
    <div className="text-xs" style={{color: 'var(--text-muted)'}}>
      No detail for <code>{id}</code> in this snapshot.
    </div>
  </div>
</div>
```

No bg, no border. Color comes from the icon and the heading weight.

## 3.7 Kbd — refinement

Current `Kbd` is fine. One change: when used in nav (G+letter hint),
wrap in a `group-hover:opacity-100 opacity-0 transition-opacity` so it
only shows on hover. Always-visible kbd hints are visual noise;
hover-only matches Linear.

## 3.8 SectionHeader (NEW shared)

For workflow groups (BACKLOG, IN PROGRESS, etc.) and Analytics sections:

```tsx
<header className="flex items-center gap-2 mt-6 mb-2">
  <ChevronDown size={12} className="opacity-60" />
  <span className="uppercase text-[11px] font-medium tracking-[0.04em]"
        style={{color: 'var(--text-muted)'}}>
    Backlog
  </span>
  <span className="text-[11px] tabular-nums px-1.5 rounded-full"
        style={{
          color: 'var(--text-muted)',
          backgroundColor: 'var(--surface-elevated)',
          minWidth: 18, textAlign: 'center'
        }}>
    {count}
  </span>
</header>
```

Replace the current `<details><summary>` style with chevron + label + count badge.

## 3.9 Button (NEW shared)

```tsx
type ButtonProps = {
  variant?: 'primary' | 'secondary' | 'ghost';   // default: secondary
  size?: 'sm' | 'md';                            // sm: 24px, md: 28px
  icon?: ReactNode;
  children?: ReactNode;
};
```

- **Primary:** `bg: var(--accent), color: white, hover: var(--accent-hover)`.
- **Secondary:** `bg: var(--surface-elevated), color: var(--text-primary), border: 1px solid var(--border-hairline)`.
- **Ghost:** `bg: transparent, hover: var(--surface-elevated)`.
- All radii `--radius-sm` (4px). All transitions `--dur-fast`.

## 3.10 CommandPalette polish

Add to the existing palette:
- **Backdrop:** `backdrop-filter: blur(8px) saturate(140%)`, `bg: rgba(0,0,0,0.3)` (dark) or `rgba(255,255,255,0.6)` (light).
- **Dialog:** `width: 640px`, `border-radius: var(--radius-lg)`, `box-shadow: var(--shadow-modal)`, `border: 1px solid var(--border-hairline)`.
- **Input:** 44px, no border, big 15px text, placeholder "Search tickets, run a command...".
- **Result rows:** 36px, with `Cmd+Enter` hint on focused row.
- **Open animation:** opacity 0→1 + scale 0.96→1 over `--dur-slow` with `--ease-spring`.

---

# 4. Pages

## 4.1 Home

**Current problems:** `text-2xl` H1, ACTIVE PAIR dashed-border empty
state, generic-looking sections.

**Redesign:**

- TopBar: `Dashboard` (no count).
- Three sections in a vertical stack, each with a §3.8 SectionHeader:
  1. **Active pair** — if a ticket is `in_progress`, render a dense card (hairline border, `--radius-md`, padding 16px) showing: ticket id (mono, accent), title (15px medium), status arc + priority dot, last activity (relative time, muted). If none: borderless empty state ("No active work · `+` New ticket" with kbd hint).
  2. **Tickets by status** — horizontal row of count chips (use the §3.4 FilterChip style, non-interactive). One chip per non-zero status. Click → navigate to `/tickets?status=...`.
  3. **Recent activity** — last 8 events as DenseRow items: time (column 70px) · ticket id (mono accent) · event verb. Click → ticket detail.
- Below the fold (optional): two-column "Up next" (3 most-recent backlog tickets) + "Lately decided" (3 most-recent decisions).

## 4.2 Tickets

**Current problems:** filter chips are pills, group headers are big bg-stripes, table headers have `·` and `○` artifacts, no top-bar actions.

**Redesign:**

- TopBar: `Tickets · {count}` left; right cluster: `[Filters]` (icon button + count badge if active) → `[Sort]` (Updated ▾) → `[Options]` (View · density · grouping). The current chip rail moves *into* a `<Popover>` opened from the Filters button.
- Group headers: §3.8 SectionHeader with chevron, label, count badge. Collapse animation 120ms ease-out.
- Table: §3.1 DenseRow with header bg removed.
- Header glyphs: replace `·` with `<Flag size={12} />` (muted), `○` with `<CircleDashed size={12} />`.
- Per-row layout (`100px 1fr 20px 20px 90px 90px`):
  - **ID:** mono 11px, accent color, 100px.
  - **Title:** 13px primary, 1fr.
  - **Priority dot:** 20px column.
  - **Status arc:** 20px column.
  - **Phase:** 12px muted, 90px.
  - **Updated:** 12px muted, 90px, right-aligned.
- Hover row: §3.1 soft hover. Keyboard focus: §3.1 focus ring + accent-tinted bg.
- Filter chips moved into Filters popover. Top of popover: search input. Body: facet groups (Status, Phase) with checkboxes. Footer: "Clear all" + "Apply".

## 4.3 TicketDetail

**Current problems:** error state is a saturated red banner, no breadcrumb back, no two-column layout density.

**Redesign:**

- TopBar: breadcrumb `Tickets / bs-xxxxxx` (the slash is muted, the id is mono accent). Right cluster: `[Copy id]` (ghost icon button), `[Open in browser]` (ghost icon, only if URL exists), `[← Back]` (ghost icon button mapped to backspace key).
- Two-column grid (already implemented per `.ticket-detail-grid`, ≥1024px):
  - **Main (720px):** title (24px medium, tight tracking), description (markdown, prose styles), then activity log.
  - **Sidebar (280px):** properties as a vertical list of `Label / Value` rows (label: 11px caption muted, value: 13px primary). Properties: Status, Priority, Phase, Created, Updated, Assignee. Hairline divider every 4 rows.
- Activity log: vertical timeline with 8px gutter on the left for time, dot, then event text. No backgrounds, just hairline between dates.
- Error state: §3.6 inline error (icon + 2 lines), no banner.

## 4.4 Live

**Current problems:** journal renders as a wall of monospace lines with no structure.

**Redesign:**

- TopBar: `Live status` left; right cluster: `[Refresh]` (relative time of last refresh, muted) → `[Auto]` toggle (disabled by default, shows `Auto · 5s` when on).
- "Active pair" section identical to Home §4.1.
- Builder profile: switch from labels-on-the-left to a `2x4` grid of Label/Value cells, hairline divider, 11px caption + 13px value.
- Journal: render as DenseRows, columns: `[time 64px] [actor 80px mono accent] [event 1fr] [duration 56px right]`. Group by 5-min buckets with §3.8 SectionHeader (`5 minutes ago`, `15 minutes ago`, etc.). The current indented-line wall becomes a scannable log.

## 4.5 Decisions

- TopBar: `Decisions · {count}`. Right cluster: `[Filters]` (Skill, Phase, Class) → `[Sort]` (Newest ▾).
- Filter chips → Filters popover (same pattern as Tickets).
- Per-row layout: same as current but tighten columns and use §3.3 Tag instead of `StatusPill` for classification.
- Truncation banner: replace styled banner with §3.6 inline (icon + "Showing 100 of 247 decisions — oldest truncated").

## 4.6 SkillEvents

- Same pattern as Decisions.
- Outcome column: §3.3 Tag (text-only with dot prefix).
- Duration column: right-align tabular-nums, muted.
- Session column: mono 11px muted, truncate.

## 4.7 Timeline

**Current problems:** flat list that doesn't show time progression visually.

**Redesign:**

- TopBar: `Timeline`. Right cluster: `[View · Day | Week]` toggle.
- Group by day with §3.8 SectionHeader (`Wed, Apr 23` + count).
- Per-day rows: time gutter (`HH:MM`, 60px, mono 11px muted) → tiny dot → event text → relative duration. Vertical hairline through the time gutter to make the day feel continuous.

## 4.8 Analytics

**Current problems:** Outcome cards are loud, "Runs per day" bar chart sits in a card with no compaction.

**Redesign:**

- TopBar: `Analytics` + project name as breadcrumb. Right cluster: `[Range · 30d ▾]`.
- Drop the "Outcomes" card row. Replace with a single horizontal stat bar: `Total runs · 1,234   Success 91%   Errors 7%   Cancelled 2%` — text-only with the §3.3 Tag pattern, no cards.
- "Runs per skill" + "Runs per day" charts: drop the card wrapper (`p-4 border` becomes nothing). Add a compact subtitle line above each: `Runs per skill — top 12 by run count`.
- Per-skill table: hairline-only DenseRow (no card), tabular-nums on numeric cells, `--type-body-sm`.

---

# 5. Implementation order (recommended)

To minimize churn and let the project be reviewable mid-flight, ship in this order:

1. **Tokens & typography** — §1.1–1.10. Pure CSS edits in `styles.css` + Inter load in `index.html`. No component changes. Visible improvement: light-mode sidebar fixes itself, type tightens app-wide. **~1 day.**
2. **Layout chrome** — §2.1 (sidebar parity, icons, hover-only kbd) + §2.2 (TopBar component). Layout.tsx gains a slot, `<TopBar>` is a new component, every view renders one. **~1 day.**
3. **Shared components** — §3.1 (DenseRow), §3.4 (FilterChips), §3.5 (EmptyState), §3.6 (ErrorBox), §3.8 (SectionHeader), §3.9 (Button), §3.3 (Tag). Replace usage app-wide. **~1.5 days.**
4. **Page rebuilds** — §4.1 Home, §4.2 Tickets, §4.4 Live first (highest visibility); §4.3 TicketDetail next; §4.5–4.8 (Decisions/SkillEvents/Timeline/Analytics) last (mostly mechanical applications of the new shared components). **~2 days.**
5. **Motion & polish** — §1.8 motion tokens, route fade, palette spring open, hover transitions everywhere. **~0.5 day.**

Total: ~6 days. Each step ships green by itself; rolling back one doesn't break the others.

---

# 6. Out of scope (deliberate)

Things that would push this past Linear-tier into over-engineering:

- **Iconography on every status row** — text-only with the dot prefix is cleaner.
- **Drag-and-drop reordering** — babysit data is read-only snapshots.
- **Real-time collaboration cursors** — single-user dashboard.
- **Full keyboard-driven editing** — read-only, so beyond view-nav (J/K/G+letter/Cmd+K) there's nothing to edit.
- **Skeleton loaders** — snapshot loads in <50ms locally; skeletons would flash.
- **Custom font (Berkeley Mono, etc.)** — listed in the mono stack but only as fallback; Inter + system mono is enough.

---

# 7. Decisions log

| Date       | Decision | Rationale |
|------------|----------|-----------|
| 2026-04-26 | Initial design system | Created via /bbs:design-consultation; bs-rlenq5ev follow-up |
| 2026-04-26 | Light-mode sidebar parity | Always-dark sidebar in light mode is the single biggest "not Linear" tell |
| 2026-04-26 | Add 44px TopBar across all views | Without it, every page reads as a static doc, not a workspace |
| 2026-04-26 | Replace rounded-full filter chips with 4px squircles | Pill = social; squircle = productivity |
| 2026-04-26 | Adopt lucide-react for iconography | ~30kb tree-shaken; nav without icons is text-heavy |
| 2026-04-26 | Drop card wrappers on Analytics charts | Cards add nothing when there's already a hairline + label |

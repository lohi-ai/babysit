---
name: design-ui
description: Design a feature, page, or component and deliver a reviewable prototype before implementation. Use for UI/UX specs, style/color/typography selection, and early design feedback on frontend work.
---

# design-ui

Design the smallest complete UI spec a builder can implement — and prove it
with a prototype the human can open. The prototype is the point: design
feedback before `implement` costs minutes; after it, a rework loop.

## Flow

1. **Context.** Read the request and requirement (ticket `pointers.*`, else
   conversation). Read the existing design system first: check the repo's
   landing doc (CLAUDE.md / AGENTS.md) for a declared design doc — if it names
   one (DESIGN.md at any path, a design-system doc, style rules inline), that
   doc is authoritative; pass its path via `bbs-design tokens --design <path>`
   when it isn't the root `DESIGN.md` that `bbs-design tokens` finds on its
   own. Then `bbs-design components --root <fe-root>` and the nearest existing
   screen that solves a similar problem. An existing system always wins over a
   new invention. When the request improves an existing screen, the current
   state is the baseline, not a guess: read the code that renders it and open
   it live (`browse`) before designing.
2. **No design doc found in step 1? Author DESIGN.md**, then continue with it
   as the master. Write it to the repo root in the `babysit-design/v1` shape
   (`references/design-md-template.md`):
   - **Existing product (UI code ships already):** derive it from what exists —
     extract tokens from the live styles (globals.css, tailwind config, theme
     files), inventory components (`bbs-design components`), codify the de
     facto reuse rules. No rebrand: the file documents the system that ships.
   - **New project (no UI yet):** brand and style are the human's call — a
     User Challenge, not a taste decision. Gather product type, audience,
     mood/style keywords, and any brand color through the mode's escalation
     surface (`AskUserQuestion` in developer mode, `NEEDS_CONTEXT` block
     otherwise), draft the system with `bbs-design suggest --product "<type>"`
     (style, palette, typography, anti-patterns; product types live in
     `data/products.csv`), write DESIGN.md, then prototype against it.
   Record how the file was authored as the first Decisions Log row.
3. **Spec.** User, primary job, entry point; layout, controls, responsive
   behavior; empty/loading/error/success states; accessibility notes. Name
   the existing components, tokens, and copy style being reused.
4. **Prototype — required for any user-facing surface.** Build the cheapest
   artifact a human can open and judge:
   - Runnable frontend (Next.js, Vite, …): a throwaway route under a clearly
     marked path (`app/prototype/<slug>/page.tsx` or the repo's equivalent),
     built from the real design-system components and tokens.
   - No runnable frontend, or a standalone request: one self-contained HTML
     file (inline CSS, no external assets) at `tickets/<ticket>/prototype.html`
     when a ticket resolves, else in the working directory.
   Show the primary state plus the empty and error variants on the same
   surface. Real copy, never lorem ipsum.
5. **Check.** Run `bbs-design ux-check --category accessibility` always, plus
   the categories the surface touches (forms, navigation, charts, …), and fix
   violations in the prototype. Verify it actually renders — load the dev
   route or open the HTML (use `browse` when available).
6. **Handoff.** When a ticket resolves, write the spec to `design.md` and
   `bbs-ticket set-pointer design <path>`; otherwise emit it inline. State
   the prototype path and the one command/URL to view it.

## Rules

- Prototype-first: a text-only spec for a user-facing surface is not done —
  if a prototype is genuinely impossible, return `DONE_WITH_CONCERNS` naming
  why.
- Quality gate (from ux-check data, non-negotiable): SVG icons — never emoji
  as icons; text contrast ≥ 4.5:1; touch targets ≥ 44px; visible focus
  states; visible labels — not placeholder-only; mobile-first responsive.
- Component library: the project's own (per DESIGN.md's inventory) always
  comes first. When the project has none, prefer Astryx
  (https://astryx.atmeta.com/docs/getting-started), else shadcn/ui
  (https://ui.shadcn.com/docs/installation). Never hand-roll a primitive
  either one provides.
- Prototype code is disposable and isolated: never wire it into production
  navigation, routes, or shared state. `implement` rebuilds it properly.
- Do not implement the production feature; that is `implement` following
  this spec.
- Do not invent brand tokens, icons, claims, or product positioning.

## Output

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: DESIGNED
PROTOTYPE: <path + how to view, or none + why>
SUMMARY: <UI spec + key decisions>
NEXT: standalone — human reviews prototype, then plan-draft or implement;
      inside a workflow — continue, prototype rides to the final handoff
```

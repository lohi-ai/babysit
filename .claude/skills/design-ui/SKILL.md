---
name: design-ui
description: Design a feature, page, or component and deliver a reviewable prototype before implementation. Use for UI/UX specs, style/color/typography selection, and early design feedback on frontend work.
---
# design-ui
Design the smallest complete UI spec a builder can implement — and prove it
with a prototype the human can open.
## Flow
1. **Context.** Read the request and requirement (ticket `pointers.*`, else
   conversation). The landing doc's declared design doc is authoritative —
   pass a non-root path via `bbs-design tokens --design <path>`. Then
   `bbs-design components --root <fe-root>` and the nearest existing screen
   that solves a similar problem. When improving an existing screen, read the
   code that renders it and open it live (`browse`) before designing.
2. **No design doc found? Author DESIGN.md** at the repo root in the
   `babysit-design/v1` shape (`references/design-md-template.md`), then
   continue with it as the master:
   - **Existing product:** derive it from what ships — extract tokens from
     the live styles, inventory components (`bbs-design components`), codify
     the de facto reuse rules. No rebrand.
   - **New project (no UI yet):** brand and style are the human's call — a
     User Challenge. Gather product type, audience, mood/style keywords, and
     any brand color via the mode's escalation surface, draft with
     `bbs-design suggest --product "<type>"` (product types in
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
  navigation, routes, or shared state — `implement` rebuilds it properly
  following this spec; do not build the production feature here.
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

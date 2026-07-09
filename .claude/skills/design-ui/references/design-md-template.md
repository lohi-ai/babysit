# DESIGN.md template — `babysit-design/v1`
DESIGN.md lives at the repo root. `bbs-design tokens` reads the YAML
frontmatter; skills and humans read the prose. Reference implementation:
kiem-lai's DESIGN.md.
## Frontmatter (machine-read)
```yaml
---
schema: babysit-design/v1
project: <slug> (<display name>)
product_type: <e.g. editorial / reading app>
ref: https://getdesign.md/what-is-design-md
tokens:
  colors:
    primary: "#..."           # + on_primary, secondary, accent, background,
    foreground: "#..."        #   muted, muted_foreground, border,
    ring: "#..."              #   destructive, ring
  brand_scale: { 50: "#...", 500: "#...", 900: "#..." }   # full ramp when one exists
  typography:
    body: { family: "...", subsets: [...], var: "--font-body" }
    heading: { family: "..." }
    scale: [12, 14, 16, 18, 24, 32]
  spacing: { base: 4 }
  layout: { radius: 12, radius_scale: [4, 8, 12, 16] }
  motion:
    easing: { enter: "cubic-bezier(...)" }
    duration_ms: { micro: 200, short: 300, medium: 400 }
---
```
Every value must trace to the live styles (globals.css, tailwind config,
theme file) for an existing product, or to the human-accepted draft for a
new one. No token that the code doesn't (or won't) define.
## Body sections, in order
- **How to use this file** — blockquote: read before any UI change; where
  tokens are defined in code; where the component library lives; the
  never-hardcode / never-rebuild rule.
- **Product Context** — what this is, who it's for, project type.
- **Aesthetic Direction** — direction, decoration level, mood, and a named
  **anti-patterns (banned)** list.
- **Typography / Color / Spacing & Layout / Motion** — the rules behind the
  tokens: hierarchy strategy, theme variants, viewport/radius rules, motion
  utilities, reduced-motion guard.
- **Components Inventory** — every reusable component, grouped, one line of
  purpose each; the import path; ⭐ marks the default choice for a job.
- **Reuse Policy** — numbered rules: reuse before building, tokens not
  literals, and the project's own defaults.
- **Decisions Log** — table `Date | Decision | Rationale`. The first row
  records how this file was authored (derived from existing system vs.
  drafted for a new project).

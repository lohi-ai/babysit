---
name: conversion-fix
description: Audit and improve a marketing or activation surface in source. Use for landing pages, pricing, signup, onboarding, paywalls, conversion friction, or CRO requests.
---

# conversion-fix

Find the highest-leverage conversion issue, fix it narrowly, and verify the rendered surface.

## Flow

1. Read `product-marketing.md` when present; otherwise infer cautiously from the page and label it.
2. Inspect the target surface as a user — rendered in the browser (`browse`) when a target runs, not inferred from source: promise, CTA, friction, trust, hierarchy, and activation path.
3. Pick the smallest source change likely to improve conversion.
4. Make the change in the existing style.
5. Verify with a focused browser check or targeted build command.

## Rules

- Do not invent ICP, metrics, testimonials, pricing, or claims.
- Do not redesign the whole page unless asked.
- Keep changes tied to one conversion problem.
- Capture before/after evidence when a browser target is available.

## Output

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: FIXED | AUDITED
SUMMARY: <issue fixed + verification>
NEXT: create-pr, qa, or none
```

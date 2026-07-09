---
name: conversion-fix
description: Audit and improve a marketing or activation surface in source. Use for landing pages, pricing, signup, onboarding, paywalls, conversion friction, or CRO requests.
---
# conversion-fix
Find the highest-leverage conversion issue, fix it narrowly in the existing
style, and verify the rendered surface — in the browser (`browse`) when a
target runs, not inferred from source. Ground in `product-marketing.md` when
present; otherwise infer cautiously and label it. Never invent ICP, metrics,
testimonials, pricing, or claims. One conversion problem per run — no full
redesigns unless asked. Capture before/after evidence when a browser target
is available.
## Output
```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: FIXED | AUDITED
SUMMARY: <issue fixed + verification>
NEXT: create-pr, qa, or none
```

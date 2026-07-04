---
name: copy-rewrite
description: Rewrite product marketing copy in source. Use for headlines, hero text, CTAs, feature copy, positioning clarity, tone, or copy audits.
---

# copy-rewrite

Make marketing copy clearer, more specific, and closer to the product's actual positioning.

## Flow

1. Read `product-marketing.md`, README, or the existing page for positioning and voice.
2. Identify the copy surface: headline, hero, CTA, feature bullets, body, email, or empty state.
3. Rewrite only the requested or highest-impact text.
4. Apply the source change in place.
5. Verify the text renders and fits when a UI target is available.

## Rules

- Do not invent claims, customer names, numbers, integrations, or outcomes.
- Preserve brand voice unless the user asks to change it.
- Prefer concrete benefits and customer language over cleverness.
- Keep design/layout changes out of scope unless needed for text fit.

## Output

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: REWRITTEN
SUMMARY: <copy changed + verification>
NEXT: create-pr, conversion-fix, or none
```

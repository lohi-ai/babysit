# P3 — UI design: bulk edit across 10,000 rows

A B2B admin console has a data table of customer records: 50 rows per page,
10,000+ records total, server-side filtering and sorting. PM asks for bulk
edit: an admin filters the table, selects records, and applies a change (set
account owner, change plan, add/remove tag) to all selected records. Applying
the change runs server-side, takes up to ~2 minutes for large selections, and
can partially fail — per-row validation may reject e.g. 200 of 10,000 rows.

Write the UX spec a designer and a frontend engineer could build from:

- Selection model, initiation, and confirmation
- Progress and completion states
- Partial-failure handling
- Undo / recovery
- Key edge cases and accessibility notes

Limit: 800 words. No visual mockups required — describe behavior precisely.

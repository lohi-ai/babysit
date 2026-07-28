# P7 — design a usage-metering and invoicing backend

Write a design document. The **deliverable is capped at 1000 words**; any
reasoning sections you choose to include before it are exempt from the cap.

## Product facts

- 50,000 business customers; each streams usage events (API calls,
  storage-hours, seats). Fleet average 100M events/day; peak ingest
  5,000 events/s.
- Delivery is at-least-once (duplicates possible). Events arrive up to
  **7 days late**. Each event carries a client-generated `event_id` and an
  `occurred_at` timestamp.
- Each customer's billing cycle is anchored to their signup day-of-month
  and closes at midnight in the **customer's own IANA timezone**.
- At cycle close an invoice is generated: line items per metered product,
  priced from the price table. **Mid-cycle price changes apply from their
  effective timestamp.**
- Invoices are **legally immutable once issued** — yet late events and
  billing disputes must still be handled after issuance.
- From any invoice line item, the customer can drill down to the underlying
  raw events, for **13 months** back.
- A regulator requires **reproducibility**: re-running invoice generation
  for any past cycle must produce the identical invoice, even after later
  price-table changes.
- Cost constraint: object storage is cheap; the OLTP database is expensive
  and should stay small.

## Deliverable

The architecture: components, data stores (schemas where they matter), the
ingest→invoice data flow, and how the system handles the hard cases implied
above. State capacity/storage estimates wherever they drive a decision.

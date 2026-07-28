# P4 — System architecture: webhook delivery platform

Design webhook delivery for a SaaS platform. Requirements:

- The platform emits events at 5,000/s sustained, with peaks of 50,000/s for
  ~10-minute bursts.
- ~20,000 customer HTTPS endpoints; a delivery is one POST per event per
  subscribed endpoint.
- At-least-once delivery; events for a given endpoint must arrive in order.
- Customer endpoints are unreliable: ~5% of requests fail or time out at any
  given moment; some endpoints go down for hours.
- Retry with exponential backoff for up to 24h, then dead-letter.
- The team already operates Kafka, Postgres, and Redis on AWS. Be
  cost-conscious.

Deliver: the architecture (components and flow), the data model for delivery
state, failure handling, and the tradeoffs you are explicitly making.

Limit: 900 words.

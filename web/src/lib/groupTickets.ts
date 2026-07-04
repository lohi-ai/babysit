import type { TicketSummary } from './data';
import { STATUS_BUCKET, BUCKET_ORDER, type StatusBucket } from './priority';

export interface TicketGroup {
  bucket: StatusBucket;
  tickets: TicketSummary[];
}

// Buckets collapsed by default in the UI
export const DEFAULT_COLLAPSED: ReadonlySet<StatusBucket> = new Set(['completed', 'cancelled']);

/**
 * Partition tickets into the 6 workflow buckets, in workflow order.
 * Empty buckets are omitted from the returned array.
 * Within each bucket the input order is preserved (caller controls sort).
 */
export function groupTickets(tickets: readonly TicketSummary[]): TicketGroup[] {
  const map = new Map<StatusBucket, TicketSummary[]>();
  for (const t of tickets) {
    const b = STATUS_BUCKET[t.status];
    let arr = map.get(b);
    if (!arr) { arr = []; map.set(b, arr); }
    arr.push(t);
  }
  return BUCKET_ORDER
    .filter(b => map.has(b))
    .map(b => ({ bucket: b, tickets: map.get(b)! }));
}

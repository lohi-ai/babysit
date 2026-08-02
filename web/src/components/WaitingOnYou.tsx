// Pending design decisions, pinned above everything else on Home and on the
// tickets list.
//
// A checkpoint that has to be hunted for is a checkpoint that gets skipped: the
// worker is *blocked* on this record, so it is the one section allowed above
// the fold. It renders nothing at all when nothing is pending, which is the
// normal state — an empty "waiting on you" heading would train the eye to
// ignore the filled one.

import type { TicketSummary } from '../lib/data';
import { DenseRow } from './DenseRow';
import { SectionHeader } from './SectionHeader';
import { formatRelative } from '../lib/format';

export function pendingApprovals(tickets: TicketSummary[]): TicketSummary[] {
  return tickets.filter(t => t.approval?.state === 'pending');
}

export function WaitingOnYou({ tickets }: { tickets: TicketSummary[] }) {
  const pending = pendingApprovals(tickets);
  if (pending.length === 0) return null;

  return (
    <section>
      <SectionHeader title="Waiting on you" count={pending.length} />
      <div className="mt-1">
        {pending.map(t => (
          <DenseRow
            key={t.id}
            columns="120px 1fr 100px 90px"
            onClick={() => { window.location.hash = `#/tickets/${t.id}`; }}
            tabIndex={0}
            role="link"
          >
            <span className="px-3 min-w-0">
              <span className="font-mono text-xs truncate block" style={{ color: 'var(--accent)' }}>
                {t.id}
              </span>
            </span>
            <span className="px-3 text-sm truncate min-w-0" style={{ color: 'var(--text-primary)' }}>
              {t.title}
            </span>
            <span className="px-3 text-xs truncate min-w-0" style={{ color: 'var(--text-secondary)' }}>
              {t.approval?.kind ?? 'plan'} review
            </span>
            <span
              className="px-3 text-xs truncate min-w-0"
              style={{ color: 'var(--text-muted)' }}
              title={t.approval?.at}
            >
              {formatRelative(t.approval?.at ?? '')}
            </span>
          </DenseRow>
        ))}
      </div>
    </section>
  );
}

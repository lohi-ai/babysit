import { useMemo } from 'react';
import type { Snapshot, TicketStatus } from '../lib/data';
import { Tag } from '../components/Tag';
import { DenseRow } from '../components/DenseRow';
import { EmptyState } from '../components/EmptyState';
import { SectionHeader } from '../components/SectionHeader';
import { TopBar } from '../components/TopBar';
import { formatRelative } from '../lib/format';
import { useFilter } from '../contexts/FilterContext';
import { useScopedTickets, useScopedTimeline } from '../lib/scope';

const STATUS_ORDER: TicketStatus[] = [
  'in_progress', 'in_review', 'blocked', 'planned', 'decomposed',
  'triage', 'backlog', 'done', 'cancelled', 'duplicate', 'unknown',
];

export function Home({ snapshot }: { snapshot: Snapshot }) {
  const { state } = useFilter();
  const tickets = useScopedTickets(snapshot, state.project);
  const timeline = useScopedTimeline(snapshot, state.project);
  const { meta } = snapshot;
  const sessionCount = snapshot.sessions?.count ?? 0;

  const counts = useMemo(() => {
    const m = new Map<string, number>();
    for (const t of tickets) m.set(t.status, (m.get(t.status) ?? 0) + 1);
    return m;
  }, [tickets]);

  const recent = useMemo(() => timeline.slice(0, 8), [timeline]);

  return (
    <>
      <TopBar title={state.project === 'all' ? 'Dashboard — all projects' : 'Dashboard'} />
      <div className="px-6 py-4 w-full space-y-6">
        <section>
          <SectionHeader title="Active work" />
          {meta.active_pair ? (
            <div
              className="p-4 mt-1"
              style={{
                border: '1px solid var(--border-hairline)',
                borderRadius: 'var(--radius-md)',
                backgroundColor: 'var(--surface-bg)',
              }}
            >
              <div className="flex items-center gap-2 text-sm">
                <a href={`#/tickets/${meta.active_pair.ticket}`} className="font-mono font-medium hover:underline" style={{ color: 'var(--accent)' }}>
                  {meta.active_pair.ticket}
                </a>
                <span style={{ color: 'var(--text-muted)' }}>/</span>
                <span>{meta.active_pair.workflow}</span>
                <span style={{ color: 'var(--text-muted)' }}>/</span>
                <span style={{ color: 'var(--text-secondary)' }}>{meta.active_pair.step}</span>
              </div>
              <div className="text-xs mt-1 truncate" style={{ color: 'var(--text-muted)' }} title={meta.active_pair.branch}>
                branch: {meta.active_pair.branch}
              </div>
              <div className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
                {sessionCount} active session{sessionCount === 1 ? '' : 's'}
              </div>
            </div>
          ) : (
            <EmptyState title="No active pair" body="No ticket is currently in_progress." />
          )}
        </section>

        <section>
          <SectionHeader title="Tickets by status" count={tickets.length} />
          {tickets.length === 0 ? (
            <EmptyState title="No tickets" />
          ) : (
            <div className="flex flex-wrap gap-x-4 gap-y-1.5 mt-1">
              {STATUS_ORDER.filter(s => counts.has(s)).map(s => (
                <div key={s} className="flex items-center gap-1.5">
                  <Tag status={s} />
                  <span
                    className="font-mono text-xs"
                    style={{ color: 'var(--text-secondary)', fontVariantNumeric: 'tabular-nums' }}
                  >
                    {counts.get(s)}
                  </span>
                </div>
              ))}
            </div>
          )}
        </section>

        <section>
          <SectionHeader title="Recent activity" count={recent.length} />
          {recent.length === 0 ? (
            <EmptyState title="No activity" body="No timeline events yet." />
          ) : (
            <div
              className="overflow-hidden mt-1"
              style={{
                border: '1px solid var(--border-hairline)',
                borderRadius: 'var(--radius-md)',
                backgroundColor: 'var(--surface-bg)',
              }}
            >
              <DenseRow columns="96px 120px 1fr" header>
                <span className="px-3 py-1 text-xs font-medium uppercase" style={{ color: 'var(--text-muted)', letterSpacing: 'var(--tracking-caption)' }}>When</span>
                <span className="px-3 py-1 text-xs font-medium uppercase" style={{ color: 'var(--text-muted)', letterSpacing: 'var(--tracking-caption)' }}>Ticket</span>
                <span className="px-3 py-1 text-xs font-medium uppercase" style={{ color: 'var(--text-muted)', letterSpacing: 'var(--tracking-caption)' }}>Event</span>
              </DenseRow>
              {recent.map((e, i) => (
                <DenseRow key={i} columns="96px 120px 1fr">
                  <span className="px-3 text-xs truncate min-w-0" style={{ color: 'var(--text-muted)' }} title={e.ts}>
                    {formatRelative(e.ts)}
                  </span>
                  <span className="px-3 min-w-0">
                    <a href={`#/tickets/${e.ticket}`} title={e.ticket} className="font-mono text-xs hover:underline truncate block" style={{ color: 'var(--accent)' }}>
                      {e.ticket}
                    </a>
                  </span>
                  <span className="px-3 text-sm truncate min-w-0" style={{ color: 'var(--text-primary)' }}>
                    {e.workflow ?? e.event ?? ''}{e.step ? ` / ${e.step}` : ''}
                    {e.status ? ` — ${e.status}` : ''}
                  </span>
                </DenseRow>
              ))}
            </div>
          )}
        </section>
      </div>
    </>
  );
}

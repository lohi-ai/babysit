import { useMemo } from 'react';
import type { Snapshot, TicketStatus, TicketSummary } from '../lib/data';
import { Tag } from '../components/Tag';
import { DenseRow } from '../components/DenseRow';
import { EmptyState } from '../components/EmptyState';
import { SectionHeader } from '../components/SectionHeader';
import { TopBar } from '../components/TopBar';
import { WaitingOnYou } from '../components/WaitingOnYou';
import { formatRelative } from '../lib/format';
import { useFilter } from '../contexts/FilterContext';
import { useScopedSessions, useScopedTickets } from '../lib/scope';

const STATUS_ORDER: TicketStatus[] = [
  'in_progress', 'in_review', 'blocked', 'planned', 'decomposed',
  'triage', 'backlog', 'done', 'cancelled', 'duplicate', 'unknown',
];

/** What is being worked right now: a foreman holds it, or a live session is
 *  sitting in it. Either one means someone would notice if it broke. */
interface ActiveRow {
  ticket: TicketSummary;
  /** Who is running it — a foreman id, `autopilot` for a bare session, or null
   *  when it is assigned but nothing is attached yet. */
  runner: string | null;
  live: boolean;
}

export function Home({ snapshot }: { snapshot: Snapshot }) {
  const { state } = useFilter();
  const tickets = useScopedTickets(snapshot, state.project);
  const sessions = useScopedSessions(snapshot);
  const { meta } = snapshot;

  const counts = useMemo(() => {
    const m = new Map<string, number>();
    for (const t of tickets) m.set(t.status, (m.get(t.status) ?? 0) + 1);
    return m;
  }, [tickets]);

  const active = useMemo<ActiveRow[]>(() => {
    const live = new Set(sessions.map(s => s.ticket).filter(Boolean) as string[]);
    return tickets
      .filter(t => t.assignee || live.has(t.id))
      .map(t => ({
        ticket: t,
        runner: t.assignee ?? (live.has(t.id) ? 'autopilot' : null),
        live: live.has(t.id),
      }))
      .sort((a, b) => (b.ticket.updated_at ?? '').localeCompare(a.ticket.updated_at ?? ''));
  }, [tickets, sessions]);

  return (
    <>
      <TopBar title={state.project === 'all' ? 'Dashboard — all projects' : 'Dashboard'} />
      <div className="px-6 py-4 w-full space-y-6">
        <WaitingOnYou tickets={tickets} />
        <section>
          <SectionHeader title="Active tickets" count={active.length} />
          {active.length === 0 ? (
            <EmptyState
              title="Nothing running"
              body="No ticket is assigned to a foreman or open in a live session."
            />
          ) : (
            <div
              className="overflow-hidden mt-1"
              style={{
                border: '1px solid var(--border-hairline)',
                borderRadius: 'var(--radius-md)',
                backgroundColor: 'var(--surface-bg)',
              }}
            >
              <DenseRow columns="120px 1fr 140px 160px 96px" header>
                <HeadCell>Ticket</HeadCell>
                <HeadCell>Title</HeadCell>
                <HeadCell>Running</HeadCell>
                <HeadCell>Step</HeadCell>
                <HeadCell>Updated</HeadCell>
              </DenseRow>
              {active.map(({ ticket: t, runner, live }) => {
                const pair = meta.active_pair?.ticket === t.id ? meta.active_pair : null;
                const step = pair ? `${pair.workflow} / ${pair.step}` : t.phase ?? '—';
                return (
                  <DenseRow key={t.id} columns="120px 1fr 140px 160px 96px">
                    <span className="px-3 min-w-0">
                      <a href={`#/tickets/${t.id}`} title={t.id} className="font-mono text-xs hover:underline truncate block" style={{ color: 'var(--accent)' }}>
                        {t.id}
                      </a>
                    </span>
                    <span className="px-3 text-sm truncate min-w-0" style={{ color: 'var(--text-primary)' }} title={t.title}>
                      {t.title || '—'}
                    </span>
                    <span className="px-3 text-xs truncate min-w-0 flex items-center gap-1.5" style={{ color: 'var(--text-secondary)' }}>
                      {/* The dot is the difference between "a foreman owns this"
                          and "something is running it right now". */}
                      <span
                        aria-hidden="true"
                        className="shrink-0 rounded-full"
                        style={{
                          width: 6,
                          height: 6,
                          backgroundColor: live ? 'var(--status-in_progress-text, var(--accent))' : 'var(--text-muted)',
                        }}
                      />
                      <span className="truncate" title={live ? `${runner} — live session` : `${runner} — assigned`}>
                        {runner ?? '—'}
                      </span>
                    </span>
                    <span className="px-3 text-xs truncate min-w-0" style={{ color: 'var(--text-secondary)' }} title={step}>
                      {step}
                    </span>
                    <span className="px-3 text-xs truncate min-w-0" style={{ color: 'var(--text-muted)' }} title={t.updated_at ?? ''}>
                      {t.updated_at ? formatRelative(t.updated_at) : '—'}
                    </span>
                  </DenseRow>
                );
              })}
            </div>
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

      </div>
    </>
  );
}

function HeadCell({ children }: { children: React.ReactNode }) {
  return (
    <span
      className="px-3 py-1 text-xs font-medium uppercase"
      style={{ color: 'var(--text-muted)', letterSpacing: 'var(--tracking-caption)' }}
    >
      {children}
    </span>
  );
}

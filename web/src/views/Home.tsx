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

const FRAME_STYLE: React.CSSProperties = {
  border: '1px solid var(--border-hairline)',
  borderRadius: 'var(--radius-md)',
  backgroundColor: 'var(--surface-bg)',
};

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
  const sessionCount = snapshot.sessions?.count ?? 0;

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

  // Sessions the table above cannot account for: no ticket attached, or a
  // ticket outside the current project filter. Showing every session here as
  // well would print the busy ones twice; showing none of them hides the case
  // that actually needs explaining — a running Claude that no ticket claims.
  const looseSessions = useMemo(() => {
    const shown = new Set(active.map(a => a.ticket.id));
    return sessions.filter(s => !s.ticket || !shown.has(s.ticket));
  }, [sessions, active]);

  return (
    <>
      <TopBar
        title={state.project === 'all' ? 'Dashboard — all projects' : 'Dashboard'}
        actions={<Liveness count={sessionCount} at={meta.generated_at || meta.snapshot_at} />}
      />
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

        {looseSessions.length > 0 && (
          <section>
            <SectionHeader title="Unclaimed sessions" count={looseSessions.length} />
            <div className="overflow-hidden mt-1" style={FRAME_STYLE}>
              <DenseRow columns="140px 140px 1fr 64px" header>
                <HeadCell>Ticket</HeadCell>
                <HeadCell>Product</HeadCell>
                <HeadCell>Cwd</HeadCell>
                <HeadCell>Age</HeadCell>
              </DenseRow>
              {looseSessions.map(s => (
                <DenseRow key={s.id} columns="140px 140px 1fr 64px">
                  <span className="px-3 min-w-0">
                    {/* A session with a ticket links to it even though the table
                        above filtered it out — that is the whole reason to show
                        the row: the ticket is in another project. */}
                    {s.ticket ? (
                      <a
                        href={`#/tickets/${s.ticket}`}
                        title={s.ticket}
                        className="font-mono text-xs hover:underline truncate block"
                        style={{ color: 'var(--accent)' }}
                      >
                        {s.ticket}
                      </a>
                    ) : (
                      <span className="font-mono text-xs truncate block" style={{ color: 'var(--text-muted)' }} title={s.id}>
                        no ticket
                      </span>
                    )}
                  </span>
                  <span className="px-3 font-mono text-xs truncate min-w-0" style={{ color: 'var(--text-secondary)' }} title={s.product ?? ''}>
                    {s.product ?? '—'}
                  </span>
                  <span className="px-3 font-mono text-xs truncate min-w-0" style={{ color: 'var(--text-secondary)' }} title={s.cwd ?? ''}>
                    {s.cwd ?? '—'}
                  </span>
                  <span
                    className="px-3 font-mono text-xs truncate min-w-0"
                    style={{ color: 'var(--text-muted)', fontVariantNumeric: 'tabular-nums' }}
                  >
                    {s.age_min}m
                  </span>
                </DenseRow>
              ))}
            </div>
          </section>
        )}

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

        <Activity lines={snapshot.journalTail ?? []} />
      </div>
    </>
  );
}

/** The one thing the ticket tables cannot say: whether anything is running at
 *  all, and how old the numbers beneath it are. Lives in the TopBar so it stays
 *  on screen while the page scrolls. */
function Liveness({ count, at }: { count: number; at: string | undefined }) {
  return (
    <span className="flex items-center gap-2" title={at ?? ''}>
      <span
        aria-hidden="true"
        className={`rounded-full shrink-0 ${count > 0 ? 'animate-pulse' : ''}`}
        style={{
          width: 7,
          height: 7,
          backgroundColor: count > 0 ? 'var(--status-completed-text)' : 'var(--text-muted)',
        }}
      />
      <span className="text-xs whitespace-nowrap" style={{ color: 'var(--text-secondary)' }}>
        {count > 0 ? `${count} live` : 'idle'}
      </span>
      <span className="text-xs whitespace-nowrap" style={{ color: 'var(--text-muted)' }}>
        · {formatRelative(at)}
      </span>
    </span>
  );
}

const TS_PREFIX = /^(\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}(?::\d{2})?(?:Z|[+-]\d{2}:?\d{2})?)\s*(.*)$/s;

interface JournalLine {
  ts: string | null;
  body: string;
}

function parseLine(raw: string): JournalLine {
  const m = raw.match(TS_PREFIX);
  return m ? { ts: m[1], body: m[2] } : { ts: null, body: raw };
}

/** Five-minute buckets, because journal lines arrive in bursts — one heading
 *  per line would be longer than the log. */
function bucketKey(ts: string | null): string {
  if (!ts) return 'no-timestamp';
  const d = new Date(ts.replace(' ', 'T'));
  if (Number.isNaN(d.getTime())) return 'no-timestamp';
  const minutes = d.getMinutes();
  d.setMinutes(minutes - (minutes % 5), 0, 0);
  return d.toISOString();
}

function bucketLabel(key: string): string {
  if (key === 'no-timestamp') return 'Other';
  const d = new Date(key);
  if (Number.isNaN(d.getTime())) return key;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** The journal tail — collapsed by default, unlike on the Live screen it came
 *  from. It is the longest thing on the page and the least summarised, so it
 *  earns a heading here and its lines only when asked for. */
function Activity({ lines }: { lines: string[] }) {
  const buckets = useMemo(() => {
    const map = new Map<string, JournalLine[]>();
    for (const raw of lines) {
      const line = parseLine(raw);
      const k = bucketKey(line.ts);
      if (!map.has(k)) map.set(k, []);
      map.get(k)!.push(line);
    }
    return Array.from(map.entries()).sort((a, b) => {
      if (a[0] === 'no-timestamp') return 1;
      if (b[0] === 'no-timestamp') return -1;
      return b[0].localeCompare(a[0]);
    });
  }, [lines]);

  if (lines.length === 0) return null;

  return (
    <section>
      <SectionHeader title="Activity" count={lines.length} defaultOpen={false}>
        <div className="space-y-3 mt-1">
          {buckets.map(([key, group]) => (
            <div key={key}>
              <div
                className="font-mono mb-1"
                style={{ fontSize: 11, color: 'var(--text-muted)' }}
              >
                {bucketLabel(key)}
              </div>
              <div className="overflow-hidden" style={FRAME_STYLE}>
                {group.map((line, i) => (
                  <DenseRow key={i} columns="64px 1fr">
                    <span
                      className="px-3 py-1.5 font-mono text-xs truncate min-w-0"
                      style={{ color: 'var(--text-muted)' }}
                      title={line.ts ?? ''}
                    >
                      {line.ts ? line.ts.slice(11, 16) : '—'}
                    </span>
                    <span
                      className="px-3 py-1.5 font-mono text-xs whitespace-pre-wrap break-words min-w-0"
                      style={{ color: 'var(--text-secondary)' }}
                    >
                      {line.body}
                    </span>
                  </DenseRow>
                ))}
              </div>
            </div>
          ))}
        </div>
      </SectionHeader>
    </section>
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

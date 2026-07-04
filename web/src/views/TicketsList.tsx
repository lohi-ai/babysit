import { useEffect, useMemo, useRef, useState } from 'react';
import { Flag, Circle } from 'lucide-react';
import type { Snapshot, TicketStatus, TicketSummary } from '../lib/data';
import { StatusArc } from '../components/StatusArc';
import { PriorityDot } from '../components/PriorityDot';
import { DenseRow } from '../components/DenseRow';
import { FiltersPopover, type FacetDef } from '../components/FiltersPopover';
import { EmptyState } from '../components/EmptyState';
import { SectionHeader } from '../components/SectionHeader';
import { TopBar } from '../components/TopBar';
import { formatRelative } from '../lib/format';
import { useFilter } from '../contexts/FilterContext';
import { useScopedTickets } from '../lib/scope';
import { groupTickets, DEFAULT_COLLAPSED } from '../lib/groupTickets';
import { BUCKET_LABEL, STATUS_BUCKET, derivePriority } from '../lib/priority';
import { useRegisterFocusScope } from '../lib/keyboard';

const ALL_STATUSES: TicketStatus[] = [
  'triage', 'backlog', 'planned', 'decomposed',
  'in_progress', 'in_review', 'blocked',
  'done', 'cancelled', 'duplicate', 'unknown',
];

// id | title | priority dot | status arc | phase | updated
const COLUMNS = '100px 1fr 24px 24px 90px 90px';

function isFlatMode(): boolean {
  // Read ?group=flat from the URL hash query string.
  const hash = typeof window !== 'undefined' ? window.location.hash : '';
  const q = hash.split('?')[1] ?? '';
  return new URLSearchParams(q).get('group') === 'flat';
}

export function TicketsList({ snapshot }: { snapshot: Snapshot }) {
  const { state } = useFilter();
  const tickets = useScopedTickets(snapshot, state.project);

  const phases = useMemo(() => {
    const set = new Set<string>();
    for (const t of tickets) { if (t.phase) set.add(t.phase); }
    return Array.from(set).sort();
  }, [tickets]);

  const facets: FacetDef[] = [
    { kind: 'status', label: 'Status', options: ALL_STATUSES },
    { kind: 'phase',  label: 'Phase',  options: phases },
  ];

  const filtered = useMemo(() => {
    return tickets.filter(t => {
      if (state.status.length > 0 && !state.status.includes(t.status)) return false;
      if (state.phase.length > 0 && !state.phase.includes(t.phase ?? '')) return false;
      return true;
    });
  }, [tickets, state.status, state.phase]);

  const groups = useMemo(() => groupTickets(filtered), [filtered]);

  const hasFilters = state.status.length > 0 || state.phase.length > 0;
  const flat = isFlatMode();

  const containerRef = useRef<HTMLDivElement>(null);
  const [rows, setRows] = useState<HTMLElement[]>([]);
  useEffect(() => {
    const root = containerRef.current;
    if (!root) { setRows([]); return; }
    const collected = Array.from(
      root.querySelectorAll<HTMLElement>('.dense-row--body[role="row"]')
    );
    setRows(collected);
  }, [filtered, flat, groups]);
  useRegisterFocusScope(rows);

  return (
    <>
      <TopBar title="Tickets" count={filtered.length} />
      <div className="px-6 py-4 w-full space-y-4">
      <FiltersPopover facets={facets} />

      {tickets.length === 0 ? (
        <EmptyState title="No tickets" body="No tickets found in this project." />
      ) : filtered.length === 0 ? (
        <EmptyState title="No results" body="No tickets match the active filters. Try clearing a chip." />
      ) : flat ? (
        <div
          ref={containerRef}
          className="overflow-hidden"
          style={{
            border: '1px solid var(--border-hairline)',
            borderRadius: 'var(--radius-md)',
            backgroundColor: 'var(--surface-bg)',
          }}
        >
          <ListHeader />
          {filtered.map(t => <TicketRow key={t.id} t={t} />)}
        </div>
      ) : (
        <div ref={containerRef} className="space-y-3">
          {groups.map(g => (
            <SectionHeader
              key={g.bucket}
              title={BUCKET_LABEL[g.bucket]}
              count={g.tickets.length}
              defaultOpen={!DEFAULT_COLLAPSED.has(g.bucket)}
            >
              <div
                className="overflow-hidden mt-1"
                style={{
                  border: '1px solid var(--border-hairline)',
                  borderRadius: 'var(--radius-md)',
                  backgroundColor: 'var(--surface-bg)',
                }}
              >
                <ListHeader />
                {g.tickets.map(t => <TicketRow key={t.id} t={t} />)}
              </div>
            </SectionHeader>
          ))}
        </div>
      )}

      {!hasFilters && filtered.length > 0 && (
        <div className="text-xs text-right" style={{ color: 'var(--text-muted)' }}>
          {filtered.length} ticket{filtered.length === 1 ? '' : 's'}
        </div>
      )}
      {hasFilters && filtered.length > 0 && (
        <div className="text-xs text-right" style={{ color: 'var(--text-muted)' }}>
          {filtered.length} of {tickets.length} ticket{tickets.length === 1 ? '' : 's'}
        </div>
      )}
      </div>
    </>
  );
}

function ListHeader() {
  return (
    <DenseRow columns={COLUMNS} header>
      <HeaderCell>ID</HeaderCell>
      <HeaderCell>Title</HeaderCell>
      <HeaderCell aria-label="Priority">
        <Flag size={12} strokeWidth={1.75} aria-hidden="true" />
      </HeaderCell>
      <HeaderCell aria-label="Status">
        <Circle size={12} strokeWidth={1.75} aria-hidden="true" />
      </HeaderCell>
      <HeaderCell>Phase</HeaderCell>
      <HeaderCell>Updated</HeaderCell>
    </DenseRow>
  );
}

function HeaderCell({ children, ...rest }: { children: React.ReactNode; 'aria-label'?: string }) {
  return (
    <span
      {...rest}
      className="px-3 py-1 text-xs font-medium uppercase flex items-center"
      style={{ color: 'var(--text-muted)', letterSpacing: 'var(--tracking-caption)' }}
    >
      {children}
    </span>
  );
}

function TicketRow({ t }: { t: TicketSummary }) {
  const priority = derivePriority(t);
  return (
    <DenseRow
      columns={COLUMNS}
      tabIndex={0}
      role="row"
      onClick={() => { window.location.hash = `#/tickets/${t.id}`; }}
    >
      <span className="px-3 min-w-0">
        <a
          href={`#/tickets/${t.id}`}
          className="font-mono text-xs hover:underline truncate block"
          style={{ color: 'var(--accent)' }}
          title={t.id}
          onClick={e => e.stopPropagation()}
        >
          {t.id}
        </a>
      </span>
      <span
        className="px-3 text-sm truncate min-w-0"
        style={{ color: 'var(--text-primary)' }}
        title={t.title || undefined}
      >
        {t.title || <span style={{ color: 'var(--text-muted)' }}>—</span>}
      </span>
      <span className="flex items-center justify-center min-w-0">
        <PriorityDot priority={priority} />
      </span>
      <span className="flex items-center justify-center min-w-0" style={{ color: bucketTextVar(t.status) }}>
        <StatusArc status={t.status} />
      </span>
      <span className="px-3 text-xs truncate min-w-0" style={{ color: 'var(--text-secondary)' }}>
        {t.phase ?? '—'}
      </span>
      <span className="px-3 text-xs truncate min-w-0" style={{ color: 'var(--text-muted)' }}>
        {formatRelative(t.updated_at)}
      </span>
    </DenseRow>
  );
}

// Map a ticket status to the status-bucket text token (drives StatusArc currentColor)
function bucketTextVar(status: TicketStatus): string {
  return `var(--status-${STATUS_BUCKET[status]}-text)`;
}

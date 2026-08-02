import { useEffect, useMemo, useRef, useState } from 'react';
import { Flag, Circle } from 'lucide-react';
import { foremanLive, type Snapshot, type TicketStatus, type TicketSummary } from '../lib/data';
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
import { useMediaQuery } from '../lib/media';
import { Button } from '../components/Button';
import { ControlChip } from '../components/ControlChip';
import { ErrorBox } from '../components/ErrorBox';
import { Field, inputStyle } from '../components/Field';
import { Modal } from '../components/Modal';
import { assignTicket, createTicket } from '../lib/api';
import { gateProps, useControlPlane, useMutation } from '../contexts/ControlContext';

const ALL_STATUSES: TicketStatus[] = [
  'triage', 'backlog', 'planned', 'decomposed',
  'in_progress', 'in_review', 'blocked',
  'done', 'cancelled', 'duplicate', 'unknown',
];

// id | title | priority dot | status arc | foreman | phase | updated
const COLUMNS = '100px 1fr 24px 24px 90px 90px 90px';
// Below 768px only the four columns that identify a ticket survive. The
// control chip lives inside the title cell precisely so it is still there
// after this collapse — a paused ticket must never look active on a phone.
const COLUMNS_NARROW = '80px 1fr 24px 24px';

function isFlatMode(): boolean {
  // Read ?group=flat from the URL hash query string.
  const hash = typeof window !== 'undefined' ? window.location.hash : '';
  const q = hash.split('?')[1] ?? '';
  return new URLSearchParams(q).get('group') === 'flat';
}

export function TicketsList({ snapshot }: { snapshot: Snapshot }) {
  const { state } = useFilter();
  const tickets = useScopedTickets(snapshot, state.project);
  const { canMutate, reason } = useControlPlane();
  const [newOpen, setNewOpen] = useState(false);
  const narrow = useMediaQuery('(max-width: 767px)');
  const columns = narrow ? COLUMNS_NARROW : COLUMNS;

  const phases = useMemo(() => {
    const set = new Set<string>();
    for (const t of tickets) { if (t.phase) set.add(t.phase); }
    return Array.from(set).sort();
  }, [tickets]);

  const controlCounts = useMemo(() => {
    const c = { active: 0, paused: 0, cancelled: 0 };
    for (const t of tickets) {
      const s = t.control?.state;
      if (s === 'paused' || s === 'cancelled') c[s]++;
      else c.active++;
    }
    return c as Record<string, number>;
  }, [tickets]);

  const facets: FacetDef[] = [
    { kind: 'status',  label: 'Status',  options: ALL_STATUSES },
    { kind: 'phase',   label: 'Phase',   options: phases },
    { kind: 'foreman', label: 'Foreman', options: (snapshot.foremen ?? []).map(f => f.id) },
    { kind: 'control', label: 'Control', options: ['active', 'paused', 'cancelled'], counts: controlCounts },
  ];

  const filtered = useMemo(() => {
    return tickets.filter(t => {
      if (state.status.length > 0 && !state.status.includes(t.status)) return false;
      if (state.phase.length > 0 && !state.phase.includes(t.phase ?? '')) return false;
      if (state.foreman.length > 0 && !state.foreman.includes(t.assignee ?? '')) return false;
      const control = t.control?.state ?? 'active';
      if (state.control.length > 0) {
        if (!state.control.includes(control)) return false;
      } else if (control === 'cancelled') {
        // Default view hides cancelled work. Nothing was deleted — the facet
        // carries the count, so it is one click back.
        return false;
      }
      return true;
    });
  }, [tickets, state.status, state.phase, state.foreman, state.control]);

  const groups = useMemo(() => groupTickets(filtered), [filtered]);

  const hasFilters =
    state.status.length > 0 || state.phase.length > 0 ||
    state.foreman.length > 0 || state.control.length > 0;
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
      <TopBar
        title="Tickets"
        count={filtered.length}
        actions={
          <Button variant="primary" onClick={() => setNewOpen(true)} {...gateProps(reason)}>
            New ticket
          </Button>
        }
      />
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
          <ListHeader columns={columns} narrow={narrow} />
          {filtered.map(t => <TicketRow key={t.id} t={t} columns={columns} narrow={narrow} />)}
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
                <ListHeader columns={columns} narrow={narrow} />
                {g.tickets.map(t => <TicketRow key={t.id} t={t} columns={columns} narrow={narrow} />)}
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
      <NewTicketModal
        open={newOpen}
        onClose={() => setNewOpen(false)}
        project={state.project}
        projects={Object.keys(snapshot.projects ?? {})}
        foremen={(snapshot.foremen ?? []).filter(f => foremanLive(f)).map(f => f.id)}
        canMutate={canMutate}
      />
    </>
  );
}

function ListHeader({ columns, narrow }: { columns: string; narrow: boolean }) {
  return (
    <DenseRow columns={columns} header>
      <HeaderCell>ID</HeaderCell>
      <HeaderCell>Title</HeaderCell>
      <HeaderCell aria-label="Priority">
        <Flag size={12} strokeWidth={1.75} aria-hidden="true" />
      </HeaderCell>
      <HeaderCell aria-label="Status">
        <Circle size={12} strokeWidth={1.75} aria-hidden="true" />
      </HeaderCell>
      {!narrow && <HeaderCell>Foreman</HeaderCell>}
      {!narrow && <HeaderCell>Phase</HeaderCell>}
      {!narrow && <HeaderCell>Updated</HeaderCell>}
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

function TicketRow({ t, columns, narrow }: { t: TicketSummary; columns: string; narrow: boolean }) {
  const priority = derivePriority(t);
  return (
    <DenseRow
      columns={columns}
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
        className="px-3 text-sm truncate min-w-0 flex items-center gap-1.5"
        style={{ color: 'var(--text-primary)' }}
        title={t.title || undefined}
      >
        {t.control && <ControlChip control={t.control} />}
        <span className="truncate">
          {t.title || <span style={{ color: 'var(--text-muted)' }}>—</span>}
        </span>
      </span>
      <span className="flex items-center justify-center min-w-0">
        <PriorityDot priority={priority} />
      </span>
      <span className="flex items-center justify-center min-w-0" style={{ color: bucketTextVar(t.status) }}>
        <StatusArc status={t.status} />
      </span>
      {!narrow && (
        <span className="px-3 font-mono text-xs truncate min-w-0" style={{ color: 'var(--text-secondary)' }} title={t.assignee ?? ''}>
          {t.assignee ?? '—'}
        </span>
      )}
      {!narrow && (
        <span className="px-3 text-xs truncate min-w-0" style={{ color: 'var(--text-secondary)' }}>
          {t.phase ?? '—'}
        </span>
      )}
      {!narrow && (
        <span className="px-3 text-xs truncate min-w-0" style={{ color: 'var(--text-muted)' }}>
          {formatRelative(t.updated_at)}
        </span>
      )}
    </DenseRow>
  );
}

function NewTicketModal({
  open,
  onClose,
  project,
  projects,
  foremen,
  canMutate,
}: {
  open: boolean;
  onClose: () => void;
  project: string;
  projects: string[];
  foremen: string[];
  canMutate: boolean;
}) {
  const [proj, setProj] = useState(project !== 'all' ? project : projects[0] ?? '');
  const [title, setTitle] = useState('');
  const [requirement, setRequirement] = useState('');
  const [assignee, setAssignee] = useState('');
  const { run, pending, error } = useMutation();
  // Create-then-assign is two calls for one intent, so a failed assign fails
  // the whole action — but the ticket it already created is real. Remembering
  // it keeps the retry from seeding a second one.
  const created = useRef('');

  const close = () => {
    created.current = '';
    onClose();
  };

  const submit = async () => {
    const body = requirement.trim() || title.trim();
    const ok = await run(async () => {
      if (!created.current) {
        const res = await createTicket(proj, body, title.trim());
        created.current = res.ticket;
      }
      if (assignee) await assignTicket(proj, created.current, assignee);
    }, `Ticket ${title.trim()} created`);
    if (ok) {
      const id = created.current;
      close();
      setTitle('');
      setRequirement('');
      setAssignee('');
      window.location.hash = `#/tickets/${id}`;
    }
  };

  return (
    <Modal
      open={open}
      onClose={close}
      title="New ticket"
      width={520}
      actions={
        <>
          <Button size="lg" onClick={close}>Cancel</Button>
          <Button
            size="lg"
            variant="primary"
            onClick={submit}
            disabled={pending || !canMutate || !title.trim() || !proj}
          >
            {pending ? 'Creating…' : 'Create ticket'}
          </Button>
        </>
      }
    >
      <Field label="Project">
        {id => (
          <select id={id} style={inputStyle} value={proj} onChange={e => setProj(e.target.value)}>
            {projects.map(p => <option key={p} value={p}>{p}</option>)}
          </select>
        )}
      </Field>
      <Field label="Title">
        {id => (
          <input id={id} style={inputStyle} value={title} onChange={e => setTitle(e.target.value)} />
        )}
      </Field>
      <Field
        label="Requirement"
        hint="Markdown. Becomes requirement.md — what a worker reads before planning. Defaults to the title."
      >
        {id => (
          <textarea
            id={id}
            style={{ ...inputStyle, minHeight: 120, resize: 'vertical' }}
            value={requirement}
            onChange={e => setRequirement(e.target.value)}
          />
        )}
      </Field>
      {/* Only live foremen, same rule the ticket detail's assign popover uses:
          one that stopped beating would park the ticket in an unread inbox. */}
      <Field
        label="Assign to"
        hint="The foreman that picks it up. It is woken now if its workspace is reachable, otherwise on its next tick."
      >
        {id => (
          <select id={id} style={inputStyle} value={assignee} onChange={e => setAssignee(e.target.value)}>
            <option value="">Unassigned</option>
            {foremen.map(f => <option key={f} value={f}>{f}</option>)}
          </select>
        )}
      </Field>
      {/* The typed requirement survives the failure — a retry starts from what
          is already on screen, not from an empty form. */}
      {error && <ErrorBox title="Could not create the ticket" body={error} />}
    </Modal>
  );
}

// Map a ticket status to the status-bucket text token (drives StatusArc currentColor)
function bucketTextVar(status: TicketStatus): string {
  return `var(--status-${STATUS_BUCKET[status]}-text)`;
}

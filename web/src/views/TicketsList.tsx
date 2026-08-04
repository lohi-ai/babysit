import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Flag, Circle } from 'lucide-react';
import { foremanLive, type Snapshot, type TicketStatus, type TicketSummary } from '../lib/data';
import { StatusArc } from '../components/StatusArc';
import { PriorityDot } from '../components/PriorityDot';
import { DenseRow } from '../components/DenseRow';
import { FiltersPopover, type FacetDef } from '../components/FiltersPopover';
import { EmptyState } from '../components/EmptyState';
import { SectionHeader } from '../components/SectionHeader';
import { TopBar } from '../components/TopBar';
import { WaitingOnYou } from '../components/WaitingOnYou';
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
import { assignTicket, createTicket, deleteTicket, setTicketStatus } from '../lib/api';
import { gateProps, useControlPlane, useMutation } from '../contexts/ControlContext';

const ALL_STATUSES: TicketStatus[] = [
  'triage', 'backlog', 'planned', 'decomposed',
  'in_progress', 'in_review', 'blocked',
  'done', 'cancelled', 'duplicate', 'unknown',
];

// `unknown` is what the composer reports for a ticket whose index.json has no
// status — it is a reading, not a rung, and the server's enum refuses it. So it
// filters but cannot be assigned.
const SETTABLE_STATUSES = ALL_STATUSES.filter(s => s !== 'unknown');

// select | id | title | priority dot | status arc | foreman | phase | updated
const COLUMNS = '28px 100px 1fr 24px 24px 90px 90px 90px';
// Below 768px only the four columns that identify a ticket survive, plus the
// checkbox — bulk-editing from a phone is the case where reaching every row
// individually is most painful. The control chip lives inside the title cell
// precisely so it is still there after this collapse — a paused ticket must
// never look active on a phone.
const COLUMNS_NARROW = '28px 80px 1fr 24px 24px';

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

  // Every mutation endpoint is scoped by project slug and this list can be
  // showing `all`, so the owning slug is looked up per ticket — the same
  // resolution the detail view does for its single ticket.
  const projectOf = useMemo(() => {
    const m = new Map<string, string>();
    for (const [slug, p] of Object.entries(snapshot.projects)) {
      for (const t of p.tickets) m.set(t.id, slug);
    }
    return m;
  }, [snapshot]);

  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const bulk = useBulk();

  // A selection the human cannot see is one they cannot check before acting on
  // it — so ids that a filter change, or a delete, took off the list are dropped
  // rather than silently carried into the next bulk action.
  useEffect(() => {
    setSelected(prev => {
      if (prev.size === 0) return prev;
      const visible = new Set(filtered.map(t => t.id));
      const next = new Set([...prev].filter(id => visible.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [filtered]);

  const toggle = useCallback((id: string) => {
    setSelected(prev => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  }, []);

  // The header checkbox scopes to its own table, which in grouped mode is one
  // status bucket — "select every blocked ticket" is the selection you actually
  // want, and it is the one the grouping already put in front of you.
  const toggleMany = useCallback((ids: string[], on: boolean) => {
    setSelected(prev => {
      const next = new Set(prev);
      for (const id of ids) {
        if (on) next.add(id);
        else next.delete(id);
      }
      return next;
    });
  }, []);

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
      {/* Unfiltered on purpose: a blocked worker that a status chip happens to
          hide is exactly the case this section exists to prevent. */}
      <WaitingOnYou tickets={tickets} />
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
          <ListHeader
            columns={columns}
            narrow={narrow}
            ids={filtered.map(t => t.id)}
            selected={selected}
            onToggleMany={toggleMany}
            canMutate={canMutate}
            reason={reason}
          />
          {filtered.map(t => (
            <TicketRow
              key={t.id}
              t={t}
              columns={columns}
              narrow={narrow}
              checked={selected.has(t.id)}
              onToggle={toggle}
              canMutate={canMutate}
              reason={reason}
            />
          ))}
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
                <ListHeader
                  columns={columns}
                  narrow={narrow}
                  ids={g.tickets.map(t => t.id)}
                  selected={selected}
                  onToggleMany={toggleMany}
                  canMutate={canMutate}
                  reason={reason}
                />
                {g.tickets.map(t => (
                  <TicketRow
                    key={t.id}
                    t={t}
                    columns={columns}
                    narrow={narrow}
                    checked={selected.has(t.id)}
                    onToggle={toggle}
                    canMutate={canMutate}
                    reason={reason}
                  />
                ))}
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
      <SelectionBar
        ids={[...selected]}
        projectOf={projectOf}
        foremen={(snapshot.foremen ?? []).filter(f => foremanLive(f)).map(f => f.id)}
        bulk={bulk}
        onClear={() => setSelected(new Set())}
      />
    </>
  );
}

interface BulkState {
  run: (ids: string[], each: (id: string) => Promise<unknown>, verb: string) => Promise<void>;
  /** "3 of 12" while applying; '' when idle. */
  progress: string;
  /** Per-ticket failures from the last run. */
  failures: { id: string; error: string }[];
}

/**
 * One bulk action = N per-ticket calls. There is no bulk endpoint on purpose:
 * partial failure is the normal case (one ticket paused by another tab, one
 * assignee that no longer exists), and a server-side loop would have to invent
 * its own protocol for reporting which of the twelve did not apply.
 */
function useBulk(): BulkState {
  const { refresh, announce } = useControlPlane();
  const [progress, setProgress] = useState('');
  const [failures, setFailures] = useState<{ id: string; error: string }[]>([]);

  const run = useCallback(
    async (ids: string[], each: (id: string) => Promise<unknown>, verb: string) => {
      setFailures([]);
      const failed: { id: string; error: string }[] = [];
      // Sequential, not Promise.all: each of these takes a per-ticket lock and
      // appends to history, and a burst of parallel writes would spend its time
      // contending for locks it is about to release anyway.
      for (let i = 0; i < ids.length; i++) {
        setProgress(`${i + 1} of ${ids.length}`);
        try {
          await each(ids[i]);
        } catch (e: unknown) {
          failed.push({ id: ids[i], error: e instanceof Error ? e.message : String(e) });
        }
      }
      setProgress('');
      setFailures(failed);
      // Unconditional: a run that failed on the ninth ticket still moved the
      // first eight, and the list has to show that.
      refresh();
      const done = ids.length - failed.length;
      announce(
        failed.length === 0
          ? `${ids.length} ticket${ids.length === 1 ? '' : 's'} ${verb}`
          : `${done} of ${ids.length} ${verb}, ${failed.length} failed`,
      );
    },
    [refresh, announce],
  );

  return { run, progress, failures };
}

/**
 * The bulk action bar. Floats over the list while anything is selected — the
 * actions have to stay reachable from the bottom of a long list, and a bar that
 * pushed the layout would move the rows out from under the pointer that is
 * still selecting them.
 */
function SelectionBar({
  ids,
  projectOf,
  foremen,
  bulk,
  onClear,
}: {
  ids: string[];
  projectOf: Map<string, string>;
  foremen: string[];
  bulk: BulkState;
  onClear: () => void;
}) {
  const { canMutate, reason } = useControlPlane();
  const [confirmDelete, setConfirmDelete] = useState(false);
  const busy = bulk.progress !== '';

  // Nothing selected and nothing to report from the last run.
  if (ids.length === 0 && bulk.failures.length === 0) return null;

  // Guaranteed by projectOf being built from the same snapshot the rows are.
  const slug = (id: string) => projectOf.get(id) ?? '';

  const applyStatus = (status: string) =>
    bulk.run(ids, id => setTicketStatus(slug(id), id, status), `moved to ${status}`);

  const applyAssignee = (foreman: string) =>
    bulk.run(ids, id => assignTicket(slug(id), id, foreman), foreman ? `assigned to ${foreman}` : 'unassigned');

  const applyDelete = async () => {
    setConfirmDelete(false);
    await bulk.run(ids, id => deleteTicket(slug(id), id), 'deleted');
    onClear();
  };

  return (
    <>
      <div
        className="fixed left-0 right-0 flex justify-center px-4 z-40"
        style={{ bottom: 16, pointerEvents: 'none' }}
      >
        <div
          className="flex flex-wrap items-center gap-2 px-3 py-2"
          style={{
            pointerEvents: 'auto',
            maxWidth: '100%',
            backgroundColor: 'var(--surface-elevated)',
            border: '1px solid var(--border-emphasis)',
            borderRadius: 'var(--radius-lg)',
            boxShadow: 'var(--shadow-popover)',
          }}
        >
          <span className="text-xs font-medium whitespace-nowrap" style={{ color: 'var(--text-primary)' }}>
            {busy ? `Applying ${bulk.progress}…` : `${ids.length} selected`}
          </span>

          {/* Both selects act on change and reset to their placeholder, so the
              control never claims to display a shared value the selection does
              not actually have. */}
          <select
            aria-label="Set status"
            style={{ ...inputStyle, width: 'auto', fontSize: 12 }}
            value=""
            disabled={busy || !canMutate || ids.length === 0}
            title={reason || undefined}
            onChange={e => { if (e.target.value) applyStatus(e.target.value); e.target.value = ''; }}
          >
            <option value="">Status…</option>
            {SETTABLE_STATUSES.map(s => <option key={s} value={s}>{s}</option>)}
          </select>

          <select
            aria-label="Set assignee"
            style={{ ...inputStyle, width: 'auto', fontSize: 12 }}
            value=""
            disabled={busy || !canMutate || ids.length === 0}
            title={reason || undefined}
            onChange={e => { if (e.target.value) applyAssignee(e.target.value === '__none__' ? '' : e.target.value); e.target.value = ''; }}
          >
            <option value="">Assign…</option>
            <option value="__none__">Unassigned</option>
            {foremen.map(f => <option key={f} value={f}>{f}</option>)}
          </select>

          {/* Plain button + a confirm that carries the weight — the same shape
              Retire uses, rather than a fourth Button variant for one caller. */}
          <Button
            disabled={busy || !canMutate || ids.length === 0}
            {...gateProps(reason)}
            onClick={() => setConfirmDelete(true)}
          >
            Delete
          </Button>
          <Button onClick={onClear} disabled={busy}>Clear</Button>
        </div>
      </div>

      {/* Failures outlive the bar's busy state: the whole point is to say which
          of the twelve did not apply, after the other eleven already did. */}
      {bulk.failures.length > 0 && (
        <div className="fixed left-0 right-0 flex justify-center px-4 z-40" style={{ bottom: 68 }}>
          <div style={{ maxWidth: 560 }}>
            <ErrorBox
              title={`${bulk.failures.length} ticket${bulk.failures.length === 1 ? '' : 's'} could not be changed`}
              body={bulk.failures.map(f => `${f.id}: ${f.error}`).join('\n')}
            />
          </div>
        </div>
      )}

      <Modal
        open={confirmDelete}
        onClose={() => setConfirmDelete(false)}
        title={`Delete ${ids.length} ticket${ids.length === 1 ? '' : 's'}?`}
        actions={
          <>
            <Button size="lg" onClick={() => setConfirmDelete(false)}>Cancel</Button>
            <Button size="lg" variant="primary" onClick={applyDelete}>
              Delete {ids.length}
            </Button>
          </>
        }
      >
        <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
          Each ticket's folder — requirement, plan, handoffs, verdicts, evidence —
          moves to <code className="font-mono text-xs">~/.babysit/trash/</code>. It
          leaves the dashboard but stays on disk, so this is recoverable by hand.
        </p>
        <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
          Git branches and worktrees are <strong>not</strong> touched.
        </p>
        <p className="font-mono text-xs" style={{ color: 'var(--text-muted)' }}>
          {ids.join(' ')}
        </p>
      </Modal>
    </>
  );
}

function ListHeader({
  columns,
  narrow,
  ids,
  selected,
  onToggleMany,
  canMutate,
  reason,
}: {
  columns: string;
  narrow: boolean;
  ids: string[];
  selected: ReadonlySet<string>;
  onToggleMany: (ids: string[], on: boolean) => void;
  canMutate: boolean;
  reason: string;
}) {
  const mine = ids.filter(id => selected.has(id)).length;
  const all = ids.length > 0 && mine === ids.length;
  return (
    <DenseRow columns={columns} header>
      <span className="flex items-center justify-center">
        <input
          type="checkbox"
          checked={all}
          // Some-but-not-all reads as a dash, so the box says "partly selected"
          // instead of "empty" while half the bucket is picked.
          ref={el => { if (el) el.indeterminate = mine > 0 && !all; }}
          disabled={!canMutate}
          title={reason || undefined}
          aria-label={all ? 'Deselect all' : 'Select all'}
          onChange={e => onToggleMany(ids, e.target.checked)}
        />
      </span>
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

function TicketRow({
  t,
  columns,
  narrow,
  checked,
  onToggle,
  canMutate,
  reason,
}: {
  t: TicketSummary;
  columns: string;
  narrow: boolean;
  checked: boolean;
  onToggle: (id: string) => void;
  canMutate: boolean;
  reason: string;
}) {
  const priority = derivePriority(t);
  return (
    <DenseRow
      columns={columns}
      tabIndex={0}
      role="row"
      selected={checked}
      onClick={() => { window.location.hash = `#/tickets/${t.id}`; }}
    >
      {/* The row navigates on click, so the checkbox has to stop the event or
          picking a ticket would leave the list for its detail page. */}
      <span className="flex items-center justify-center" onClick={e => e.stopPropagation()}>
        <input
          type="checkbox"
          checked={checked}
          disabled={!canMutate}
          title={reason || undefined}
          aria-label={`Select ${t.id}`}
          onChange={() => onToggle(t.id)}
        />
      </span>
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

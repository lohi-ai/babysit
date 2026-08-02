import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { foremanLive, type ForemanRow, type HistoryRow, type Snapshot, type TicketControl } from '../lib/data';
import { assignTicket, controlTicket, type ControlAction } from '../lib/api';
import { Button } from '../components/Button';
import { Tag } from '../components/Tag';
import { ErrorBox } from '../components/ErrorBox';
import { Field, inputStyle } from '../components/Field';
import { Markdown } from '../components/Markdown';
import { ApprovalPanel } from './ApprovalPanel';
import { Modal } from '../components/Modal';
import { controlTone } from '../components/ControlChip';
import { TopBar } from '../components/TopBar';
import { formatDate, formatRelative } from '../lib/format';
import { useFilter } from '../contexts/FilterContext';
import { gateProps, useControlPlane, useMutation } from '../contexts/ControlContext';
import { useScopedTicketDetail } from '../lib/scope';

type Tab = 'approval' | 'requirement' | 'plan' | 'manifest' | 'history' | 'handoffs' | 'verdicts' | 'reviews';

export function TicketDetail({ snapshot, ticketId }: { snapshot: Snapshot; ticketId: string }) {
  const { state } = useFilter();
  const detail = useScopedTicketDetail(snapshot, state.project, ticketId);

  // Every mutation endpoint is scoped by project slug, and the detail can be
  // reached with the project filter on `all`, so the owning slug is looked up
  // rather than taken from the filter.
  const project = useMemo(() => {
    if (state.project !== 'all') return state.project;
    for (const [slug, p] of Object.entries(snapshot.projects)) {
      if (p.ticketDetail[ticketId]) return slug;
    }
    return '';
  }, [snapshot, state.project, ticketId]);

  // Pause and cancel never touch a running session — this is what says so.
  const workerRunning = (snapshot.sessions?.sessions ?? []).some(s => s.ticket === ticketId);

  const tabs: { key: Tab; label: string; available: boolean }[] = detail ? [
    // First in the strip and first in `firstAvailable`, so a ticket with a
    // pending decision opens on the decision. It stays in the strip once
    // answered — the record is how the human sees what they already decided.
    { key: 'approval',    label: detail.approval?.state === 'pending' ? 'Approval ●' : 'Approval',
                          available: !!detail.approval },
    { key: 'requirement', label: 'Requirement', available: !!detail.requirement },
    { key: 'plan',        label: 'Plan',        available: !!detail.plan },
    { key: 'manifest',    label: 'Manifest',    available: !!detail.manifest },
    { key: 'history',     label: `History (${detail.history.length})`, available: detail.history.length > 0 },
    { key: 'handoffs',    label: `Handoffs (${detail.handoffs.length})`, available: detail.handoffs.length > 0 },
    { key: 'verdicts',    label: `Verdicts (${detail.verdicts.length})`, available: detail.verdicts.length > 0 },
    { key: 'reviews',     label: `Reviews (${detail.reviews.length})`,   available: detail.reviews.length > 0 },
  ] : [];

  // A pending decision is why the human opened the page; an answered one is
  // reference, so it does not displace the requirement on every past ticket.
  const firstAvailable: Tab = detail?.approval?.state === 'pending'
    ? 'approval'
    : tabs.find(t => t.available && t.key !== 'approval')?.key ?? 'requirement';
  const [tab, setTab] = useState<Tab>(firstAvailable);

  if (!detail) {
    return <ErrorBox title="Ticket not found" body={`No detail for ${ticketId} in this snapshot.`} />;
  }

  const backHref = state.project !== 'all' ? `#/tickets?project=${encodeURIComponent(state.project)}` : '#/tickets';

  const breadcrumb = (
    <div className="flex items-center gap-2 min-w-0">
      <a href={backHref} className="hover:underline shrink-0" style={{ color: 'var(--text-muted)', fontSize: 13 }}>
        Tickets
      </a>
      <span style={{ color: 'var(--text-muted)' }}>/</span>
      <span
        className="font-mono truncate"
        style={{ fontSize: 14, fontWeight: 500, color: 'var(--text-primary)' }}
      >
        {detail.id}
      </span>
    </div>
  );

  return (
    <>
      <TopBar
        title={detail.id}
        breadcrumb={breadcrumb}
        actions={
          <div className="flex items-center gap-3">
            <Tag status={detail.status} />
            {detail.phase && <Tag status={detail.phase} />}
            <ControlActions
              project={project}
              ticket={detail.id}
              status={detail.status}
              control={detail.control}
            />
          </div>
        }
      />
      <div className="px-6 py-4 w-full space-y-4">
      {detail.title && (
        <div className="text-lg" style={{ color: 'var(--text-secondary)' }}>{detail.title}</div>
      )}

      {/* Two-column: 720 main + 280 sidebar; collapses below 1024px */}
      <div className="ticket-detail-grid">
        <main className="min-w-0">
          {detail.control && (
            <ControlBanner
              project={project}
              ticket={detail.id}
              control={detail.control}
              workerRunning={workerRunning}
            />
          )}
          <div style={{ borderBottom: '1px solid var(--border-hairline)' }}>
            <nav className="flex gap-1 -mb-px overflow-x-auto">
              {tabs.map(t => (
                <button
                  key={t.key}
                  disabled={!t.available}
                  onClick={() => setTab(t.key)}
                  className="px-3 py-2 text-sm whitespace-nowrap"
                  style={{
                    borderBottom: '2px solid',
                    borderColor: tab === t.key ? 'var(--accent)' : 'transparent',
                    color: !t.available
                      ? 'var(--text-muted)'
                      : tab === t.key
                        ? 'var(--accent)'
                        : 'var(--text-secondary)',
                    cursor: !t.available ? 'not-allowed' : 'pointer',
                    fontWeight: tab === t.key ? 500 : 400,
                    opacity: !t.available ? 0.5 : 1,
                  }}
                >
                  {t.label}
                </button>
              ))}
            </nav>
          </div>

          <div className="pt-4">
            {tab === 'approval' && detail.approval && (
              <ApprovalPanel
                project={project}
                ticket={detail.id}
                approval={detail.approval}
                requirement={detail.requirement}
                plan={detail.plan}
                design={detail.design}
                prototype={detail.prototype}
              />
            )}
            {tab === 'requirement' && (detail.requirement
              ? <Markdown source={detail.requirement} />
              : <EmptyTab label="No requirement." />)}
            {tab === 'plan' && (detail.plan
              ? <Markdown source={detail.plan} />
              : <EmptyTab label="No plan." />)}
            {tab === 'manifest' && (detail.manifest
              ? <Markdown source={detail.manifest} />
              : <EmptyTab label="No manifest." />)}
            {tab === 'history' && <HistoryTimeline rows={detail.history} />}
            {tab === 'handoffs' && <FilesView files={detail.handoffs} />}
            {tab === 'verdicts' && <FilesView files={detail.verdicts} />}
            {tab === 'reviews' && <FilesView files={detail.reviews} />}
          </div>
        </main>

        <aside className="ticket-detail-sidebar">
          <PropertyList
            items={[
              { label: 'Status', value: detail.status },
              { label: 'Foreman', value: (
                <AssignRow
                  project={project}
                  ticket={detail.id}
                  assignee={detail.assignee}
                  foremen={snapshot.foremen ?? []}
                />
              ) },
              // Control sits under its own label, never folded into Status —
              // the two axes stay readable apart on the one screen that shows
              // both.
              { label: 'Control', value: detail.control
                  ? (
                    <span title={detail.control.at}>
                      <span style={{ color: controlTone(detail.control.state).fg }}>{detail.control.state}</span>
                      {' · '}{detail.control.actor || 'unknown'}
                      {' · '}{formatRelative(detail.control.at)}
                    </span>
                  )
                  : 'Active' },
              { label: 'Size', value: detail.size ?? '—' },
              { label: 'Parent', value: detail.parent
                  ? <a href={`#/tickets/${detail.parent}`} className="font-mono hover:underline" style={{ color: 'var(--accent)' }}>{detail.parent}</a>
                  : '—' },
              { label: 'Branch', value: <span className="font-mono break-all">{detail.branch ?? '—'}</span> },
              { label: 'Created', value: formatDate(detail.created_at) },
              { label: 'Updated', value: formatDate(detail.updated_at) },
              { label: 'Evidence', value: detail.evidence.length },
            ]}
          />

          {detail.repos.length > 0 && (
            <div className="mt-4">
              <div className="text-xs font-medium uppercase tracking-wide mb-1" style={{ color: 'var(--text-muted)' }}>
                Repos {detail.repos.length > 1 ? `(${detail.repos.length})` : ''}
              </div>
              <ul className="text-xs space-y-2">
                {detail.repos.map(r => (
                  <li
                    key={r.name ?? Math.random()}
                    className="rounded p-2 space-y-0.5"
                    style={{ border: '1px solid var(--border-hairline)', backgroundColor: 'var(--surface-elevated)' }}
                  >
                    <div className="flex items-center gap-2">
                      <span className="font-mono font-medium" style={{ color: 'var(--text-primary)' }}>{r.name ?? '—'}</span>
                      {r.pushed && (
                        <span
                          className="px-1.5 py-0.5 text-xs"
                          style={{ backgroundColor: 'var(--status-completed-bg, var(--surface-bg))', color: 'var(--status-completed-text, var(--text-secondary))', borderRadius: 'var(--radius-sm)' }}
                          title="Branch pushed to remote"
                        >
                          pushed
                        </span>
                      )}
                    </div>
                    {r.branch && (
                      <div className="font-mono break-all" style={{ color: 'var(--text-secondary)' }} title={r.branch}>
                        {r.branch}
                      </div>
                    )}
                    {r.worktree && r.worktree !== '.' && (
                      <div className="font-mono break-all" style={{ color: 'var(--text-muted)' }} title={r.worktree}>
                        {r.worktree}
                      </div>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          )}

          {Object.keys(detail.verdict_statuses).length > 0 && (
            <div className="mt-4">
              <div className="text-xs font-medium uppercase tracking-wide mb-1" style={{ color: 'var(--text-muted)' }}>
                Verdicts
              </div>
              <ul className="text-xs space-y-1">
                {Object.entries(detail.verdict_statuses).map(([skill, status]) => (
                  <li key={skill} className="flex items-center justify-between gap-2">
                    <span className="font-mono truncate" style={{ color: 'var(--text-secondary)' }}>{skill}</span>
                    <Tag tone={VERDICT_TONE[status] ?? 'muted'}>{status}</Tag>
                  </li>
                ))}
              </ul>
            </div>
          )}

          {detail.checkpoint && (
            <div
              className="mt-4 rounded p-3 text-xs space-y-1"
              style={{ border: '1px solid var(--border-hairline)', backgroundColor: 'var(--surface-elevated)' }}
            >
              <div className="uppercase tracking-wide font-medium" style={{ color: 'var(--text-muted)' }}>Checkpoint</div>
              <div>
                <span className="font-medium">{detail.checkpoint.workflow}</span>
                <span style={{ color: 'var(--text-muted)' }}> / </span>
                <span>{detail.checkpoint.step}</span>
              </div>
              <div style={{ color: 'var(--text-secondary)' }}>{detail.checkpoint.status}</div>
              {detail.checkpoint.note && (
                <div className="whitespace-pre-wrap" style={{ color: 'var(--text-secondary)' }}>{detail.checkpoint.note}</div>
              )}
            </div>
          )}

          {detail.evidence.length > 0 && (
            <div className="mt-4">
              <div className="text-xs font-medium uppercase tracking-wide mb-1" style={{ color: 'var(--text-muted)' }}>Files</div>
              <ul className="text-xs space-y-0.5" style={{ color: 'var(--text-secondary)' }}>
                {detail.evidence.map(e => (
                  <li key={e} title={e} className="font-mono truncate">{e}</li>
                ))}
              </ul>
            </div>
          )}
        </aside>
      </div>
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// Control — the human's override on the ticket. Every action here is
// reversible, and the copy says which one undoes which.
// ---------------------------------------------------------------------------

const UNDONE: Record<ControlAction, string> = {
  pause: 'paused',
  cancel: 'cancelled',
  resume: 'resumed',
  restore: 'restored',
};

function useControlAction(project: string, ticket: string) {
  const { run, pending, error } = useMutation();
  const act = useCallback(
    (action: ControlAction, note = '') =>
      run(() => controlTicket(project, ticket, action, note), `${ticket} ${UNDONE[action]}`),
    [run, project, ticket],
  );
  return { act, pending, error };
}

/**
 * A small anchored panel. Kept local to this view rather than promoted to
 * components/: the design's two popovers (pause note, assign foreman) both live
 * here, and the only other one — FiltersPopover — is a filter panel with its own
 * draft/apply semantics, not a shell this could have reused.
 *
 * The caller supplies the `relative` wrapper and the trigger.
 */
function Popover({
  open,
  onClose,
  label,
  anchorRef,
  width = 260,
  children,
}: {
  open: boolean;
  onClose: () => void;
  label: string;
  /** The wrapper holding the trigger — excluded from the outside-click check,
   *  or a click on the trigger would close and re-open in the same gesture. */
  anchorRef: React.RefObject<HTMLElement | null>;
  width?: number;
  children: ReactNode;
}) {
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node;
      if (panelRef.current?.contains(t) || anchorRef.current?.contains(t)) return;
      onClose();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      }
    };
    window.addEventListener('mousedown', onDown);
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('mousedown', onDown);
      window.removeEventListener('keydown', onKey);
    };
  }, [open, onClose, anchorRef]);

  // Focus the first control so the panel is usable from the keyboard alone,
  // and hand focus back to the trigger on close — same contract as Modal.
  const returnTo = useRef<HTMLElement | null>(null);
  useEffect(() => {
    if (!open) return;
    returnTo.current = document.activeElement as HTMLElement | null;
    const t = window.setTimeout(() => {
      panelRef.current?.querySelector<HTMLElement>('textarea, input, button')?.focus();
    }, 0);
    return () => {
      window.clearTimeout(t);
      returnTo.current?.focus();
    };
  }, [open]);

  if (!open) return null;

  return (
    <div
      ref={panelRef}
      role="dialog"
      aria-label={label}
      className="absolute right-0 mt-1 z-30 p-3 space-y-2"
      style={{
        width,
        backgroundColor: 'var(--surface-bg)',
        color: 'var(--text-primary)',
        border: '1px solid var(--border-hairline)',
        borderRadius: 'var(--radius-md)',
        boxShadow: 'var(--shadow-popover)',
        transformOrigin: 'top right',
        animation: 'bbs-popover-in var(--dur-fast) var(--ease-out)',
      }}
    >
      {children}
    </div>
  );
}

/**
 * The banner is what makes reversibility legible: the undo action sits inside
 * the same box as the state, and the cancel copy names the rung it restores to.
 */
function ControlBanner({
  project,
  ticket,
  control,
  workerRunning,
}: {
  project: string;
  ticket: string;
  control: TicketControl;
  workerRunning: boolean;
}) {
  const tone = controlTone(control.state);
  const { reason } = useControlPlane();
  const { act, pending, error } = useControlAction(project, ticket);
  const cancelled = control.state === 'cancelled';

  return (
    <div
      className="mb-4 p-3"
      style={{
        backgroundColor: tone.bg,
        border: '1px solid var(--border-hairline)',
        borderRadius: 'var(--radius-md)',
      }}
    >
      <div className="flex items-start justify-between gap-3 flex-wrap">
        <div className="text-sm min-w-0" style={{ color: 'var(--text-primary)' }}>
          <div>
            <span className="font-mono uppercase font-semibold" style={{ color: tone.fg }}>
              <span aria-hidden="true">▮ </span>{control.state}
            </span>
            <span style={{ color: 'var(--text-muted)' }}> · by </span>
            <span className="font-medium">{control.actor || 'unknown'}</span>
            <span style={{ color: 'var(--text-muted)' }}> · </span>
            <span title={control.at} style={{ color: 'var(--text-secondary)' }}>{formatRelative(control.at)}</span>
            {control.note && (
              <span style={{ color: 'var(--text-secondary)' }}> — “{control.note}”</span>
            )}
          </div>
          {cancelled && (
            <div className="mt-1 text-xs" style={{ color: 'var(--text-secondary)' }}>
              Nothing was deleted; files, history, and branch are intact.
            </div>
          )}
          {workerRunning && (
            <div className="mt-1 text-xs" style={{ color: 'var(--text-secondary)' }}>
              A worker is still running for this ticket; it will finish its current pass.
            </div>
          )}
        </div>
        <Button
          size="lg"
          onClick={() => act(cancelled ? 'restore' : 'resume')}
          disabled={pending}
          {...gateProps(reason)}
        >
          {/* Naming the rung is the point — but the rung is whichever one the
              cancel interrupted, not always `planned`. */}
          {cancelled ? `Restore to ${control.prior_status || 'its status'}` : 'Resume'}
        </Button>
      </div>
      {error && <div className="mt-2"><ErrorBox title="Could not update this ticket" body={error} /></div>}
    </div>
  );
}

function ControlActions({
  project,
  ticket,
  status,
  control,
}: {
  project: string;
  ticket: string;
  status: string;
  control: TicketControl | null;
}) {
  const { reason } = useControlPlane();
  const { act, pending, error } = useControlAction(project, ticket);
  const [pauseOpen, setPauseOpen] = useState(false);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [note, setNote] = useState('');
  const pauseAnchor = useRef<HTMLDivElement>(null);

  const cancelled = control?.state === 'cancelled';

  const pause = async () => {
    if (await act('pause', note.trim())) {
      setPauseOpen(false);
      setNote('');
    }
  };

  const cancel = async () => {
    if (await act('cancel')) setCancelOpen(false);
  };

  return (
    <div className="flex items-center gap-2">
      {/* The action that failed keeps its message next to it; the banner and
          the popovers each own theirs, so this covers the bare top-bar clicks. */}
      {error && !pauseOpen && !cancelOpen && (
        <span
          className="truncate"
          style={{ fontSize: 12, color: 'var(--status-blocked-text)', maxWidth: 220 }}
          title={error}
        >
          {error}
        </span>
      )}

      {/* One at a time: `control` is a single field, so a paused ticket has no
          cancel to offer — the server rejects it. The pair swaps to whichever
          undo the state actually has. */}
      {control ? (
        <Button
          onClick={() => act(cancelled ? 'restore' : 'resume')}
          disabled={pending}
          {...gateProps(reason)}
        >
          {cancelled ? 'Restore' : 'Resume'}
        </Button>
      ) : (
        <>
          <div className="relative" ref={pauseAnchor}>
            <Button
              onClick={() => setPauseOpen(o => !o)}
              aria-haspopup="dialog"
              aria-expanded={pauseOpen}
              {...gateProps(reason)}
            >
              Pause
            </Button>
            <Popover
              open={pauseOpen}
              onClose={() => setPauseOpen(false)}
              label="Pause ticket"
              anchorRef={pauseAnchor}
              width={280}
            >
              <Field label="Note" hint="Optional — why it is paused, for whoever finds it next.">
                {id => (
                  <textarea
                    id={id}
                    rows={3}
                    style={{ ...inputStyle, resize: 'vertical' }}
                    value={note}
                    onChange={e => setNote(e.target.value)}
                  />
                )}
              </Field>
              {error && <ErrorBox title="Could not pause" body={error} />}
              <div className="flex justify-end gap-2">
                <Button onClick={() => setPauseOpen(false)}>Cancel</Button>
                <Button variant="primary" onClick={pause} disabled={pending}>
                  {pending ? 'Pausing…' : 'Pause'}
                </Button>
              </div>
            </Popover>
          </div>

          <Button onClick={() => setCancelOpen(true)} {...gateProps(reason)}>
            Cancel
          </Button>
        </>
      )}

      <Modal
        open={cancelOpen}
        onClose={() => setCancelOpen(false)}
        title={`Cancel ${ticket}?`}
        actions={
          <>
            <Button size="lg" onClick={() => setCancelOpen(false)}>Keep working</Button>
            <Button size="lg" variant="primary" onClick={cancel} disabled={pending}>
              {pending ? 'Cancelling…' : 'Cancel ticket'}
            </Button>
          </>
        }
      >
        <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
          This stops the foreman from dispatching {ticket}. It does not delete the ticket, its
          files, or its branch, and an in-flight worker is left alone. You can restore it to{' '}
          <span className="font-mono">{status}</span> at any time.
        </p>
        {error && <ErrorBox title="Could not cancel" body={error} />}
      </Modal>
    </div>
  );
}

/** Foreman property row — reads as a value, opens the assign popover on click. */
function AssignRow({
  project,
  ticket,
  assignee,
  foremen,
}: {
  project: string;
  ticket: string;
  assignee: string | null;
  foremen: ForemanRow[];
}) {
  const { reason } = useControlPlane();
  const { run, pending, error } = useMutation();
  const [open, setOpen] = useState(false);
  const anchor = useRef<HTMLDivElement>(null);

  // Assigning to a foreman that stopped beating would silently park the ticket
  // in an inbox nobody reads, so the list only offers live ones — plus whoever
  // holds it now, which must stay visible to be removable.
  const options = foremen.filter(f => foremanLive(f) || f.id === assignee);

  const choose = async (id: string) => {
    const ok = await run(
      () => assignTicket(project, ticket, id),
      id ? `${ticket} assigned to ${id}` : `${ticket} unassigned`,
    );
    if (ok) setOpen(false);
  };

  return (
    <div className="relative" ref={anchor}>
      <button
        type="button"
        onClick={() => setOpen(o => !o)}
        aria-haspopup="dialog"
        aria-expanded={open}
        className="font-mono text-left hover:underline"
        style={{ color: assignee ? 'var(--accent)' : 'var(--text-muted)' }}
        {...gateProps(reason)}
      >
        {assignee || 'Unassigned'}
      </button>
      <Popover open={open} onClose={() => setOpen(false)} label="Assign foreman" anchorRef={anchor} width={240}>
        {options.length === 0 ? (
          <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
            No live foremen. Spawn one from <a href="#/foremen" style={{ color: 'var(--accent)' }}>Foremen</a>.
          </div>
        ) : (
          <ul className="space-y-0.5">
            {options.map(f => (
              <li key={f.id}>
                <AssignOption
                  label={f.id}
                  selected={f.id === assignee}
                  disabled={pending}
                  onClick={() => choose(f.id)}
                />
              </li>
            ))}
          </ul>
        )}
        {assignee && (
          <AssignOption label="Unassign" selected={false} disabled={pending} onClick={() => choose('')} />
        )}
        {error && <ErrorBox title="Could not assign" body={error} />}
      </Popover>
    </div>
  );
}

function AssignOption({
  label,
  selected,
  disabled,
  onClick,
}: {
  label: string;
  selected: boolean;
  disabled: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className="w-full text-left px-2 py-1 font-mono truncate"
      style={{
        fontSize: 12,
        borderRadius: 'var(--radius-sm)',
        color: selected ? 'var(--accent)' : 'var(--text-primary)',
        backgroundColor: selected ? 'var(--accent-bg-subtle)' : 'transparent',
        cursor: disabled ? 'not-allowed' : 'pointer',
      }}
      onMouseEnter={e => { if (!selected) e.currentTarget.style.backgroundColor = 'var(--surface-hover)'; }}
      onMouseLeave={e => { if (!selected) e.currentTarget.style.backgroundColor = 'transparent'; }}
    >
      {label}
    </button>
  );
}

const VERDICT_TONE: Record<string, 'ok' | 'warn' | 'err' | 'muted' | 'info' | 'accent'> = {
  DONE: 'ok',
  DONE_WITH_CONCERNS: 'warn',
  BLOCKED: 'err',
  NEEDS_CONTEXT: 'warn',
  none: 'muted',
};

function EmptyTab({ label }: { label: string }) {
  return <div className="text-sm" style={{ color: 'var(--text-muted)' }}>{label}</div>;
}

function PropertyList({ items }: { items: { label: string; value: React.ReactNode }[] }) {
  return (
    <dl className="text-xs space-y-2">
      {items.map(it => (
        <div key={it.label}>
          <dt className="uppercase tracking-wide font-medium mb-0.5" style={{ color: 'var(--text-muted)' }}>{it.label}</dt>
          <dd style={{ color: 'var(--text-primary)' }}>{it.value}</dd>
        </div>
      ))}
    </dl>
  );
}

function dayKey(ts: string): string {
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return ts.slice(0, 10);
  return d.toISOString().slice(0, 10);
}

function HistoryTimeline({ rows }: { rows: HistoryRow[] }) {
  const groups = useMemo(() => {
    const m = new Map<string, HistoryRow[]>();
    for (const r of rows) {
      const k = dayKey(r.ts);
      if (!m.has(k)) m.set(k, []);
      m.get(k)!.push(r);
    }
    return Array.from(m.entries()).sort((a, b) => b[0].localeCompare(a[0]));
  }, [rows]);

  if (rows.length === 0) {
    return <div className="text-sm" style={{ color: 'var(--text-muted)' }}>No history.</div>;
  }

  return (
    <div className="space-y-4">
      {groups.map(([day, items]) => (
        <section key={day}>
          <h3 className="text-xs font-semibold uppercase tracking-wide mb-2" style={{ color: 'var(--text-muted)' }}>
            {day}
          </h3>
          <ul
            className="space-y-2 pl-4"
            style={{ borderLeft: '1px solid var(--border-hairline)' }}
          >
            {items.map((r, i) => (
              <li key={i} className="text-sm relative">
                <span
                  aria-hidden="true"
                  className="absolute -left-[17px] top-1.5 w-2 h-2 rounded-full"
                  style={{ backgroundColor: 'var(--text-muted)' }}
                />
                <div className="flex items-baseline gap-2">
                  <span className="text-xs font-mono" style={{ color: 'var(--text-muted)' }} title={r.ts}>
                    {r.ts.slice(11, 16) || ''}
                  </span>
                  <span style={{ color: 'var(--text-primary)' }}>
                    {r.workflow ?? r.event}
                    {r.step ? <span style={{ color: 'var(--text-muted)' }}> / {r.step}</span> : null}
                    {r.status ? <span style={{ color: 'var(--text-secondary)' }}> — {r.status}</span> : null}
                  </span>
                </div>
                {r.note && (
                  <div className="mt-0.5 whitespace-pre-wrap text-xs" style={{ color: 'var(--text-secondary)' }}>
                    {r.note}
                  </div>
                )}
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}

function FilesView({ files }: { files: { name: string; body: string }[] }) {
  if (files.length === 0) return <div className="text-sm" style={{ color: 'var(--text-muted)' }}>None.</div>;
  return (
    <div className="space-y-4">
      {files.map(f => (
        <details
          key={f.name}
          className="rounded"
          style={{ border: '1px solid var(--border-hairline)', backgroundColor: 'var(--surface-bg)' }}
        >
          <summary
            className="px-3 py-2 cursor-pointer font-mono text-sm"
            style={{ color: 'var(--text-secondary)' }}
          >
            {f.name}
          </summary>
          <div className="px-4 pb-4 pt-2" style={{ borderTop: '1px solid var(--border-hairline)' }}>
            <Markdown source={f.body} />
          </div>
        </details>
      ))}
    </div>
  );
}

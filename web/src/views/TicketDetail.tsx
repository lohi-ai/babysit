import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { ArrowRight, Check, ChevronDown, Clock, Play, TriangleAlert } from 'lucide-react';
import { fetchTicketDetail, foremanLive, type DagNode, type DagState, type ForemanRow, type HistoryRow, type ManifestRepo, type NamedFile, type Snapshot, type TicketApproval, type TicketControl, type TicketDetail as TicketDetailData, type TicketDag, type TicketSummary } from '../lib/data';
import { assignTicket, controlTicket, ticketReadiness, type ApiError, type ControlAction, type ReadinessResult } from '../lib/api';
import { Button } from '../components/Button';
import { Tag } from '../components/Tag';
import { ErrorBox } from '../components/ErrorBox';
import { Field, inputStyle } from '../components/Field';
import { Markdown } from '../components/Markdown';
import { PrototypeFrame } from '../components/PrototypeFrame';
import { ApprovalPanel } from './ApprovalPanel';
import { ProjectPanel } from './ProjectPanel';
import { Modal } from '../components/Modal';
import { controlTone } from '../components/ControlChip';
import { TopBar } from '../components/TopBar';
import { formatDate, formatRelative } from '../lib/format';
import { useFilter } from '../contexts/FilterContext';
import { gateProps, useControlPlane, useMutation } from '../contexts/ControlContext';
import { useScopedTicketDetail, useScopedTickets } from '../lib/scope';

// The documents the page is *for*, then everything else behind one menu.
// Split rather than ordered, because the split is what keeps the strip from
// overflowing: eight tabs pushed Reviews off a 1440px screen entirely.
type Tab =
  | 'project' | 'report' | 'requirement' | 'plan' | 'dag' | 'prototype' | 'handoffs' | 'qa'
  | 'activity' | 'reviews' | 'manifest' | 'repos' | 'approval';

const OVERFLOW: Tab[] = ['activity', 'reviews', 'manifest', 'repos', 'approval'];

// One panel serves every tab, so the association is a constant rather than a
// per-tab id. Without it the tablist announces "tab 2 of 4" over a panel a
// screen reader has no way to reach from the tab.
const TABPANEL_ID = 'ticket-tabpanel';

/** A tab as the strip renders it. `count` is omitted where a number says nothing. */
interface TabSpec {
  key: Tab;
  label: string;
  count?: number;
}

export function TicketDetail({ snapshot, ticketId }: { snapshot: Snapshot; ticketId: string }) {
  const { state } = useFilter();
  const embedded = useScopedTicketDetail(snapshot, state.project, ticketId);
  const { mode } = useControlPlane();

  // Served snapshots carry summaries only — the detail body loads on demand.
  // The summary doubles as the staleness signal: index.json's updated_at and
  // the run's checkpoint stamp are the two fields a mutation or a running
  // worker bumps, so a change in either means the cached detail is behind.
  const summary = useMemo(() => {
    for (const p of Object.values(snapshot.projects)) {
      const s = p.tickets.find(t => t.id === ticketId);
      if (s) return s;
    }
    return undefined;
  }, [snapshot, ticketId]);

  // Every mutation endpoint is scoped by project slug, and the detail can be
  // reached with the project filter on `all`, so the owning slug is looked up
  // rather than taken from the filter. Served snapshots have no ticketDetail
  // map to search — the summaries carry the same membership fact.
  const project = useMemo(() => {
    if (state.project !== 'all') return state.project;
    for (const [slug, p] of Object.entries(snapshot.projects)) {
      if (p.ticketDetail[ticketId]) return slug;
      if (p.tickets.some(t => t.id === ticketId)) return slug;
    }
    return '';
  }, [snapshot, state.project, ticketId]);

  // The last successfully loaded detail plus the project it came from, kept
  // across polls so a failed refetch degrades to a stale banner instead of
  // blanking the page — and so a ticket that vanishes from the summaries
  // (trashed mid-view under the 'all' filter) can still be re-fetched once
  // to learn its 404.
  const [fetched, setFetched] = useState<{ project: string; detail: TicketDetailData } | null>(null);
  const [detailError, setDetailError] = useState<{ id: string; status: number; message: string } | null>(null);
  const [loading, setLoading] = useState(false);
  const [retryNonce, setRetryNonce] = useState(0);
  const detailCtl = useRef<AbortController | null>(null);

  const detail = embedded ?? (fetched?.detail.id === ticketId ? fetched.detail : null);
  const fetchProject = project || (fetched?.detail.id === ticketId ? fetched.project : '');

  const stale =
    !!detail &&
    ((summary?.updated_at ?? null) !== (detail.updated_at ?? null) ||
      (summary?.run?.updated_at ?? null) !== (detail.checkpoint?.updated_at ?? null));

  const needsFetch =
    !embedded && mode !== 'readonly' && fetchProject !== '' && (!detail || stale);

  useEffect(() => {
    if (!needsFetch) return;
    // A dep change mid-fetch (a poll bumped the summary while a detail was
    // loading) retires the old request — its response must never overwrite
    // the newer one.
    detailCtl.current?.abort();
    // Own controller, independent of useSnapshot's poll: a poll tick must
    // never abort a detail fetch in flight, and a detail fetch must never
    // hold up the next poll.
    const ctl = new AbortController();
    detailCtl.current = ctl;
    setLoading(true);
    fetchTicketDetail(fetchProject, ticketId, ctl.signal).then(
      d => {
        if (ctl.signal.aborted) return;
        setFetched({ project: fetchProject, detail: d });
        setDetailError(null);
        setLoading(false);
      },
      (e: unknown) => {
        if (ctl.signal.aborted) return;
        const err = e as Partial<ApiError>;
        setDetailError({
          id: ticketId,
          status: typeof err.status === 'number' ? err.status : 0,
          message: e instanceof Error ? e.message : String(e),
        });
        setLoading(false);
      },
    );
  }, [needsFetch, fetchProject, ticketId, stale, retryNonce]);
  // Abort on ticket change or unmount — never on a poll re-render. The
  // aborted fetch's settle handlers no-op, so loading is reset here too:
  // an unknown ticket id must land on not-found, not a spinner that never
  // resolves.
  useEffect(() => {
    return () => {
      detailCtl.current?.abort();
      detailCtl.current = null;
      setLoading(false);
    };
  }, [ticketId]);

  const error = detailError?.id === ticketId ? detailError : null;
  const retry = useCallback(() => {
    setDetailError(null);
    setRetryNonce(n => n + 1);
  }, []);

  const [readiness, setReadiness] = useState<ReadinessResult | null>(null);
  // The parent's children resolved against the scoped list — the strip renders
  // each child's status, so it needs the summaries, not just the ids.
  const scopedTickets = useScopedTickets(snapshot, project || 'all');
  const childTickets = useMemo(() => {
    if (!detail?.children?.length) return [];
    const byId = new Map(scopedTickets.map(t => [t.id, t]));
    return detail.children
      .map(id => byId.get(id))
      .filter((t): t is NonNullable<typeof t> => !!t);
  }, [detail, scopedTickets]);

  useEffect(() => {
    if (mode !== 'served' || !project) {
      setReadiness(null);
      return;
    }
    let active = true;
    ticketReadiness(project, ticketId).then(
      result => { if (active) setReadiness(result); },
      error => { if (active) setReadiness({ available: false, action: 'pr', reason: String(error) }); },
    );
    return () => { active = false; };
  }, [mode, project, ticketId]);

  // Pause and cancel never touch a running session — this is what says so.
  const workerRunning = (snapshot.sessions?.sessions ?? []).some(s => s.ticket === ticketId);
  // Same signal one line up in the strip: assigned is not the same as running.
  const runner = workerRunning ? 'autopilot' : null;

  // Only tabs with something in them are rendered at all. A greyed-out label
  // still costs the width that pushed real tabs off the strip, and "Plan"
  // unclickable teaches nothing that "no Plan tab" does not.
  const qaCount = detail ? detail.verdicts.length + detail.evidence.length : 0;
  const tabs: TabSpec[] = detail ? ([
    { key: 'project',     label: 'Project',     available: !!detail.project_contract || !!detail.manifest || !!detail.children?.length },
    { key: 'report',      label: 'Project report', available: !!detail.report },
    { key: 'requirement', label: 'Requirement', available: !!detail.requirement },
    { key: 'plan',        label: 'Plan',        available: !!detail.plan },
    { key: 'dag',         label: 'DAG',         count: detail.dag?.counts.nodes, available: !!detail.dag },
    { key: 'prototype',   label: 'Prototype',   available: !!detail.prototype },
    { key: 'handoffs',    label: 'Handoffs',    count: detail.handoffs.length, available: detail.handoffs.length > 0 },
    { key: 'qa',          label: 'QA evidence', count: qaCount, available: qaCount > 0 },
    { key: 'activity',    label: 'Activity',    count: detail.history.length, available: detail.history.length > 0 },
    { key: 'reviews',     label: 'Reviews',     count: detail.reviews.length, available: detail.reviews.length > 0 },
    { key: 'manifest',    label: 'Manifest',    available: !!detail.manifest },
    { key: 'repos',       label: 'Repos',       count: detail.repos.length, available: detail.repos.length > 0 },
    { key: 'approval',    label: 'Design checkpoint', available: !!detail.approval },
  ] satisfies (TabSpec & { available: boolean })[]).filter(t => t.available).map(({ key, label, count }) => ({ key, label, count })) : [];

  const primaryTabs = tabs.filter(t => !OVERFLOW.includes(t.key));
  const overflowTabs = tabs.filter(t => OVERFLOW.includes(t.key));

  const [tab, setTab] = useState<Tab>(tabs[0]?.key ?? 'requirement');
  const activeTab = tabs.some(t => t.key === tab) ? tab : tabs[0]?.key ?? 'requirement';
  // Write the fallback back, so a selection the page has already stopped
  // honouring cannot come back to life. The page is not remounted between
  // tickets and polls every few seconds: without this, following a Parent link
  // to a ticket that has no Reviews yet leaves `tab` on 'reviews', and the
  // panel jumps off whatever the human is reading the moment review-pr writes
  // its file.
  useEffect(() => { setTab(activeTab); }, [activeTab]);

  // A 404 is authoritative even over a cached detail: the ticket is gone.
  if (error?.status === 404) {
    return <ErrorBox title="Ticket not found" body={`No detail for ${ticketId} — the server has no such ticket.`} />;
  }
  if (!detail) {
    if (error) {
      return (
        <>
          <TopBar title={ticketId} warnings={snapshot.meta.warnings} />
          <div className="px-6 py-4 w-full space-y-3">
            <ErrorBox
              title="Cannot reach the dashboard server"
              body={`Ticket details could not be loaded: ${error.message}`}
            />
            <div>
              <Button size="sm" onClick={retry}>Retry</Button>
            </div>
          </div>
        </>
      );
    }
    if (mode !== 'readonly' && (loading || needsFetch)) {
      return (
        <>
          <TopBar title={ticketId} warnings={snapshot.meta.warnings} />
          <div className="px-6 py-4 w-full text-sm" style={{ color: 'var(--text-tertiary)' }}>
            Loading ticket…
          </div>
        </>
      );
    }
    return (
      <>
        <TopBar title={ticketId} warnings={snapshot.meta.warnings} />
        <div className="px-6 py-4 w-full">
          <ErrorBox title="Ticket not found" body={`No detail for ${ticketId} in this snapshot.`} />
        </div>
      </>
    );
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
        warnings={snapshot.meta.warnings}
        breadcrumb={breadcrumb}
        actions={
          // Status and phase used to sit here too. The strip below carries both
          // now, and the bar does not stick — printing them twice on one screen
          // is the noise this page was asked to lose.
          <div className="flex items-center gap-3">
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

      {detail.control && (
        <ControlBanner
          project={project}
          ticket={detail.id}
          control={detail.control}
          workerRunning={workerRunning}
        />
      )}

      {error && (
        <div className="flex items-center gap-3">
          <div className="flex-1 min-w-0">
            <ErrorBox
              title="Error loading detail"
              body={`Ticket details could not be loaded. Showing the last data received. ${error.message}`}
            />
          </div>
          <Button size="sm" onClick={retry}>Retry</Button>
        </div>
      )}

      {detail.approval?.state === 'pending' && (
        <ApprovalCallout
          approval={detail.approval}
          onOpen={() => setTab('approval')}
          isOpen={activeTab === 'approval'}
        />
      )}

      <StatusStrip
        detail={detail}
        running={runner}
        readiness={readiness}
        childTickets={childTickets}
        foreman={
          <AssignRow
            project={project}
            ticket={detail.id}
            assignee={detail.assignee}
            foremen={snapshot.foremen ?? []}
          />
        }
      />

      <div>
        <div
          className="flex items-end justify-between gap-2"
          style={{ borderBottom: '1px solid var(--border-hairline)' }}
        >
          <nav className="flex gap-1 -mb-px overflow-x-auto" role="tablist">
            {primaryTabs.map(t => (
              <TabButton key={t.key} tab={t} active={activeTab === t.key} onClick={() => setTab(t.key)} />
            ))}
          </nav>
          {overflowTabs.length > 0 && (
            <OverflowTabs
              tabs={overflowTabs}
              active={activeTab}
              onPick={setTab}
              pending={detail.approval?.state === 'pending'}
            />
          )}
        </div>

        <div className="pt-4" id={TABPANEL_ID} role="tabpanel">
          {activeTab === 'project' && <ProjectPanel project={project} ticket={detail.id} />}
          {activeTab === 'report' && detail.report && <Markdown source={detail.report} />}
          {activeTab === 'requirement' && detail.requirement && <Markdown source={detail.requirement} />}
          {activeTab === 'plan' && detail.plan && <Markdown source={detail.plan} />}
          {activeTab === 'dag' && detail.dag && <DagPanel dag={detail.dag} />}
          {activeTab === 'prototype' && (
            <PrototypeFrame
              project={project}
              ticket={detail.id}
              prototype={detail.prototype}
              height={720}
            />
          )}
          {activeTab === 'handoffs' && <FilesView files={detail.handoffs} />}
          {activeTab === 'qa' && <QaEvidence verdicts={detail.verdicts} evidence={detail.evidence} />}
          {activeTab === 'activity' && <HistoryTimeline rows={detail.history} />}
          {activeTab === 'reviews' && <FilesView files={detail.reviews} />}
          {activeTab === 'manifest' && detail.manifest && <Markdown source={detail.manifest} />}
          {activeTab === 'repos' && <ReposList repos={detail.repos} />}
          {activeTab === 'approval' && detail.approval && (
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
          {tabs.length === 0 && (
            <EmptyTab label="Nothing on this ticket yet — documents appear as autopilot produces them." />
          )}
        </div>
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

// ---------------------------------------------------------------------------
// The project DAG — a decomposed ticket's fan-out, in the waves the server
// layered it into. Every edge, wave and state is lifted from the snapshot
// (`internal/ticket/dag.go`, the same model as `bbs ticket dag --json`); this
// panel groups and colours what it is handed and derives nothing, which is what
// keeps the page and the command from disagreeing.
// ---------------------------------------------------------------------------

// One word per graph state, the glyph beside it, and the rail that repeats it.
// The word is uppercased by CSS and sits in the same place on every card, so
// colour and glyph are never the only carriers of state. `not_found` is muted
// rather than alarming: a dangling id is a note about the data, not a failure.
const DAG_STATE: Record<DagState, { label: string; tone: string; rail: string; glyph: typeof Check }> = {
  done:      { label: 'done',      tone: 'var(--status-completed-text)', rail: 'var(--status-completed-text)', glyph: Check },
  running:   { label: 'running',   tone: 'var(--status-started-text)',  rail: 'var(--status-started-text)',  glyph: Play },
  ready:     { label: 'ready',     tone: 'var(--accent)',               rail: 'var(--accent)',               glyph: ArrowRight },
  waiting:   { label: 'waiting',   tone: 'var(--status-blocked-text)',  rail: 'var(--status-blocked-text)',  glyph: Clock },
  not_found: { label: 'not found', tone: 'var(--text-muted)',           rail: 'var(--border-emphasis)',      glyph: TriangleAlert },
};

// The order a wave's per-state meta reads in. Zero counts drop out, so a lane
// says what is in it and nothing else.
const DAG_META_ORDER: DagState[] = ['done', 'not_found', 'running', 'ready', 'waiting'];

function dagMeta(nodes: DagNode[]): string {
  return DAG_META_ORDER
    .map(state => [state, nodes.filter(n => n.state === state).length] as const)
    .filter(([, count]) => count > 0)
    .map(([state, count]) => `${count} ${DAG_STATE[state].label}`)
    .join(' · ');
}

const DAG_CARD_STYLE: CSSProperties = {
  borderWidth: '1px',
  borderColor: 'var(--border-hairline)',
  borderRadius: 'var(--radius-md)',
  padding: '8px 10px',
  minWidth: 0,
  display: 'flex',
  flexDirection: 'column',
  gap: '3px',
};

const DAG_META_STYLE: CSSProperties = {
  fontSize: '11px',
  color: 'var(--text-secondary)',
  textTransform: 'uppercase',
  letterSpacing: 'var(--tracking-caption)',
};

function DagPanel({ dag }: { dag: TicketDag }) {
  const byId = useMemo(() => new Map(dag.nodes.map(n => [n.id, n])), [dag.nodes]);
  const outside = dag.nodes.filter(n => n.external);
  const c = dag.counts;

  return (
    <div>
      <div
        className="flex flex-wrap items-baseline gap-x-3 gap-y-1"
        style={{ color: 'var(--text-secondary)', fontSize: '12px', marginBottom: '14px' }}
      >
        <span style={{ color: 'var(--text-primary)', fontWeight: 500 }}>
          DAG · root <span className="font-mono">{dag.root}</span>
        </span>
        <span>
          {c.nodes} tickets · {c.waves} waves · {c.ready} ready · {c.waiting} waiting
          {' · '}{c.external} outside · {c.dangling} not found · {dag.cycles.length} cycles
        </span>
      </div>
      {dag.cycles.length > 0 && (
        <p
          role="status"
          className="flex items-start gap-2 mb-3"
          style={{
            border: '1px solid var(--status-blocked-text)',
            borderRadius: 'var(--radius-md)',
            padding: '10px 12px',
            color: 'var(--text-secondary)',
            fontSize: '12px',
          }}
        >
          <TriangleAlert
            size={14}
            aria-hidden
            className="shrink-0"
            style={{ marginTop: '2px', color: 'var(--status-blocked-text)' }}
          />
          <span>
            Cycle. <strong style={{ color: 'var(--status-blocked-text)', fontWeight: 600 }}>
              {dag.cycles.map(cyc => [...cyc, cyc[0]].join(' → ')).join('; ')}
            </strong>
            {' '}— these tickets block each other, so no wave can go first.
          </span>
        </p>
      )}
      {outside.length > 0 && (
        <DagLane
          caption="Outside this subtree"
          meta="blockers that live in another branch of the project"
          nodes={outside}
          byId={byId}
        />
      )}
      {dag.waves.map((wave, i) => {
        const nodes = wave.map(id => byId.get(id)).filter((n): n is DagNode => !!n);
        return <DagLane key={i} caption={`Wave ${i}`} meta={dagMeta(nodes)} nodes={nodes} byId={byId} />;
      })}
    </div>
  );
}

// One lane: a caption, its count, and the cards in it. Waves and the outside
// lane render through the same component, because a lane is a grouping, not a
// kind of thing — the cards themselves carry what differs.
function DagLane({ caption, meta, nodes, byId }: {
  caption: string;
  meta: string;
  nodes: DagNode[];
  byId: Map<string, DagNode>;
}) {
  return (
    <section style={{ marginBottom: '14px' }}>
      <h3 className="flex flex-wrap items-center gap-2" style={{ marginBottom: '6px' }}>
        <span
          className="uppercase"
          style={{ fontSize: '11px', fontWeight: 500, letterSpacing: 'var(--tracking-caption)', color: 'var(--text-secondary)' }}
        >
          {caption}
        </span>
        <span
          style={{
            fontSize: '11px',
            color: 'var(--text-muted)',
            background: 'var(--surface-elevated)',
            borderRadius: 'var(--radius-sm)',
            minWidth: '18px',
            textAlign: 'center',
            padding: '0 4px',
          }}
        >
          {nodes.length}
        </span>
        {meta && <span style={{ fontSize: '11px', color: 'var(--text-secondary)' }}>{meta}</span>}
      </h3>
      <div
        className="grid gap-2"
        style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(min(228px, 100%), 1fr))', alignItems: 'start' }}
      >
        {nodes.map(n => <DagCard key={n.id} node={n} byId={byId} />)}
      </div>
    </section>
  );
}

function DagCard({ node, byId }: { node: DagNode; byId: Map<string, DagNode> }) {
  const state = DAG_STATE[node.state] ?? DAG_STATE.not_found;
  const Glyph = state.glyph;
  // A settled card is an outcome, not a set of questions: its status and
  // verdicts say nothing the state word has not already said, so it renders as
  // one row and leaves the wave's actionable set to the eye.
  const compact = node.state === 'done' || node.state === 'not_found';
  const nested = node.children.map(id => byId.get(id)).filter((n): n is DagNode => !!n);
  const blockers = node.blocked_by.map(id => ({ id, blocker: byId.get(id) }));
  const chip = node.dangling ? 'not found' : node.external ? 'outside' : null;

  return (
    <section
      style={{
        ...DAG_CARD_STYLE,
        ...(compact ? { flexDirection: 'row', alignItems: 'center', gap: '6px', padding: '5px 10px' } : {}),
        borderStyle: node.external || node.state === 'not_found' ? 'dashed' : 'solid',
        ...(node.external ? { background: 'var(--surface-elevated)' } : {}),
        borderLeft: `2px solid ${state.rail}`,
      }}
    >
      <div className="flex flex-wrap items-center gap-1.5 min-w-0">
        <span
          className="inline-flex items-center gap-0.5 uppercase shrink-0"
          style={{ color: state.tone, fontSize: '10px', fontWeight: 600, letterSpacing: 'var(--tracking-caption)' }}
        >
          <Glyph size={11} aria-hidden />
          <span>{state.label}</span>
        </span>
        <a
          href={`#/tickets/${node.id}`}
          className="font-mono truncate"
          style={{ fontSize: '12px', fontWeight: 500, color: 'var(--text-primary)' }}
        >
          {node.id}
        </a>
        <span className="ml-auto font-mono shrink-0" style={{ fontSize: '11px', color: 'var(--text-muted)' }}>
          {node.position ? `#${node.position}` : '—'}
        </span>
        {chip && (
          <span
            className="uppercase shrink-0"
            style={{
              fontSize: '10px',
              letterSpacing: 'var(--tracking-caption)',
              borderRadius: 'var(--radius-sm)',
              border: '1px solid',
              padding: '0 3px',
              ...(node.dangling
                ? { color: 'var(--status-blocked-text)', borderColor: 'currentColor' }
                : { color: 'var(--text-muted)', borderColor: 'var(--border-hairline)' }),
            }}
          >
            {chip}
          </span>
        )}
      </div>
      {!compact && (
        <>
          <div style={DAG_META_STYLE}>
            {node.status || 'unknown'} · qa {node.qa} · review-pr {node.review_pr}
          </div>
          {node.external && node.parent && (
            <div style={{ fontSize: '11px', color: 'var(--text-muted)' }}>parent {node.parent}</div>
          )}
          {nested.length > 0 && (
            <div className="flex flex-wrap items-center gap-1" style={{ fontSize: '11px', color: 'var(--text-secondary)' }}>
              <span>› {nested.length} nested · {nested.filter(n => n.state === 'done').length} done</span>
              <span style={{ color: 'var(--text-muted)' }}>—</span>
              <a href={`#/tickets/${node.id}`} style={{ color: 'var(--accent)' }}>open its DAG</a>
            </div>
          )}
          {blockers.length > 0 && (
            <div style={{ fontSize: '11px', color: 'var(--text-secondary)', overflowWrap: 'anywhere' }}>
              blocked by{' '}
              {blockers.map((b, i) => (
                <span key={b.id}>
                  {i > 0 && ', '}
                  <a
                    href={`#/tickets/${b.id}`}
                    className="font-mono"
                    style={{ color: b.blocker?.state === 'done' ? 'var(--text-secondary)' : 'var(--status-blocked-text)' }}
                  >
                    {b.id}
                  </a>
                  {b.blocker && (
                    <span style={{ color: b.blocker.state === 'done' ? 'var(--text-secondary)' : 'var(--status-blocked-text)' }}>
                      {' '}{b.blocker.status}
                    </span>
                  )}
                </span>
              ))}
            </div>
          )}
        </>
      )}
    </section>
  );
}

function EmptyTab({ label }: { label: string }) {
  return <div className="text-sm" style={{ color: 'var(--text-muted)' }}>{label}</div>;
}

/**
 * The pending design checkpoint, said out loud above the fold.
 *
 * It points at the decision rather than carrying it: the buttons live in
 * ApprovalPanel, under the rubric and the documents, because approving from a
 * banner is exactly the unread rubber stamp that panel was built to prevent.
 */
function ApprovalCallout({
  approval,
  onOpen,
  isOpen,
}: {
  approval: TicketApproval;
  onOpen: () => void;
  isOpen: boolean;
}) {
  return (
    <div
      className="flex items-center justify-between gap-4 flex-wrap px-4 py-3"
      style={{
        border: '1px solid var(--accent)',
        borderRadius: 'var(--radius-md)',
        backgroundColor: 'var(--accent-bg-subtle)',
      }}
    >
      <div className="min-w-0">
        <div className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
          Design checkpoint — waiting on you
        </div>
        <div className="text-xs mt-0.5" style={{ color: 'var(--text-secondary)' }}>
          {approval.requested_by || 'a worker'} published a plan for review
          {approval.at && (
            <>
              {' · '}
              <span title={formatDate(approval.at)}>{formatRelative(approval.at)}</span>
            </>
          )}
        </div>
      </div>
      <Button size="lg" variant="primary" onClick={onOpen} disabled={isOpen}>
        {isOpen ? 'Reviewing below' : 'Review the design'}
      </Button>
    </div>
  );
}

/**
 * Status, across the top instead of down a 280px rail.
 *
 * The rail put eight one-line facts in a column tall enough to scroll past a
 * two-line requirement; the same facts wrap here and read in one glance.
 */
function StatusStrip({
  detail,
  running,
  readiness,
  childTickets,
  foreman,
}: {
  detail: TicketDetailData;
  running: string | null;
  readiness: ReadinessResult | null;
  childTickets: TicketSummary[];
  foreman: ReactNode;
}) {
  const qa = detail.verdict_statuses['qa'] ?? 'none';
  const review = detail.verdict_statuses['review-pr'] ?? 'none';
  // The checkpoint's own status and note ride along with the step: `blocked`
  // is the word that explains a ticket sitting still, and the note is where
  // autopilot writes what it is blocked on. Neither is shown anywhere else on
  // the page, so the cell carries both rather than dropping them.
  const cp = detail.checkpoint;
  const step = cp
    ? (
      <span title={cp.note || undefined}>
        {cp.workflow} / {cp.step}
        {cp.status && (
          <>
            {' · '}
            <span style={{ color: 'var(--text-muted)' }}>{cp.status}</span>
          </>
        )}
      </span>
    )
    : detail.phase ?? '—';

  const cells: { label: string; value: ReactNode }[] = [
    { label: 'Status', value: <Tag status={detail.status} /> },
    { label: 'Step', value: step },
    { label: 'Running', value: running
        ? <span className="font-mono" style={{ color: 'var(--status-in_progress-text, var(--accent))' }}>{running}</span>
        : <span style={{ color: 'var(--text-muted)' }}>—</span> },
    { label: 'Foreman', value: foreman },
    { label: 'QA', value: <Tag tone={VERDICT_TONE[qa] ?? 'muted'}>{qa}</Tag> },
    { label: 'Review', value: <Tag tone={VERDICT_TONE[review] ?? 'muted'}>{review}</Tag> },
    { label: 'Readiness', value: detail.project_contract || detail.children?.length ? <span>See Project tab</span> : readinessValue(readiness) },
    { label: 'Size', value: detail.size ?? '—' },
    { label: 'Updated', value: <span title={formatDate(detail.updated_at)}>{formatRelative(detail.updated_at)}</span> },
  ];
  if (detail.parent) {
    cells.push({
      label: 'Parent',
      value: <a href={`#/tickets/${detail.parent}`} className="font-mono hover:underline" style={{ color: 'var(--accent)' }}>{detail.parent}</a>,
    });
  }
  // The DAG edges the record carries: children resolved to live statuses so a
  // parent reads its fan-out at a glance, blocked_by names what is holding a
  // child back. Both are links — the strip is a map, not a report.
  if (childTickets.length > 0) {
    cells.push({
      label: 'Children',
      value: (
        <span className="flex flex-wrap gap-x-2 gap-y-0.5">
          {childTickets.map(c => (
            <a key={c.id} href={`#/tickets/${c.id}`} className="font-mono hover:underline" style={{ color: 'var(--accent)' }}>
              {c.id} <Tag status={c.status} />
            </a>
          ))}
        </span>
      ),
    });
  }
  const blockedBy = detail.relations?.blocked_by ?? [];
  if (blockedBy.length > 0) {
    cells.push({
      label: 'Blocked by',
      value: (
        <span className="flex flex-wrap gap-x-2 gap-y-0.5">
          {blockedBy.map(id => (
            <a key={id} href={`#/tickets/${id}`} className="font-mono hover:underline" style={{ color: 'var(--status-blocked-text)' }}>{id}</a>
          ))}
        </span>
      ),
    });
  }
  if (detail.branch) {
    // Truncated, not wrapped: a `feat/<ticket>_<slug>` branch is long enough to
    // triple the height of every cell in its row, and the tail is the part
    // already spelled out in the breadcrumb.
    cells.push({ label: 'Branch', value: <span className="block truncate font-mono" title={detail.branch}>{detail.branch}</span> });
  }
  // Control keeps its own axis — a paused ticket is not a status, and folding
  // the two together loses which one the human changed.
  if (detail.control) {
    cells.push({
      label: 'Control',
      value: (
        <span title={detail.control.at} style={{ color: controlTone(detail.control.state).fg }}>
          {detail.control.state}
        </span>
      ),
    });
  }

  return (
    <dl className="ticket-status-strip">
      {cells.map(c => (
        <div key={c.label}>
          <dt>{c.label}</dt>
          <dd>{c.value}</dd>
        </div>
      ))}
    </dl>
  );
}

function readinessValue(readiness: ReadinessResult | null): ReactNode {
  if (!readiness) return <span style={{ color: 'var(--text-muted)' }}>snapshot only</span>;
  if (!readiness.available) return <span title={readiness.reason} style={{ color: 'var(--text-muted)' }}>unavailable</span>;
  if (!readiness.enforced) return <span style={{ color: 'var(--text-muted)' }}>legacy</span>;
  if (readiness.ready) return <Tag tone="ok">ready</Tag>;
  const reasons = readiness.reason_codes?.join(', ') || 'not ready';
  return <span className="font-mono" title={reasons} style={{ color: 'var(--status-blocked-text)' }}>{reasons}</span>;
}

function TabButton({
  tab,
  active,
  onClick,
}: {
  tab: { key: Tab; label: string; count?: number };
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      role="tab"
      aria-selected={active}
      aria-controls={TABPANEL_ID}
      onClick={onClick}
      className="px-3 py-2 text-sm whitespace-nowrap"
      style={{
        borderBottom: '2px solid',
        borderColor: active ? 'var(--accent)' : 'transparent',
        color: active ? 'var(--accent)' : 'var(--text-secondary)',
        fontWeight: active ? 500 : 400,
      }}
    >
      {tab.label}
      {tab.count !== undefined && (
        <span className="ml-1.5 font-mono" style={{ fontSize: 12, color: 'var(--text-muted)' }}>
          {tab.count}
        </span>
      )}
    </button>
  );
}

/** Everything that is not one of the five documents, behind one menu. */
function OverflowTabs({
  tabs,
  active,
  onPick,
  pending,
}: {
  tabs: { key: Tab; label: string; count?: number }[];
  active: Tab;
  onPick: (t: Tab) => void;
  pending: boolean;
}) {
  const [open, setOpen] = useState(false);
  const anchor = useRef<HTMLDivElement>(null);
  const activeHere = tabs.some(t => t.key === active);

  return (
    <div className="relative shrink-0" ref={anchor}>
      <button
        type="button"
        onClick={() => setOpen(o => !o)}
        aria-haspopup="menu"
        aria-expanded={open}
        className="px-3 py-2 text-sm whitespace-nowrap flex items-center gap-1"
        style={{
          borderBottom: '2px solid',
          borderColor: activeHere ? 'var(--accent)' : 'transparent',
          color: activeHere ? 'var(--accent)' : 'var(--text-secondary)',
          fontWeight: activeHere ? 500 : 400,
        }}
      >
        More
        {/* A pending checkpoint hides in here; the dot is what says so. It
            goes out only once the decision itself is on screen — reading
            Activity, which also lives in this menu, answers nothing. */}
        {pending && active !== 'approval' && (
          <span
            aria-label="a decision is waiting"
            className="rounded-full"
            style={{ width: 6, height: 6, backgroundColor: 'var(--accent)' }}
          />
        )}
        <ChevronDown size={13} aria-hidden="true" />
      </button>
      <Popover open={open} onClose={() => setOpen(false)} label="More views" anchorRef={anchor} width={220}>
        <ul className="space-y-0.5" role="menu">
          {tabs.map(t => (
            <li key={t.key} role="none">
              <button
                type="button"
                role="menuitem"
                onClick={() => { onPick(t.key); setOpen(false); }}
                className="w-full flex items-center justify-between gap-2 text-left px-2 text-sm"
                style={{
                  minHeight: 44,
                  borderRadius: 'var(--radius-sm)',
                  color: active === t.key ? 'var(--accent)' : 'var(--text-primary)',
                  backgroundColor: active === t.key ? 'var(--accent-bg-subtle)' : 'transparent',
                }}
                onMouseEnter={e => { if (active !== t.key) e.currentTarget.style.backgroundColor = 'var(--surface-hover)'; }}
                onMouseLeave={e => { if (active !== t.key) e.currentTarget.style.backgroundColor = 'transparent'; }}
              >
                <span className="truncate">{t.label}</span>
                {t.count !== undefined && (
                  <span className="font-mono shrink-0" style={{ fontSize: 12, color: 'var(--text-muted)' }}>{t.count}</span>
                )}
              </button>
            </li>
          ))}
        </ul>
      </Popover>
    </div>
  );
}

/** The QA surface: the verdict files, then the artifacts they cite. */
function QaEvidence({ verdicts, evidence }: { verdicts: NamedFile[]; evidence: string[] }) {
  return (
    <div className="space-y-4">
      <FilesView files={verdicts} />
      {evidence.length > 0 && (
        <div>
          <div className="text-xs font-medium uppercase tracking-wide mb-1" style={{ color: 'var(--text-muted)' }}>
            Evidence files
          </div>
          <ul className="text-xs space-y-0.5" style={{ color: 'var(--text-secondary)' }}>
            {evidence.map(e => (
              <li key={e} title={e} className="font-mono truncate">{e}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function ReposList({ repos }: { repos: ManifestRepo[] }) {
  return (
    <ul className="text-xs space-y-2">
      {repos.map(r => (
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

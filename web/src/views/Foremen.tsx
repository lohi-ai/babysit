import { useEffect, useMemo, useRef, useState } from 'react';
import { HardHat } from 'lucide-react';
import { foremanLive, type ForemanRow, type Snapshot } from '../lib/data';
import { retireForeman, spawnForeman } from '../lib/api';
import { Button } from '../components/Button';
import { DenseRow } from '../components/DenseRow';
import { EmptyState } from '../components/EmptyState';
import { ErrorBox } from '../components/ErrorBox';
import { Field, inputStyle } from '../components/Field';
import { Modal } from '../components/Modal';
import { Tag, type TagTone } from '../components/Tag';
import { TopBar } from '../components/TopBar';
import { formatRelative } from '../lib/format';
import { gateProps, useControlPlane, useMutation } from '../contexts/ControlContext';
import { useRegisterFocusScope } from '../lib/keyboard';
import { useScopedTickets } from '../lib/scope';

const FRAME_STYLE: React.CSSProperties = {
  border: '1px solid var(--border-hairline)',
  borderRadius: 'var(--radius-md)',
  backgroundColor: 'var(--surface-bg)',
};

const COLUMNS = '20px 150px 110px 1fr 110px 64px 72px 72px';

// A ticket in one of these is finished with, so a foreman holding only these is
// safe to retire. Anything else is work that stops moving the moment its foreman
// is gone, which is what the confirm has to say out loud.
const SETTLED: ReadonlySet<string> = new Set(['done', 'cancelled', 'duplicate']);

type Liveness = 'live' | 'stale' | 'unreachable' | 'retired';

const LIVENESS_TONE: Record<Liveness, TagTone> = {
  live: 'ok',
  stale: 'warn',
  unreachable: 'err',
  retired: 'muted',
};

/**
 * Liveness is derived at render, never read from the record: the heartbeat
 * ages every second, so a stored verdict would keep claiming "live" long after
 * the foreman stopped writing.
 *
 * `unreachable` outranks a fresh heartbeat — a foreman can be beating happily
 * while its workspace is gone, and the assignment that could not be delivered
 * is the fact the human needs.
 */
function liveness(f: ForemanRow): Liveness {
  if (f.status === 'retired') return 'retired';
  if (f.unreachable) return 'unreachable';
  if (foremanLive(f)) return 'live';
  return 'stale';
}

export function Foremen({ snapshot }: { snapshot: Snapshot }) {
  const foremen = snapshot.foremen ?? [];
  const { reason } = useControlPlane();
  const [spawnOpen, setSpawnOpen] = useState(false);
  const [retiring, setRetiring] = useState<ForemanRow | null>(null);

  const rows = useMemo(
    () => foremen.map(f => ({ f, state: liveness(f) })),
    [foremen],
  );

  // Computed here, not asked of the server: the snapshot already carries every
  // ticket with its assignee and status, so an extra field would be a second
  // source for a number already on the page.
  //
  // Scope is 'all', not the active project filter, for the same reason the row
  // below links with project=all — the `assigned` count beside it spans every
  // project, and a stranded-ticket warning narrower than that number would
  // reassure the human about tickets it never looked at.
  const allTickets = useScopedTickets(snapshot, 'all');
  const openTickets = useMemo(() => {
    const byForeman = new Map<string, string[]>();
    for (const t of allTickets) {
      if (!t.assignee || SETTLED.has(t.status)) continue;
      const list = byForeman.get(t.assignee) ?? [];
      list.push(t.id);
      byForeman.set(t.assignee, list);
    }
    return byForeman;
  }, [allTickets]);

  const containerRef = useRef<HTMLDivElement>(null);
  const [rowEls, setRowEls] = useState<HTMLElement[]>([]);
  useEffect(() => {
    const root = containerRef.current;
    setRowEls(root ? Array.from(root.querySelectorAll<HTMLElement>('.dense-row--body[role="row"]')) : []);
  }, [rows]);
  useRegisterFocusScope(rowEls);

  const spawn = (
    <Button
      variant="primary"
      onClick={() => setSpawnOpen(true)}
      {...gateProps(reason)}
    >
      Spawn foreman
    </Button>
  );

  return (
    <>
      <TopBar title="Foremen" count={foremen.length} actions={spawn} />
      <div className="px-6 py-4 w-full" ref={containerRef}>
        {rows.length === 0 ? (
          <div style={FRAME_STYLE}>
            <EmptyState
              icon={<HardHat size={32} strokeWidth={1.5} />}
              title="No foremen"
              body="A foreman watches the tickets you assign to it and dispatches a worker for each one."
              action={spawn}
            />
          </div>
        ) : (
          <div className="overflow-hidden" style={FRAME_STYLE} role="table">
            <DenseRow columns={COLUMNS} header role="row">
              <span />
              <HeaderCell>Foreman</HeaderCell>
              <HeaderCell>Owner</HeaderCell>
              <HeaderCell>Workspace dir</HeaderCell>
              <HeaderCell>Orca</HeaderCell>
              <HeaderCell align="right">Tickets</HeaderCell>
              <HeaderCell align="right">Heartbeat</HeaderCell>
              <span />
            </DenseRow>
            {rows.map(({ f, state }) => (
              <ForemanListRow
                key={f.id}
                f={f}
                state={state}
                gate={reason}
                onRetire={() => setRetiring(f)}
              />
            ))}
          </div>
        )}
      </div>
      <SpawnModal
        open={spawnOpen}
        onClose={() => setSpawnOpen(false)}
        defaultDir={snapshot.meta.current_dir ?? ''}
      />
      <RetireModal
        foreman={retiring}
        openTickets={retiring ? openTickets.get(retiring.id) ?? [] : []}
        onClose={() => setRetiring(null)}
      />
    </>
  );
}

function HeaderCell({ children, align }: { children: React.ReactNode; align?: 'right' }) {
  return (
    <span
      className={`px-3 py-1.5 text-xs uppercase tracking-wide truncate ${align === 'right' ? 'text-right' : ''}`}
      style={{ color: 'var(--text-muted)' }}
    >
      {children}
    </span>
  );
}

function ForemanListRow({
  f,
  state,
  gate,
  onRetire,
}: {
  f: ForemanRow;
  state: Liveness;
  gate: string;
  onRetire: () => void;
}) {
  return (
    <DenseRow
      columns={COLUMNS}
      role="row"
      tabIndex={0}
      // Everything a foreman detail page would show is its tickets, so the row
      // links straight there instead of to a page that would re-list them.
      // `project=all` because the count beside it spans every project — omitting
      // it would scope the list to one and contradict the number just clicked.
      onClick={() => { window.location.hash = `#/tickets?project=all&foreman=${encodeURIComponent(f.id)}`; }}
    >
      <span className="flex items-center justify-center">
        <span
          className={`w-2 h-2 rounded-full ${state === 'live' ? 'motion-safe:animate-pulse' : ''}`}
          style={{ backgroundColor: `var(--${dotVar(state)})` }}
          aria-hidden="true"
        />
      </span>
      <span className="px-3 py-1.5 font-mono text-xs truncate" style={{ color: 'var(--text-primary)' }} title={f.id}>
        {f.id}
      </span>
      <span className="px-3 py-1.5 font-mono text-xs truncate" style={{ color: 'var(--text-secondary)' }} title={f.owner}>
        {f.owner || '—'}
      </span>
      <span
        className="px-3 py-1.5 font-mono text-xs truncate"
        style={{ color: 'var(--text-secondary)' }}
        title={f.workspace_dir || f.project_dir}
      >
        {f.workspace_dir || f.project_dir || '—'}
      </span>
      <span
        className="px-3 py-1.5 font-mono text-xs truncate"
        style={{ color: 'var(--text-secondary)' }}
        title={f.workspace_title ? `${f.workspace_title} (${f.workspace_ref})` : f.workspace_ref}
      >
        {f.workspace_ref || '—'}
      </span>
      <span
        className="px-3 py-1.5 font-mono text-xs text-right"
        style={{ color: 'var(--text-secondary)', fontVariantNumeric: 'tabular-nums' }}
      >
        {f.assigned}
      </span>
      <span className="px-3 py-1.5 text-xs text-right truncate" title={f.heartbeat}>
        {/* The state is in the tag text, not only in the dot's color. */}
        <Tag tone={LIVENESS_TONE[state]}>{state}</Tag>
        <span className="block" style={{ color: 'var(--text-muted)', fontSize: 11 }}>
          {formatRelative(f.heartbeat)}
        </span>
      </span>
      {/* stopPropagation because the row itself navigates: without it, retiring
          would also send the human to the ticket list of what they just removed. */}
      <span className="px-2 py-1.5 flex justify-end" onClick={e => e.stopPropagation()}>
        <Button
          size="sm"
          onClick={onRetire}
          aria-label={`Retire ${f.id}`}
          {...gateProps(gate)}
        >
          Retire
        </Button>
      </span>
    </DenseRow>
  );
}

// Retiring is reversible in the sense that a foreman can be spawned again, but
// it is not an undo: the record, its id and its Orca terminal all go. So the
// confirm names the two things the human cannot see from the row — what happens
// to the workspace, and which tickets stop moving.
function RetireModal({
  foreman,
  openTickets,
  onClose,
}: {
  foreman: ForemanRow | null;
  openTickets: string[];
  onClose: () => void;
}) {
  const [keepWorkspace, setKeepWorkspace] = useState(false);
  const { run, pending, error } = useMutation();

  useEffect(() => {
    if (foreman) setKeepWorkspace(false);
  }, [foreman]);

  if (!foreman) return null;

  const submit = async () => {
    const ok = await run(
      () => retireForeman(foreman.id, keepWorkspace),
      `Retired ${foreman.id}`,
    );
    if (ok) onClose();
  };

  return (
    <Modal
      open
      onClose={onClose}
      title={`Retire ${foreman.id}?`}
      actions={
        <>
          <Button size="lg" onClick={onClose}>Cancel</Button>
          <Button size="lg" variant="primary" onClick={submit} disabled={pending}>
            {pending ? 'Retiring…' : 'Retire'}
          </Button>
        </>
      }
    >
      <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
        Drops the foreman record. {foreman.workspace_title
          ? <>Its Orca terminal <span className="font-mono">{foreman.workspace_title}</span> closes unless you keep it.</>
          : <>It has no Orca terminal to close — it was registered, not spawned.</>}
      </p>

      {openTickets.length > 0 ? (
        <div className="mt-3">
          <p className="text-sm" style={{ color: 'var(--status-blocked-text)' }}>
            {openTickets.length === 1 ? 'One unfinished ticket is' : `${openTickets.length} unfinished tickets are`} assigned
            to it. Retiring leaves {openTickets.length === 1 ? 'it' : 'them'} assigned to a foreman that no longer
            exists, so nothing will dispatch {openTickets.length === 1 ? 'it' : 'them'} until you reassign:
          </p>
          <ul className="mt-1 font-mono text-xs" style={{ color: 'var(--text-secondary)' }}>
            {openTickets.map(id => <li key={id}>{id}</li>)}
          </ul>
        </div>
      ) : (
        <p className="mt-3 text-sm" style={{ color: 'var(--text-muted)' }}>
          No unfinished tickets are assigned to it — nothing is left stranded.
        </p>
      )}

      <label className="mt-4 flex items-center gap-2 text-sm" style={{ color: 'var(--text-secondary)' }}>
        <input
          type="checkbox"
          checked={keepWorkspace}
          onChange={e => setKeepWorkspace(e.target.checked)}
        />
        Keep the Orca terminal open
      </label>

      {error && <ErrorBox title="Retire failed" body={error} />}
    </Modal>
  );
}

function dotVar(state: Liveness): string {
  switch (state) {
    case 'live': return 'status-completed-text';
    case 'stale': return 'status-started-text';
    case 'unreachable': return 'status-blocked-text';
    default: return 'text-muted';
  }
}

// nanoid's alphabet, inlined rather than depended on: this is the only random
// id the SPA mints. 64 chars divides 256 evenly, so the byte fold is unbiased.
const ID_ALPHABET = 'useandom-26T198340PX75pxJACKVERYMINDBUSHWOLF_GQZbfghjklqvwyzrict';

// Every foreman gets its own name, so the default cannot be derived from the
// folder the way the CLI's `fm-<basename>` is: a second foreman in the same
// repo would collide with the first and the spawn would be refused.
function newForemanId(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(8));
  let s = '';
  for (const b of bytes) s += ID_ALPHABET[b % ID_ALPHABET.length];
  return `foremen-${s}`;
}

// The spawn form asks for the workspace folder, not a project: a foreman is
// bound to a folder, and the snapshot carries project *slugs* with no path to
// prefill from. A project select here would be a control that cannot fill in
// the field next to it. The one path the snapshot does carry is the server's
// own launch repo (meta.current_dir), which is the folder the human means in
// the common case — prefilled, and editable for the rest.
function SpawnModal({ open, onClose, defaultDir }: { open: boolean; onClose: () => void; defaultDir: string }) {
  const [dir, setDir] = useState(defaultDir);
  const [id, setId] = useState(newForemanId);
  const { run, pending, error } = useMutation();

  // Re-seeded per opening, not per mount: the modal outlives one spawn, and a
  // second foreman must not inherit the id the first one just took.
  useEffect(() => {
    if (!open) return;
    setDir(defaultDir);
    setId(newForemanId());
  }, [open, defaultDir]);

  const submit = async () => {
    const ok = await run(() => spawnForeman(dir.trim(), id.trim()), `Foreman spawned in ${dir.trim()}`);
    if (ok) onClose();
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Spawn foreman"
      actions={
        <>
          <Button size="lg" onClick={onClose}>Cancel</Button>
          <Button size="lg" variant="primary" onClick={submit} disabled={pending || !dir.trim()}>
            {pending ? 'Spawning…' : 'Spawn'}
          </Button>
        </>
      }
    >
      <Field
        label="Workspace folder"
        hint="The repo checkout the foreman works in. An Orca terminal is created rooted here."
      >
        {fid => (
          <input
            id={fid}
            style={inputStyle}
            className="font-mono"
            value={dir}
            placeholder="/Users/you/workspace/project"
            onChange={e => setDir(e.target.value)}
          />
        )}
      </Field>
      <Field label="Foreman id" hint="Prefilled with a fresh name — rename it if you want one you'll recognize.">
        {fid => (
          <input
            id={fid}
            style={inputStyle}
            className="font-mono"
            value={id}
            placeholder="derived from the folder name when blank"
            onChange={e => setId(e.target.value)}
          />
        )}
      </Field>
      {/* Spawning fails for boring reasons — Orca not running, path missing —
          and the server's message is the fix, so it goes through verbatim. */}
      {error && <ErrorBox title="Spawn failed" body={error} />}
    </Modal>
  );
}

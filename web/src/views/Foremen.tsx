import { useEffect, useMemo, useRef, useState } from 'react';
import { HardHat } from 'lucide-react';
import { foremanLive, type ForemanRow, type Snapshot } from '../lib/data';
import { spawnForeman } from '../lib/api';
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

const FRAME_STYLE: React.CSSProperties = {
  border: '1px solid var(--border-hairline)',
  borderRadius: 'var(--radius-md)',
  backgroundColor: 'var(--surface-bg)',
};

const COLUMNS = '20px 150px 110px 1fr 110px 64px 72px';

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

  const rows = useMemo(
    () => foremen.map(f => ({ f, state: liveness(f) })),
    [foremen],
  );

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
              <HeaderCell>cmux</HeaderCell>
              <HeaderCell align="right">Tickets</HeaderCell>
              <HeaderCell align="right">Heartbeat</HeaderCell>
            </DenseRow>
            {rows.map(({ f, state }) => (
              <ForemanListRow key={f.id} f={f} state={state} />
            ))}
          </div>
        )}
      </div>
      <SpawnModal open={spawnOpen} onClose={() => setSpawnOpen(false)} />
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

function ForemanListRow({ f, state }: { f: ForemanRow; state: Liveness }) {
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
    </DenseRow>
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

// The spawn form asks for the workspace folder, not a project: a foreman is
// bound to a folder, and the snapshot carries project *slugs* with no path to
// prefill from. A project select here would be a control that cannot fill in
// the field next to it.
function SpawnModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [dir, setDir] = useState('');
  const [id, setId] = useState('');
  const { run, pending, error } = useMutation();

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
        hint="The repo checkout the foreman works in. A cmux workspace is created rooted here."
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
      <Field label="Foreman id" hint="Optional — derived from the folder name when blank.">
        {fid => (
          <input
            id={fid}
            style={inputStyle}
            className="font-mono"
            value={id}
            onChange={e => setId(e.target.value)}
          />
        )}
      </Field>
      {/* Spawning fails for boring reasons — cmux not running, path missing —
          and the server's message is the fix, so it goes through verbatim. */}
      {error && <ErrorBox title="Spawn failed" body={error} />}
    </Modal>
  );
}

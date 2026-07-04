import { useMemo, useState } from 'react';
import type { Snapshot, HistoryRow } from '../lib/data';
import { Tag } from '../components/Tag';
import { ErrorBox } from '../components/ErrorBox';
import { Markdown } from '../components/Markdown';
import { TopBar } from '../components/TopBar';
import { formatDate } from '../lib/format';
import { useFilter } from '../contexts/FilterContext';
import { useScopedTicketDetail } from '../lib/scope';

type Tab = 'requirement' | 'plan' | 'manifest' | 'history' | 'handoffs' | 'verdicts' | 'reviews';

export function TicketDetail({ snapshot, ticketId }: { snapshot: Snapshot; ticketId: string }) {
  const { state } = useFilter();
  const detail = useScopedTicketDetail(snapshot, state.project, ticketId);

  const tabs: { key: Tab; label: string; available: boolean }[] = detail ? [
    { key: 'requirement', label: 'Requirement', available: !!detail.requirement },
    { key: 'plan',        label: 'Plan',        available: !!detail.plan },
    { key: 'manifest',    label: 'Manifest',    available: !!detail.manifest },
    { key: 'history',     label: `History (${detail.history.length})`, available: detail.history.length > 0 },
    { key: 'handoffs',    label: `Handoffs (${detail.handoffs.length})`, available: detail.handoffs.length > 0 },
    { key: 'verdicts',    label: `Verdicts (${detail.verdicts.length})`, available: detail.verdicts.length > 0 },
    { key: 'reviews',     label: `Reviews (${detail.reviews.length})`,   available: detail.reviews.length > 0 },
  ] : [];

  const firstAvailable = tabs.find(t => t.available)?.key ?? 'requirement';
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

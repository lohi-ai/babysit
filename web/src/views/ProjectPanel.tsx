import { useEffect, useState } from 'react';
import { Button } from '../components/Button';
import { ErrorBox } from '../components/ErrorBox';
import { SectionHeader } from '../components/SectionHeader';
import { Tag } from '../components/Tag';
import { useControlPlane } from '../contexts/ControlContext';
import { formatDate, formatRelative } from '../lib/format';
import { projectLink, readProject, type ProjectSnapshot } from '../lib/project';

export function ProjectPanel({ project, ticket }: { project: string; ticket: string }) {
  const { mode } = useControlPlane();
  const [data, setData] = useState<ProjectSnapshot | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [revision, refresh] = useState(0);
  useEffect(() => {
    setData(null);
    setError(null);
    if (mode !== 'served') return;
    let controller: AbortController | undefined;
    let stopped = false;
    // Schedule after completion, so a slow repository read is not restarted
    // forever by a short polling interval. Hide stale successes on failure.
    let timer: ReturnType<typeof setTimeout>;
    const load = async () => {
      if (stopped) return;
      if (document.visibilityState === 'visible') {
        controller = new AbortController();
        try {
          const next = await readProject(project, ticket, controller.signal);
          if (!stopped) { setData(next); setError(null); }
        } catch (e) {
          if (!stopped) { setData(null); setError(String(e)); }
        }
      }
      if (!stopped) timer = setTimeout(load, 15000);
    };
    void load();
    return () => { stopped = true; controller?.abort(); clearTimeout(timer); };
  }, [project, ticket, mode, revision]);

  if (mode !== 'served') return <p className="text-sm">Live project readiness is available when the dashboard server is running. This saved snapshot cannot verify current revisions.</p>;
  if (error) return <div className="space-y-3"><ErrorBox title="Project state unavailable" body={error} /><p>Readiness is unknown.</p><Button onClick={() => refresh(r => r + 1)}>Retry</Button></div>;
  if (!data) return <p role="status" className="text-sm">Reading project evidence…</p>;

  const proven = data.coverage.filter(c => c.state === 'proven').length;
  const current = data.evidence.filter(e => e.subject === data.subject);
  const latest = [...current].sort((a, b) => Date.parse(b.finished) - Date.parse(a.finished));
  const preview = latest.find(e => e.state === 'passed' && projectLink(e.preview) && e.runtime);
  const blockers = [...new Set([...data.dispatch_blockers, ...data.blockers])];
  return <div className="project-panel space-y-5">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div className="flex flex-wrap items-center gap-4">
        <Tag tone={data.ready ? 'ok' : 'warn'}>{data.completion === 'verified' ? 'Completed · verified' : data.ready ? 'Ready to finish' : 'Verification pending'}</Tag>
        <span>{proven} / {data.coverage.length} criteria proven</span>
        <Tag tone={data.approval === 'approved' ? 'ok' : 'warn'}>Design {data.approval || 'pending'}</Tag>
      </div>
      <Button onClick={() => refresh(r => r + 1)}>Refresh</Button>
    </div>
    <p className="text-xs" style={{ color: 'var(--text-secondary)' }}>Observed {formatDate(data.observed_at)} · Usage unavailable</p>

    {data.coordinator?.id && <p className="text-xs" style={{ color: 'var(--text-secondary)' }}>Coordinator {data.coordinator.id} · Terminal observed {data.coordinator.runtime_observed_at ? formatRelative(data.coordinator.runtime_observed_at) : 'unknown'} · Agent liveness unknown</p>}

    {data.contract ? <section aria-label="Project outcome">
      <SectionHeader title="Outcome" />
      <p className="text-base mt-2">{data.contract.outcome}</p>
      <p className="mt-1" style={{ color: 'var(--text-secondary)' }}>For {data.contract.audience}</p>
      <p className="mt-2 text-xs">First usable journey: {data.contract.first_journey.join(', ')}{data.contract.deadline && ` · Budget ends ${formatDate(data.contract.deadline)}`}</p>
      {!!data.contract.non_goals?.length && <details className="mt-2 text-sm"><summary>Outside this project</summary><ul className="list-disc pl-5 mt-2">{data.contract.non_goals.map((goal, i) => <li key={i}>{goal}</li>)}</ul></details>}
    </section> : <p>No acceptance contract yet. The existing project artifacts are available in the other tabs.</p>}

    {preview && projectLink(preview.preview) && <section className="space-y-1">
      <a className="inline-flex items-center min-h-11" href={projectLink(preview.preview)} target="_blank" rel="noopener noreferrer">Open verified preview ↗</a>
      <p className="text-xs">Tested revision <code>{preview.runtime?.slice(0, 12)}</code> · {formatDate(preview.finished)}. Availability has not been re-probed.</p>
    </section>}

    <section aria-label="Acceptance coverage">
      <SectionHeader title="Acceptance coverage" count={data.coverage.length} />
      {data.coverage.length === 0 ? <p className="mt-2">No acceptance criteria recorded.</p> : <div className="overflow-x-auto mt-2">
        <table className="project-table"><thead><tr><th scope="col">Journey</th><th scope="col">Owner</th><th scope="col">Required checks</th><th scope="col">Evidence</th></tr></thead>
          <tbody>{data.coverage.map(c => <tr key={c.id}><td><span className="text-xs">{c.id}</span><p>{c.text}</p></td><td>{c.owner}</td><td>{c.checks.join(', ')}</td><td><Tag tone={c.state === 'proven' ? 'ok' : 'warn'}>{c.state}</Tag>{c.evidence.length > 0 && <details><summary className="text-xs mt-1">Receipts</summary><ul className="text-xs space-y-1 mt-1">{c.evidence.map(e => <li key={e}><code>{e}</code></li>)}</ul></details>}</td></tr>)}</tbody>
        </table>
      </div>}
    </section>

    <section aria-label="Remaining work"><SectionHeader title={blockers.length ? 'Next actions' : 'Completion checks passed'} count={blockers.length} />
      {blockers.length > 0 ? <ul className="mt-2 space-y-1 list-disc pl-5">{blockers.map(b => <li key={b}>{b}</li>)}</ul> : <p className="mt-2">All required criteria, revisions, delivery, and cleanup checks are current.</p>}
    </section>

    <section aria-label="Project workers"><SectionHeader title="Workers" count={data.children.length} />
      {data.children.length === 0 ? <p className="mt-2">No child tickets allocated.</p> : <div className="overflow-x-auto mt-2"><table className="project-table">
        <thead><tr><th scope="col">Work</th><th scope="col">Progress</th><th scope="col">Dependencies / next action</th></tr></thead>
        <tbody>{data.children.map(c => <tr key={c.id}><td><p>{c.title || c.seed || c.id}</p><code className="text-xs">{c.id}</code>{c.repos.map(r => <p key={r.branch} className="text-xs mt-1">{r.branch}@{r.head.slice(0, 12)}</p>)}</td><td>{c.progress ? <><Tag tone={c.progress.health === 'progressing' ? 'ok' : 'warn'}>{c.progress.health.replaceAll('_', ' ')}</Tag><p className="mt-1">{c.progress.summary}</p><p className="text-xs mt-1">Reported {formatRelative(c.progress.updated_at)}</p>{c.progress.wait_kind && <p className="text-xs">{c.progress.wait_reason} · until {formatDate(c.progress.wait_until)}</p>}</> : <><Tag tone="muted">Progress unknown</Tag><p className="text-xs mt-1">No structured milestone reported.</p></>}</td><td>{c.blocked_by?.length > 0 && <p>Depends on {c.blocked_by.join(', ')}</p>}{c.problems.map(p => <p key={p}>{p}</p>)}{!c.problems.length && <Tag tone="ok">Child evidence sealed</Tag>}</td></tr>)}</tbody>
      </table></div>}
    </section>

    <section aria-label="Integration and product review"><SectionHeader title="Integration and product review" />
      {latest.length === 0 ? <p className="mt-2">No current integrated verification. {data.evidence.length > 0 && `${data.evidence.length} older receipt(s) do not match current inputs.`}</p> : latest.map(e => <details key={e.id} className="py-2 border-b" style={{ borderColor: 'var(--border-hairline)' }}><summary>{e.kind} · {e.state} · {formatDate(e.finished)} · {e.producer}</summary><div className="mt-2 space-y-2"><code className="text-xs">{e.id}</code>{e.surfaces.map(s => <p key={s.dir} className="text-xs">{s.ref}@{s.head.slice(0, 12)}</p>)}{e.findings?.map((f, i) => <p key={i}>{f.severity}: {f.summary}</p>)}<ul className="space-y-1">{e.checks.map((c, i) => <li key={i}><Tag tone={c.exit_code === 0 ? 'ok' : 'err'}>{c.exit_code === 0 ? 'Pass' : 'Fail'}</Tag> {c.criterion} / {c.kind}<p className="text-xs break-all">{c.log}</p></li>)}</ul></div></details>)}
    </section>

    <section aria-label="Project delivery"><SectionHeader title="Delivery" />
      {!data.delivery?.length ? <p className="mt-2">No verified delivery yet.</p> : data.delivery.map((d, i) => <div className="py-2" key={`${d.ticket}-${i}`}><span>{d.ticket} · </span><Tag tone={d.state === 'UNKNOWN' ? 'warn' : 'ok'}>{d.state.replaceAll('_', ' ')}</Tag><p className="text-xs mt-1">{d.head.slice(0, 12)} · {d.policy}{d.reason && ` · ${d.reason}`}</p>{d.state === 'LANDED_LOCAL' && <p className="text-xs">Remote delivery has not been verified.</p>}{projectLink(d.url) && <a href={projectLink(d.url)} target="_blank" rel="noopener noreferrer" className="inline-flex items-center min-h-11">Open pull request ↗</a>}</div>)}
    </section>
  </div>;
}

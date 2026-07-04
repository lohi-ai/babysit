import { useMemo } from 'react';
import type { Snapshot } from '../lib/data';
import { DenseRow } from '../components/DenseRow';
import { FiltersPopover, type FacetDef } from '../components/FiltersPopover';
import { EmptyState } from '../components/EmptyState';
import { Tag } from '../components/Tag';
import { TopBar } from '../components/TopBar';
import { formatRelative } from '../lib/format';
import { useFilter } from '../contexts/FilterContext';

function unique(vals: (string | undefined)[]): string[] {
  return [...new Set(vals.filter((v): v is string => !!v))].sort();
}

const HEAD_CLASS = 'px-3 py-1 text-xs font-medium uppercase';
const HEAD_STYLE: React.CSSProperties = { color: 'var(--text-muted)', letterSpacing: 'var(--tracking-caption)' };

const CLASSIFICATION_TONE: Record<string, 'ok' | 'warn' | 'err' | 'muted' | 'info' | 'accent'> = {
  mechanical: 'muted',
  taste: 'accent',
  'user-challenge': 'warn',
  user_challenge: 'warn',
  resize: 'info',
};

// Typed rows (e.g. kind:"resize" from the scope-contraction hook) have no
// `decision` field — synthesize a readable line from their own keys.
function decisionText(d: Record<string, unknown>): string {
  if (typeof d.decision === 'string' && d.decision) return d.decision;
  if (d.kind === 'resize') return `ticket_size ${d.from} → ${d.to} (${d.trigger})`;
  return '';
}

export function DecisionsFeed({ snapshot }: { snapshot: Snapshot }) {
  const { state } = useFilter();
  const decisions = snapshot.decisions ?? [];

  const skillOptions = useMemo(() => unique(decisions.map(d => d.skill)), [decisions]);
  const phaseOptions = useMemo(() => unique(decisions.map(d => d.phase)), [decisions]);
  const classificationOptions = useMemo(() => unique(decisions.map(d => d.classification)), [decisions]);

  const facets: FacetDef[] = [
    { kind: 'label', label: 'Skill', options: skillOptions },
    { kind: 'phase', label: 'Phase', options: phaseOptions },
    { kind: 'status', label: 'Classification', options: classificationOptions },
  ];

  const filtered = useMemo(() => {
    return decisions.filter(d => {
      if (state.label.length > 0 && !state.label.includes(d.skill)) return false;
      if (state.phase.length > 0 && !state.phase.includes(d.phase)) return false;
      if (state.status.length > 0 && !state.status.includes(d.classification)) return false;
      return true;
    });
  }, [decisions, state.label, state.phase, state.status]);

  const truncation = snapshot.meta.truncations.find(t => t.kind === 'decisions');

  return (
    <>
      <TopBar title="Decisions feed" count={filtered.length} />
      <div className="px-6 py-4 w-full space-y-4">
      {truncation && (
        <div
          className="px-3 py-1.5 text-xs"
          style={{
            backgroundColor: 'var(--status-started-bg)',
            color: 'var(--status-started-text)',
            border: '1px solid var(--border-hairline)',
            borderRadius: 'var(--radius-sm)',
          }}
        >
          Showing {truncation.kept} of {truncation.total} decisions — oldest entries truncated.
        </div>
      )}

      <FiltersPopover facets={facets} />

      {decisions.length === 0 ? (
        <EmptyState title="No decisions" body="No auto-decisions have been logged yet." />
      ) : filtered.length === 0 ? (
        <EmptyState title="No results" body="No decisions match the active filters." />
      ) : (
        <div
          className="overflow-hidden"
          style={{
            border: '1px solid var(--border-hairline)',
            borderRadius: 'var(--radius-md)',
            backgroundColor: 'var(--surface-bg)',
          }}
        >
          <DenseRow columns="88px 100px 90px 110px 1fr" header>
            <span className={HEAD_CLASS} style={HEAD_STYLE}>When</span>
            <span className={HEAD_CLASS} style={HEAD_STYLE}>Skill</span>
            <span className={HEAD_CLASS} style={HEAD_STYLE}>Phase</span>
            <span className={HEAD_CLASS} style={HEAD_STYLE}>Class</span>
            <span className={HEAD_CLASS} style={HEAD_STYLE}>Decision</span>
          </DenseRow>
          {filtered.map((d, i) => (
            <DenseRow key={i} columns="88px 100px 90px 110px 1fr">
              <span className="px-3 py-1.5 text-xs truncate min-w-0" style={{ color: 'var(--text-muted)' }} title={d.ts}>
                {formatRelative(d.ts)}
              </span>
              <span className="px-3 py-1.5 min-w-0">
                <span className="font-mono text-xs truncate block" style={{ color: 'var(--text-secondary)' }} title={d.skill}>{d.skill}</span>
              </span>
              <span className="px-3 py-1.5 min-w-0">
                <span className="text-xs truncate block" style={{ color: 'var(--text-secondary)' }}>{d.phase}</span>
              </span>
              <span className="px-3 py-1.5 min-w-0 flex items-center">
                <Tag tone={CLASSIFICATION_TONE[d.classification?.toLowerCase() ?? ''] ?? 'info'}>
                  {d.classification ?? (d.kind as string | undefined) ?? '—'}
                </Tag>
              </span>
              <span className="px-3 py-1.5 text-sm truncate min-w-0" style={{ color: 'var(--text-primary)' }} title={decisionText(d)}>
                {decisionText(d)}
              </span>
            </DenseRow>
          ))}
        </div>
      )}
      </div>
    </>
  );
}

import { useMemo } from 'react';
import type { Snapshot } from '../lib/data';
import { DenseRow } from '../components/DenseRow';
import { FiltersPopover, type FacetDef } from '../components/FiltersPopover';
import { EmptyState } from '../components/EmptyState';
import { Tag } from '../components/Tag';
import { TopBar } from '../components/TopBar';
import { formatRelative, formatDuration } from '../lib/format';
import { useFilter } from '../contexts/FilterContext';

function unique(vals: (string | undefined)[]): string[] {
  return [...new Set(vals.filter((v): v is string => !!v))].sort();
}

const HEAD_CLASS = 'px-3 py-1 text-xs font-medium uppercase';
const HEAD_STYLE: React.CSSProperties = { color: 'var(--text-muted)', letterSpacing: 'var(--tracking-caption)' };

const OUTCOME_TONE: Record<string, 'ok' | 'warn' | 'err' | 'muted' | 'info'> = {
  success: 'ok',
  ok: 'ok',
  done: 'ok',
  partial: 'warn',
  blocked: 'err',
  error: 'err',
  failed: 'err',
};

export function SkillEvents({ snapshot }: { snapshot: Snapshot }) {
  const { state } = useFilter();
  const skillEvents = snapshot.skillEvents ?? [];

  const skillOptions = useMemo(() => unique(skillEvents.map(e => e.skill)), [skillEvents]);
  const outcomeOptions = useMemo(() => unique(skillEvents.map(e => e.outcome)), [skillEvents]);

  const facets: FacetDef[] = [
    { kind: 'label', label: 'Skill', options: skillOptions },
    { kind: 'status', label: 'Outcome', options: outcomeOptions },
  ];

  const filtered = useMemo(() => {
    return skillEvents.filter(e => {
      if (state.label.length > 0 && !state.label.includes(e.skill)) return false;
      if (state.status.length > 0 && e.outcome && !state.status.includes(e.outcome)) return false;
      return true;
    });
  }, [skillEvents, state.label, state.status]);

  const truncation = snapshot.meta.truncations.find(t => t.kind === 'skillEvents');

  return (
    <>
      <TopBar title="Skill events" count={filtered.length} />
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
          Showing {truncation.kept} of {truncation.total} events — oldest entries truncated.
        </div>
      )}

      <FiltersPopover facets={facets} />

      {skillEvents.length === 0 ? (
        <EmptyState title="No skill events" body="No skill usage events have been logged yet." />
      ) : filtered.length === 0 ? (
        <EmptyState title="No results" body="No events match the active filters." />
      ) : (
        <div
          className="overflow-hidden"
          style={{
            border: '1px solid var(--border-hairline)',
            borderRadius: 'var(--radius-md)',
            backgroundColor: 'var(--surface-bg)',
          }}
        >
          <DenseRow columns="88px 120px 70px 80px 90px 1fr" header>
            <span className={HEAD_CLASS} style={HEAD_STYLE}>When</span>
            <span className={HEAD_CLASS} style={HEAD_STYLE}>Skill</span>
            <span className={HEAD_CLASS} style={HEAD_STYLE}>Event</span>
            <span className={HEAD_CLASS} style={HEAD_STYLE}>Duration</span>
            <span className={HEAD_CLASS} style={HEAD_STYLE}>Outcome</span>
            <span className={HEAD_CLASS} style={HEAD_STYLE}>Session</span>
          </DenseRow>
          {filtered.map((e, i) => (
            <DenseRow key={i} columns="88px 120px 70px 80px 90px 1fr">
              <span className="px-3 py-1.5 text-xs truncate min-w-0" style={{ color: 'var(--text-muted)' }} title={e.ts}>
                {formatRelative(e.ts)}
              </span>
              <span className="px-3 py-1.5 min-w-0">
                <span className="font-mono text-xs truncate block" style={{ color: 'var(--text-secondary)' }} title={e.skill}>{e.skill}</span>
              </span>
              <span className="px-3 py-1.5 text-xs truncate min-w-0" style={{ color: 'var(--text-secondary)' }}>{e.event}</span>
              <span className="px-3 py-1.5 text-xs truncate min-w-0" style={{ color: 'var(--text-muted)' }}>
                {formatDuration(e.duration_s)}
              </span>
              <span className="px-3 py-1.5 min-w-0 flex items-center">
                {e.outcome
                  ? <Tag tone={OUTCOME_TONE[e.outcome.toLowerCase()] ?? 'info'}>{e.outcome}</Tag>
                  : <span className="text-xs" style={{ color: 'var(--text-muted)' }}>—</span>}
              </span>
              <span className="px-3 py-1.5 font-mono text-xs truncate min-w-0" style={{ color: 'var(--text-muted)' }} title={e.session}>
                {e.session ?? '—'}
              </span>
            </DenseRow>
          ))}
        </div>
      )}
      </div>
    </>
  );
}

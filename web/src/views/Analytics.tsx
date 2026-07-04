import { useMemo } from 'react';
import type { Snapshot } from '../lib/data';
import { DenseRow } from '../components/DenseRow';
import { BarChart } from '../components/BarChart';
import { EmptyState } from '../components/EmptyState';
import { SectionHeader } from '../components/SectionHeader';
import { TopBar } from '../components/TopBar';
import { formatDuration } from '../lib/format';
import { useFilter } from '../contexts/FilterContext';
import { useScopedAnalytics } from '../lib/scope';

const FRAME_STYLE: React.CSSProperties = {
  border: '1px solid var(--border-hairline)',
  borderRadius: 'var(--radius-md)',
  backgroundColor: 'var(--surface-bg)',
};

export function Analytics({ snapshot }: { snapshot: Snapshot }) {
  const { state } = useFilter();
  const analytics = useScopedAnalytics(snapshot, state.project);
  const isAll = state.project === 'all';
  const hasData = analytics.rows.length > 0;

  const perSkillBars = useMemo(() =>
    analytics.per_skill
      .slice()
      .sort((a, b) => b.runs - a.runs)
      .slice(0, 12)
      .map(s => ({ label: s.skill, value: s.runs })),
    [analytics.per_skill]
  );

  const perDayBars = useMemo(() =>
    analytics.per_day
      .slice()
      .sort((a, b) => a.day.localeCompare(b.day))
      .slice(-30)
      .map(d => ({ label: d.day, value: d.runs })),
    [analytics.per_day]
  );

  const title = isAll ? 'Analytics — all projects' : 'Analytics';

  if (!hasData) {
    return (
      <>
        <TopBar title={title} />
        <div className="px-6 py-4 w-full space-y-4">
          <EmptyState
            title="No analytics yet"
            body="No skill-usage events have been logged. Run a few skills, then re-snapshot."
          />
        </div>
      </>
    );
  }

  return (
    <>
      <TopBar title={title} />
      <div className="px-6 py-4 w-full space-y-6">
        {analytics.outcome.length > 0 && (
          <section>
            <SectionHeader title="Outcomes" />
            <div
              className="flex flex-wrap gap-x-6 gap-y-2 mt-1 text-sm"
              style={{ fontVariantNumeric: 'tabular-nums' }}
            >
              {analytics.outcome.map(o => (
                <div key={o.outcome} className="flex items-baseline gap-2">
                  <span
                    className="text-xs font-medium uppercase"
                    style={{ color: 'var(--text-muted)', letterSpacing: 'var(--tracking-caption)' }}
                  >
                    {o.outcome}
                  </span>
                  <span className="font-mono" style={{ color: 'var(--text-primary)' }}>
                    {o.count}
                  </span>
                </div>
              ))}
            </div>
          </section>
        )}

        <section>
          <SectionHeader title="Runs per skill" />
          <div className="p-4 mt-1" style={FRAME_STYLE}>
            <BarChart bars={perSkillBars} ariaLabel="Runs per skill" />
          </div>
        </section>

        <section>
          <SectionHeader title="Runs per day (last 30)" />
          <div className="p-4 mt-1" style={FRAME_STYLE}>
            <BarChart bars={perDayBars} ariaLabel="Runs per day" />
          </div>
        </section>

        <section>
          <SectionHeader title="Per-skill detail" count={analytics.per_skill.length} />
          <div className="overflow-x-auto mt-1" style={FRAME_STYLE}>
            <DenseRow columns="1fr 70px 90px 70px 70px" header>
              {(['Skill', 'Runs', 'Total time', 'Success', 'Error'] as const).map(h => (
                <span key={h} className="px-3 py-1 text-xs font-medium uppercase" style={{ color: 'var(--text-muted)', letterSpacing: 'var(--tracking-caption)' }}>{h}</span>
              ))}
            </DenseRow>
            {analytics.per_skill.map(s => (
              <DenseRow key={s.skill} columns="1fr 70px 90px 70px 70px">
                <span className="px-3 py-1.5 font-mono text-sm truncate min-w-0" style={{ color: 'var(--text-primary)' }} title={s.skill}>{s.skill}</span>
                <span className="px-3 py-1.5 font-mono text-sm text-right" style={{ color: 'var(--text-primary)', fontVariantNumeric: 'tabular-nums' }}>{s.runs}</span>
                <span className="px-3 py-1.5 font-mono text-sm text-right" style={{ color: 'var(--text-muted)', fontVariantNumeric: 'tabular-nums' }}>{formatDuration(s.total_s)}</span>
                <span className="px-3 py-1.5 font-mono text-sm text-right" style={{ color: 'var(--status-completed-text)', fontVariantNumeric: 'tabular-nums' }}>{s.success}</span>
                <span className="px-3 py-1.5 font-mono text-sm text-right" style={{ color: 'var(--status-blocked-text)', fontVariantNumeric: 'tabular-nums' }}>{s.error || ''}</span>
              </DenseRow>
            ))}
          </div>
        </section>
      </div>
    </>
  );
}

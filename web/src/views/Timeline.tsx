import { useMemo } from 'react';
import type { Snapshot } from '../lib/data';
import { DenseRow } from '../components/DenseRow';
import { FiltersPopover, type FacetDef } from '../components/FiltersPopover';
import { EmptyState } from '../components/EmptyState';
import { SectionHeader } from '../components/SectionHeader';
import { TopBar } from '../components/TopBar';
import { formatDate } from '../lib/format';
import { useFilter } from '../contexts/FilterContext';
import { useScopedTimeline } from '../lib/scope';

export function Timeline({ snapshot }: { snapshot: Snapshot }) {
  const { state } = useFilter();
  const timeline = useScopedTimeline(snapshot, state.project);

  const phases = useMemo(() => {
    const set = new Set<string>();
    for (const e of timeline) {
      if (e.phase && typeof e.phase === 'string') set.add(e.phase);
    }
    return Array.from(set).sort();
  }, [timeline]);

  const facets: FacetDef[] = [
    { kind: 'phase', label: 'Phase', options: phases },
  ];

  const filtered = useMemo(() => {
    if (state.phase.length === 0) return timeline;
    return timeline.filter(e => state.phase.includes((e.phase as string | undefined) ?? ''));
  }, [timeline, state.phase]);

  const grouped = useMemo(() => {
    const days = new Map<string, typeof filtered>();
    for (const e of filtered) {
      const day = e.ts ? e.ts.slice(0, 10) : 'unknown';
      if (!days.has(day)) days.set(day, []);
      days.get(day)!.push(e);
    }
    return Array.from(days.entries()).sort((a, b) => b[0].localeCompare(a[0]));
  }, [filtered]);

  const hasFilters = state.phase.length > 0;

  return (
    <>
      <TopBar title="Timeline" count={filtered.length} />
      <div className="px-6 py-4 w-full space-y-4">
      <FiltersPopover facets={facets} />

      {timeline.length === 0 ? (
        <EmptyState title="No events" body="No timeline events in this project." />
      ) : filtered.length === 0 ? (
        <EmptyState title="No results" body="No events match the active filters." />
      ) : (
        <div className="space-y-3">
          {grouped.map(([day, events]) => (
            <SectionHeader key={day} title={day} count={events.length}>
              <div
                className="mt-1"
                style={{
                  border: '1px solid var(--border-hairline)',
                  borderRadius: 'var(--radius-md)',
                  backgroundColor: 'var(--surface-bg)',
                  overflow: 'hidden',
                }}
              >
                <DenseRow columns="120px 120px 1fr" header>
                    <span className="px-3 py-1 text-xs font-medium uppercase" style={{ color: 'var(--text-muted)', letterSpacing: 'var(--tracking-caption)' }}>Time</span>
                    <span className="px-3 py-1 text-xs font-medium uppercase" style={{ color: 'var(--text-muted)', letterSpacing: 'var(--tracking-caption)' }}>Ticket</span>
                    <span className="px-3 py-1 text-xs font-medium uppercase" style={{ color: 'var(--text-muted)', letterSpacing: 'var(--tracking-caption)' }}>Event</span>
                  </DenseRow>
                  {events.map((e, i) => (
                    <DenseRow
                      key={i}
                      columns="120px 120px 1fr"
                      tabIndex={0}
                      role="row"
                      onClick={() => { window.location.hash = `#/tickets/${e.ticket}`; }}
                    >
                      <span className="px-3 text-xs truncate min-w-0 whitespace-nowrap" style={{ color: 'var(--text-muted)' }} title={e.ts}>
                        {formatDate(e.ts)}
                      </span>
                      <span className="px-3 min-w-0">
                        <a
                          href={`#/tickets/${e.ticket}`}
                          className="font-mono text-xs hover:underline truncate block"
                          style={{ color: 'var(--accent)' }}
                          title={e.ticket}
                          onClick={ev => ev.stopPropagation()}
                        >
                          {e.ticket}
                        </a>
                      </span>
                      <span className="px-3 text-sm truncate min-w-0" style={{ color: 'var(--text-primary)' }}>
                        {e.workflow ?? e.event ?? ''}
                        {e.step ? ` / ${e.step}` : ''}
                        {e.status ? <span style={{ color: 'var(--text-muted)' }}> — {e.status}</span> : null}
                        {e.note ? <span style={{ color: 'var(--text-muted)' }}> · {e.note}</span> : null}
                      </span>
                    </DenseRow>
                  ))}
              </div>
            </SectionHeader>
          ))}
        </div>
      )}

      {hasFilters && filtered.length > 0 && (
        <div className="text-xs text-right" style={{ color: 'var(--text-muted)' }}>{filtered.length} of {timeline.length} event{timeline.length === 1 ? '' : 's'}</div>
      )}
      </div>
    </>
  );
}

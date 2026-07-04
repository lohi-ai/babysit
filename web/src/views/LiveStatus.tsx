import { useMemo } from 'react';
import type { Snapshot } from '../lib/data';
import { DenseRow } from '../components/DenseRow';
import { EmptyState } from '../components/EmptyState';
import { SectionHeader } from '../components/SectionHeader';
import { TopBar } from '../components/TopBar';
import { formatRelative } from '../lib/format';

const FRAME_STYLE: React.CSSProperties = {
  border: '1px solid var(--border-hairline)',
  borderRadius: 'var(--radius-md)',
  backgroundColor: 'var(--surface-bg)',
};

const TS_PREFIX = /^(\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}(?::\d{2})?(?:Z|[+-]\d{2}:?\d{2})?)\s*(.*)$/s;

interface JournalLine {
  ts: string | null;
  body: string;
  raw: string;
}

function parseLine(raw: string): JournalLine {
  const m = raw.match(TS_PREFIX);
  if (m) return { ts: m[1], body: m[2], raw };
  return { ts: null, body: raw, raw };
}

function bucketKey(ts: string | null): string {
  if (!ts) return 'no-timestamp';
  const d = new Date(ts.replace(' ', 'T'));
  if (Number.isNaN(d.getTime())) return 'no-timestamp';
  const minutes = d.getMinutes();
  d.setMinutes(minutes - (minutes % 5), 0, 0);
  return d.toISOString();
}

function bucketLabel(key: string): string {
  if (key === 'no-timestamp') return 'Other';
  const d = new Date(key);
  if (Number.isNaN(d.getTime())) return key;
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  const yyyy = d.getFullYear();
  const mo = String(d.getMonth() + 1).padStart(2, '0');
  const da = String(d.getDate()).padStart(2, '0');
  return `${yyyy}-${mo}-${da} ${hh}:${mm}`;
}

export function LiveStatus({ snapshot }: { snapshot: Snapshot }) {
  const { sessions, journalTail, builderProfile, meta } = snapshot;
  const sessionCount = sessions?.count ?? 0;
  const slugs = sessions?.slugs ?? [];
  const sessionRows = sessions?.sessions ?? [];
  const profile = builderProfile?.[0] ?? null;

  const buckets = useMemo(() => {
    const lines = (journalTail ?? []).map(parseLine);
    const map = new Map<string, JournalLine[]>();
    for (const line of lines) {
      const k = bucketKey(line.ts);
      if (!map.has(k)) map.set(k, []);
      map.get(k)!.push(line);
    }
    return Array.from(map.entries()).sort((a, b) => {
      if (a[0] === 'no-timestamp') return 1;
      if (b[0] === 'no-timestamp') return -1;
      return b[0].localeCompare(a[0]);
    });
  }, [journalTail]);

  const hasJournal = (journalTail ?? []).length > 0;

  return (
    <>
      <TopBar title="Live status" />
      <div className="px-6 py-4 w-full space-y-6">
        <section>
          <SectionHeader title="Is babysit live right now?" />
          <div className="p-4 mt-1 flex items-center gap-4" style={FRAME_STYLE}>
            <span
              className={`w-3 h-3 rounded-full shrink-0 ${sessionCount > 0 ? 'animate-pulse' : ''}`}
              style={{
                backgroundColor: sessionCount > 0
                  ? 'var(--status-completed-text)'
                  : 'var(--text-muted)',
              }}
              aria-hidden="true"
            />
            <div>
              <div className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
                {sessionCount > 0
                  ? `${sessionCount} active session${sessionCount === 1 ? '' : 's'}`
                  : 'No active sessions'}
              </div>
              {sessionCount > 0 && sessionRows.length === 0 && slugs.length > 0 && (
                <div className="text-xs mt-0.5 font-mono" style={{ color: 'var(--text-muted)' }}>
                  {slugs.join(', ')}
                </div>
              )}
              <div className="text-xs mt-0.5" style={{ color: 'var(--text-muted)' }}>
                snapshot {formatRelative(meta.generated_at || meta.snapshot_at)}
              </div>
            </div>
          </div>
          {sessionRows.length > 0 && (
            <div className="mt-2 overflow-hidden" style={FRAME_STYLE}>
              <DenseRow columns="160px 140px 1fr 60px" header>
                <span className="px-3 py-1.5 text-xs uppercase tracking-wide" style={{ color: 'var(--text-muted)' }}>Ticket</span>
                <span className="px-3 py-1.5 text-xs uppercase tracking-wide" style={{ color: 'var(--text-muted)' }}>Product</span>
                <span className="px-3 py-1.5 text-xs uppercase tracking-wide" style={{ color: 'var(--text-muted)' }}>Cwd</span>
                <span className="px-3 py-1.5 text-xs uppercase tracking-wide text-right" style={{ color: 'var(--text-muted)' }}>Age</span>
              </DenseRow>
              {sessionRows.map(s => (
                <DenseRow key={s.id} columns="160px 140px 1fr 60px">
                  <span
                    className="px-3 py-1.5 font-mono text-xs truncate"
                    style={{ color: 'var(--text-primary)' }}
                    title={s.ticket ?? s.id}
                  >
                    {s.ticket ?? '—'}
                  </span>
                  <span
                    className="px-3 py-1.5 font-mono text-xs truncate"
                    style={{ color: 'var(--text-secondary)' }}
                    title={s.product ?? ''}
                  >
                    {s.product ?? '—'}
                  </span>
                  <span
                    className="px-3 py-1.5 font-mono text-xs truncate"
                    style={{ color: 'var(--text-secondary)' }}
                    title={s.cwd ?? ''}
                  >
                    {s.cwd ?? '—'}
                  </span>
                  <span
                    className="px-3 py-1.5 font-mono text-xs text-right"
                    style={{ color: 'var(--text-muted)' }}
                  >
                    {s.age_min}m
                  </span>
                </DenseRow>
              ))}
            </div>
          )}
        </section>

        <section>
          <SectionHeader title="Builder profile" />
          {!profile ? (
            <EmptyState title="No builder profile" body="No profile data available." />
          ) : (
            <div className="p-4 mt-1 space-y-2" style={FRAME_STYLE}>
              <Row label="Date" value={profile.date} />
              {profile.assignment && <Row label="Assignment" value={profile.assignment} />}
              {profile.mood && <Row label="Mood" value={profile.mood} />}
              {profile.topics && profile.topics.length > 0 && (
                <div className="flex items-start gap-3">
                  <span className="text-xs w-20 shrink-0 pt-0.5" style={{ color: 'var(--text-muted)' }}>Topics</span>
                  <div className="flex flex-wrap gap-1">
                    {profile.topics.map(topic => (
                      <span
                        key={topic}
                        className="px-2 py-0.5 text-xs"
                        style={{
                          backgroundColor: 'var(--surface-elevated)',
                          color: 'var(--text-secondary)',
                          borderRadius: 'var(--radius-sm)',
                        }}
                      >
                        {topic}
                      </span>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}
        </section>

        {!hasJournal ? (
          <EmptyState title="No activity" body="No journal entries in this session yet." />
        ) : (
          <div className="space-y-3">
            {buckets.map(([key, lines]) => (
              <SectionHeader key={key} title={bucketLabel(key)} count={lines.length}>
                <div className="overflow-hidden mt-1" style={FRAME_STYLE}>
                  {lines.map((line, i) => (
                    <DenseRow key={i} columns="80px 1fr">
                      <span
                        className="px-3 py-1.5 font-mono text-xs truncate min-w-0"
                        style={{ color: 'var(--text-muted)' }}
                        title={line.ts ?? ''}
                      >
                        {line.ts ? line.ts.slice(11, 16) : '—'}
                      </span>
                      <span
                        className="px-3 py-1.5 font-mono text-xs whitespace-pre-wrap break-words min-w-0"
                        style={{ color: 'var(--text-secondary)' }}
                      >
                        {line.body}
                      </span>
                    </DenseRow>
                  ))}
                </div>
              </SectionHeader>
            ))}
          </div>
        )}
      </div>
    </>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center gap-3">
      <span className="text-xs w-20 shrink-0" style={{ color: 'var(--text-muted)' }}>{label}</span>
      <span className="text-sm truncate" style={{ color: 'var(--text-primary)' }}>{value}</span>
    </div>
  );
}

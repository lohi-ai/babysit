import type { TicketStatus } from '../lib/data';
import { STATUS_BUCKET } from '../lib/priority';

export type TagTone = 'ok' | 'warn' | 'err' | 'muted' | 'info' | 'accent';

const TONE_VAR: Record<TagTone, string> = {
  ok:     'var(--status-completed-text)',
  warn:   'var(--status-started-text)',
  err:    'var(--status-blocked-text)',
  muted:  'var(--text-muted)',
  info:   'var(--text-secondary)',
  accent: 'var(--accent)',
};

interface TagProps {
  children?: React.ReactNode;
  tone?: TagTone;
  /** Convenience: if `status` is set, tone is derived from STATUS_BUCKET. */
  status?: string;
}

/**
 * Tag — text-only label. No background, no border. Linear-style.
 * For status, prefer `status="…"` so bucket→tone derivation stays in one place.
 */
export function Tag({ children, tone, status }: TagProps) {
  const derived: TagTone = status
    ? bucketToTone(STATUS_BUCKET[status as TicketStatus] ?? 'backlog')
    : (tone ?? 'muted');
  return (
    <span
      className="text-xs font-medium uppercase tracking-wide"
      style={{
        color: TONE_VAR[derived],
        letterSpacing: 'var(--tracking-caption)',
        transition: 'color var(--dur-fast) var(--ease-out)',
      }}
    >
      {status ?? children}
    </span>
  );
}

function bucketToTone(bucket: string): TagTone {
  switch (bucket) {
    case 'completed': return 'ok';
    case 'started':   return 'warn';
    case 'blocked':   return 'err';
    case 'cancelled': return 'muted';
    default:          return 'info';
  }
}

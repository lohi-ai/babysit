import type { TicketStatus, TicketSummary } from './data';

// 6 logical buckets across the 11 raw TicketStatus values
export type StatusBucket =
  | 'backlog' | 'unstarted' | 'started'
  | 'blocked' | 'completed' | 'cancelled';

export const STATUS_BUCKET: Record<TicketStatus, StatusBucket> = {
  triage:      'backlog',
  backlog:     'backlog',
  planned:     'unstarted',
  decomposed:  'unstarted',
  in_progress: 'started',
  in_review:   'started',
  blocked:     'blocked',
  done:        'completed',
  cancelled:   'cancelled',
  duplicate:   'cancelled',
  unknown:     'backlog',
};

export const BUCKET_LABEL: Record<StatusBucket, string> = {
  backlog:   'Backlog',
  unstarted: 'Todo',
  started:   'In progress',
  blocked:   'Blocked',
  completed: 'Done',
  cancelled: 'Cancelled',
};

// Workflow ordering (Linear-style)
export const BUCKET_ORDER: StatusBucket[] = [
  'backlog', 'unstarted', 'started', 'blocked', 'completed', 'cancelled',
];

// 4-tier priority derived from existing snapshot data — no schema change
export type Priority = 'urgent' | 'high' | 'medium' | 'low';

export const PRIORITY_LABEL: Record<Priority, string> = {
  urgent: 'Urgent',
  high:   'High',
  medium: 'Medium',
  low:    'Low',
};

export function derivePriority(t: TicketSummary): Priority {
  if (t.status === 'blocked') return 'urgent';
  const size = (t.size ?? '').toUpperCase();
  if (size === 'L') return 'high';
  if (size === 'M') return 'medium';
  return 'low'; // 'S' or null
}

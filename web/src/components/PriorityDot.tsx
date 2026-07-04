import type { Priority } from '../lib/priority';
import { PRIORITY_LABEL } from '../lib/priority';

interface PriorityDotProps {
  priority: Priority;
  size?: number;
}

const PRIORITY_COLOR: Record<Priority, string> = {
  urgent: 'oklch(0.55 0.20 25)',   // red
  high:   'oklch(0.65 0.16 60)',   // orange
  medium: 'oklch(0.70 0.13 95)',   // amber
  low:    'oklch(0.65 0.010 240)', // gray
};

/**
 * PriorityDot — 8 px filled dot, color encoded by priority tier.
 * Derived 4-tier priority (urgent / high / medium / low) — no schema change.
 */
export function PriorityDot({ priority, size = 8 }: PriorityDotProps) {
  return (
    <span
      aria-label={`${PRIORITY_LABEL[priority]} priority`}
      title={`${PRIORITY_LABEL[priority]} priority`}
      style={{
        display: 'inline-block',
        width: size,
        height: size,
        borderRadius: '50%',
        backgroundColor: PRIORITY_COLOR[priority],
        flexShrink: 0,
      }}
    />
  );
}

import type { TicketControl } from '../lib/data';

const TONE = {
  paused: { bg: 'var(--status-started-bg)', fg: 'var(--status-started-text)' },
  cancelled: { bg: 'var(--status-cancelled-bg)', fg: 'var(--status-cancelled-text)' },
} as const;

export function controlTone(state: string) {
  return state === 'cancelled' ? TONE.cancelled : TONE.paused;
}

/**
 * ControlChip — the human's override, rendered in a different shape language
 * from the lifecycle ladder.
 *
 * `Tag` is deliberately text-only with no background, and `StatusArc` is the
 * derived lifecycle rung. A pause is neither: it is a decision someone made,
 * sitting on top of whatever rung the ticket is still on. Giving it a filled
 * bracketed chip is what stops a human from reading a decision as an
 * observation — and is why `status` is never overwritten with "paused".
 */
export function ControlChip({ control }: { control: TicketControl }) {
  const tone = controlTone(control.state);
  return (
    <span
      className="inline-flex items-center font-mono uppercase align-middle"
      style={{
        fontSize: 10,
        lineHeight: '16px',
        padding: '0 5px',
        gap: 3,
        borderRadius: 'var(--radius-sm)',
        backgroundColor: tone.bg,
        color: tone.fg,
        letterSpacing: 'var(--tracking-caption)',
        flexShrink: 0,
      }}
      title={`${control.state} by ${control.actor || 'unknown'} at ${control.at}${control.note ? ` — ${control.note}` : ''}`}
    >
      <span aria-hidden="true">▮</span>
      {control.state}
    </span>
  );
}

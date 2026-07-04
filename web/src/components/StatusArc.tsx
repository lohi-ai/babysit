import type { TicketStatus } from '../lib/data';
import { STATUS_BUCKET, BUCKET_LABEL, type StatusBucket } from '../lib/priority';

interface StatusArcProps {
  status: TicketStatus;
  /** Pixel size; default 14 px (matches plan SVG primitives table) */
  size?: number;
}

/**
 * StatusArc — 14 px circular SVG with 6 distinct shapes, one per bucket.
 * Distinguishable from a 2 m viewing distance (plan AC).
 *
 * Geometry pinned by the parent plan SVG primitives table:
 *   viewBox="0 0 14 14", cx=7, cy=7, r=5.5, stroke-width=1.5
 */
export function StatusArc({ status, size = 14 }: StatusArcProps) {
  const bucket: StatusBucket = STATUS_BUCKET[status];
  const label = BUCKET_LABEL[bucket];

  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 14 14"
      role="img"
      aria-label={label}
      // raw status verbatim, exposed via title for power-users
    >
      <title>{status}</title>
      {renderShape(bucket)}
    </svg>
  );
}

function renderShape(bucket: StatusBucket) {
  const stroke = 'currentColor';
  const fill = 'currentColor';

  switch (bucket) {
    case 'backlog':
      // dashed ring
      return (
        <circle
          cx="7" cy="7" r="5.5"
          fill="none"
          stroke={stroke}
          strokeWidth="1.5"
          strokeDasharray="2 2"
        />
      );
    case 'unstarted':
      // solid ring
      return (
        <circle cx="7" cy="7" r="5.5" fill="none" stroke={stroke} strokeWidth="1.5" />
      );
    case 'started':
      // 3/4 arc — solid ring + filled 270° arc segment to imply progress
      return (
        <>
          <circle cx="7" cy="7" r="5.5" fill="none" stroke={stroke} strokeWidth="1.5" opacity="0.35" />
          <path
            d="M 7 1.5 A 5.5 5.5 0 1 1 1.5 7"
            fill="none"
            stroke={stroke}
            strokeWidth="1.5"
            strokeLinecap="round"
          />
        </>
      );
    case 'blocked':
      // filled ring with center dot
      return (
        <>
          <circle cx="7" cy="7" r="5.5" fill="none" stroke={stroke} strokeWidth="1.5" />
          <circle cx="7" cy="7" r="2" fill={fill} />
        </>
      );
    case 'completed':
      // filled circle + checkmark
      return (
        <>
          <circle cx="7" cy="7" r="5.5" fill={fill} />
          <path
            d="M 4.2 7.2 L 6.2 9.2 L 9.8 4.8"
            fill="none"
            stroke="var(--surface-bg)"
            strokeWidth="1.5"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </>
      );
    case 'cancelled':
      // solid ring with diagonal slash
      return (
        <>
          <circle cx="7" cy="7" r="5.5" fill="none" stroke={stroke} strokeWidth="1.5" />
          <path
            d="M 3.5 3.5 L 10.5 10.5"
            stroke={stroke}
            strokeWidth="1.5"
            strokeLinecap="round"
          />
        </>
      );
  }
}

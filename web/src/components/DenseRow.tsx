import type { ReactNode } from 'react';

interface DenseRowProps {
  /** CSS grid-template-columns value, e.g. "32px 1fr 90px 100px" */
  columns: string;
  children: ReactNode;
  onClick?: () => void;
  tabIndex?: number;
  role?: string;
  /** Mark row as selected (reserved for future multi-select) */
  selected?: boolean;
  /** Render as a header row (no hover, no border-bottom highlight) */
  header?: boolean;
}

/**
 * DenseRow — list row primitive using CSS grid + token surfaces.
 *
 * Body rows: 32 px height, hairline border, hover surface, focus ring.
 * Header rows: same grid, no hover, slightly stronger border.
 */
export function DenseRow({
  columns,
  children,
  onClick,
  tabIndex,
  role,
  selected = false,
  header = false,
}: DenseRowProps) {
  return (
    <div
      className={
        header
          ? 'dense-row dense-row--header'
          : 'dense-row dense-row--body'
      }
      style={{ gridTemplateColumns: columns }}
      onClick={onClick}
      tabIndex={tabIndex}
      role={role}
      data-selected={selected || undefined}
    >
      {children}
    </div>
  );
}

import type { ReactNode } from 'react';
import { Tag } from './Tag';
import { useControlPlane } from '../contexts/ControlContext';

export function TopBar({
  title,
  count,
  breadcrumb,
  actions,
}: {
  title: string;
  count?: number;
  breadcrumb?: ReactNode;
  actions?: ReactNode;
}) {
  const { mode, reason } = useControlPlane();
  return (
    <div
      className="sticky top-0 z-10 flex items-center justify-between"
      style={{
        height: 44,
        paddingInline: 24,
        backgroundColor: 'var(--surface-bg)',
        borderBottom: '1px solid var(--border-hairline)',
      }}
    >
      <div className="flex items-center gap-2 min-w-0">
        {breadcrumb ?? (
          <h1
            className="truncate"
            style={{
              fontSize: 18,
              lineHeight: '24px',
              fontWeight: 500,
              letterSpacing: 'var(--tracking-display)',
              color: 'var(--text-primary)',
            }}
          >
            {title}
          </h1>
        )}
        {typeof count === 'number' && (
          <span
            className="font-mono"
            style={{ fontSize: 13, color: 'var(--text-muted)', fontVariantNumeric: 'tabular-nums' }}
          >
            · {count}
          </span>
        )}
      </div>
      <div className="flex items-center gap-3">
        {/* Why the buttons beside it are dim. Without this the only explanation
            is a tooltip, which a human never hovers to find. */}
        {mode === 'readonly' && (
          <span title={reason} className="whitespace-nowrap">
            <Tag tone="muted">read-only snapshot</Tag>
          </span>
        )}
        {actions && <div className="flex items-center gap-2">{actions}</div>}
      </div>
    </div>
  );
}

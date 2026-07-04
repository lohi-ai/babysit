import type { ReactNode } from 'react';

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
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  );
}

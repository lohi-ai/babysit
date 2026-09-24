import type { ReactNode } from 'react';
import { Tag } from './Tag';
import { useControlPlane } from '../contexts/ControlContext';
import type { SnapshotWarning } from '../lib/data';

export function TopBar({
  title,
  count,
  breadcrumb,
  actions,
  warnings,
}: {
  title: string;
  count?: number;
  breadcrumb?: ReactNode;
  actions?: ReactNode;
  /** Ticket dirs the snapshot walk could not read — rendered once, below the
   *  bar, so a missing ticket is announced rather than silently absent. */
  warnings?: SnapshotWarning[];
}) {
  const { mode, reason } = useControlPlane();
  return (
    <>
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
      {warnings && warnings.length > 0 && (
        <div
          role="status"
          aria-label="Unreadable ticket warnings"
          className="px-6 py-3"
          style={{
            backgroundColor: 'var(--status-started-bg)',
            color: 'var(--status-started-text)',
            borderBottom: '1px solid var(--border-emphasis)',
          }}
        >
          <div className="font-medium" style={{ fontSize: 13 }}>
            {(() => {
              const allTickets = warnings.every(w => w.ticket !== '');
              const noun = allTickets
                ? (warnings.length === 1 ? 'ticket directory' : 'ticket directories')
                : (warnings.length === 1 ? 'directory' : 'directories');
              return `${warnings.length} ${noun} could not be read`;
            })()}
          </div>
          <ul className="mt-1 space-y-1" style={{ fontSize: 13, paddingLeft: 20, listStyle: 'disc' }}>
            {warnings.slice(0, 8).map(w => (
              <li key={`${w.project}/${w.ticket}`} className="break-words">
                <code className="font-mono">{w.ticket ? `${w.project}/${w.ticket}` : w.project}</code>
                {' — '}{w.reason}
              </li>
            ))}
            {warnings.length > 8 && (
              <li style={{ listStyle: 'none' }}>…and {warnings.length - 8} more</li>
            )}
          </ul>
        </div>
      )}
    </>
  );
}

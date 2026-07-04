import type { ReactNode } from 'react';

interface EmptyStateProps {
  title: string;
  body?: string;
  /** 32px lucide icon recommended */
  icon?: ReactNode;
  /** Optional action button slot */
  action?: ReactNode;
}

export function EmptyState({ title, body, icon, action }: EmptyStateProps) {
  return (
    <div className="px-6 py-10 text-center" style={{ color: 'var(--text-muted)' }}>
      {icon && (
        <div className="mx-auto mb-3 flex items-center justify-center" style={{ color: 'var(--text-muted)' }}>
          {icon}
        </div>
      )}
      <div
        className="font-medium"
        style={{ color: 'var(--text-secondary)', fontSize: '14px', lineHeight: 1.4 }}
      >
        {title}
      </div>
      {body && (
        <div className="mt-1" style={{ fontSize: '13px', lineHeight: 1.5 }}>{body}</div>
      )}
      {action && <div className="mt-4 flex justify-center">{action}</div>}
    </div>
  );
}

import type { ReactNode } from 'react';

export function Kbd({ children, dim = false }: { children: ReactNode; dim?: boolean }) {
  return (
    <kbd
      className="font-mono px-1.5 py-0.5"
      style={{
        fontSize: '11px',
        lineHeight: 1,
        borderRadius: 'var(--radius-sm)',
        border: '1px solid var(--border-hairline)',
        backgroundColor: dim ? 'transparent' : 'var(--surface-elevated)',
        color: dim ? 'var(--text-muted)' : 'var(--text-secondary)',
      }}
    >
      {children}
    </kbd>
  );
}

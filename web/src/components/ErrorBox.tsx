import { AlertCircle } from 'lucide-react';

export function ErrorBox({ title, body }: { title: string; body: string }) {
  return (
    <div
      className="flex items-start gap-2 px-3 py-2"
      style={{
        borderRadius: 'var(--radius-md)',
        border: '1px solid var(--border-hairline)',
        backgroundColor: 'var(--surface-elevated)',
      }}
      role="alert"
    >
      <AlertCircle
        size={16}
        strokeWidth={1.75}
        aria-hidden="true"
        style={{ color: 'var(--status-blocked-text)', flexShrink: 0, marginTop: 1 }}
      />
      <div className="min-w-0">
        <div className="font-medium" style={{ fontSize: '13px', color: 'var(--text-primary)' }}>
          {title}
        </div>
        <div
          className="whitespace-pre-wrap"
          style={{ fontSize: '13px', color: 'var(--text-secondary)', marginTop: 2 }}
        >
          {body}
        </div>
      </div>
    </div>
  );
}

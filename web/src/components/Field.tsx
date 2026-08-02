import { useId, type ReactNode } from 'react';

/**
 * Field — visible label + control. The dashboard had no forms until the control
 * plane; this exists so the "visible label, never placeholder-only" rule is
 * structural rather than something each form remembers.
 *
 * `children` is called with the id to put on the control, so the label's `for`
 * always points at something real.
 */
export function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: (id: string) => ReactNode;
}) {
  const id = useId();
  return (
    <div>
      <label
        htmlFor={id}
        className="block font-medium"
        style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 4 }}
      >
        {label}
      </label>
      {children(id)}
      {hint && (
        <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>{hint}</div>
      )}
    </div>
  );
}

/** Shared input skin, so the three forms don't drift apart. */
export const inputStyle: React.CSSProperties = {
  width: '100%',
  minHeight: 32,
  padding: '6px 8px',
  fontSize: 13,
  color: 'var(--text-primary)',
  backgroundColor: 'var(--surface-bg)',
  border: '1px solid var(--border-emphasis)',
  borderRadius: 'var(--radius-sm)',
};

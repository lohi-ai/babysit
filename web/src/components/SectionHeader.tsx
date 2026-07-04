import { useState, type ReactNode } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';

interface SectionHeaderProps {
  title: string;
  count?: number;
  /** When provided, header is interactive and toggles `children` visibility. */
  children?: ReactNode;
  /** Initial expanded state for collapsible sections. Defaults to true. */
  defaultOpen?: boolean;
  /** Right-aligned slot (action button, summary metric, etc.) */
  action?: ReactNode;
}

export function SectionHeader({
  title,
  count,
  children,
  defaultOpen = true,
  action,
}: SectionHeaderProps) {
  const [open, setOpen] = useState(defaultOpen);
  const collapsible = children !== undefined;
  const Chevron = open ? ChevronDown : ChevronRight;

  return (
    <div>
      <div
        className="flex items-center justify-between py-1"
        style={{ color: 'var(--text-muted)' }}
      >
        <button
          type="button"
          onClick={collapsible ? () => setOpen(o => !o) : undefined}
          disabled={!collapsible}
          className="flex items-center gap-1.5 text-left"
          style={{
            color: 'inherit',
            cursor: collapsible ? 'pointer' : 'default',
            background: 'none',
            border: 'none',
            padding: 0,
          }}
        >
          {collapsible && (
            <Chevron size={12} strokeWidth={2} aria-hidden="true" />
          )}
          <span
            className="font-medium uppercase"
            style={{
              fontSize: '11px',
              letterSpacing: 'var(--tracking-caption)',
              color: 'var(--text-muted)',
            }}
          >
            {title}
          </span>
          {typeof count === 'number' && (
            <span
              className="font-mono"
              style={{
                fontSize: '11px',
                padding: '0 6px',
                lineHeight: '16px',
                borderRadius: 'var(--radius-sm)',
                backgroundColor: 'var(--surface-elevated)',
                color: 'var(--text-secondary)',
                fontVariantNumeric: 'tabular-nums',
                marginLeft: 4,
              }}
            >
              {count}
            </span>
          )}
        </button>
        {action && <div>{action}</div>}
      </div>
      {collapsible && open && <div>{children}</div>}
    </div>
  );
}

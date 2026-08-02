import type { ButtonHTMLAttributes, ReactNode } from 'react';

type Variant = 'primary' | 'secondary' | 'ghost';
// `lg` exists for dialog and decision actions: 24/28px are under the 44px
// touch-target floor, which is not acceptable for the highest-stakes buttons
// in the app. The floor is applied below 640px only — a mouse-driven desktop
// row of 44px buttons would tower over the dense chrome around it.
type Size = 'sm' | 'md' | 'lg';

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
  children: ReactNode;
}

export function Button({
  variant = 'secondary',
  size = 'md',
  children,
  style,
  ...rest
}: ButtonProps) {
  const sz = size === 'sm'
    ? { height: 24, padding: '0 8px', fontSize: 12 }
    : size === 'lg'
      ? { height: 36, padding: '0 16px', fontSize: 13 }
      : { height: 28, padding: '0 12px', fontSize: 13 };

  const v = variantStyles(variant);

  return (
    <button
      type="button"
      {...rest}
      className={`inline-flex items-center justify-center gap-1.5 font-medium ${size === 'lg' ? 'max-[639px]:min-h-[44px]' : ''} ${rest.className ?? ''}`}
      style={{
        ...sz,
        ...v.base,
        borderRadius: 'var(--radius-sm)',
        transition:
          'background-color var(--dur-fast) var(--ease-out), border-color var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out)',
        cursor: rest.disabled ? 'not-allowed' : 'pointer',
        opacity: rest.disabled ? 0.5 : 1,
        ...style,
      }}
      onMouseEnter={(e) => {
        if (rest.disabled) return;
        for (const [k, val] of Object.entries(v.hover)) {
          (e.currentTarget.style as unknown as Record<string, string>)[k] = val;
        }
        rest.onMouseEnter?.(e);
      }}
      onMouseLeave={(e) => {
        if (rest.disabled) return;
        for (const [k, val] of Object.entries(v.base)) {
          (e.currentTarget.style as unknown as Record<string, string>)[k] = val;
        }
        rest.onMouseLeave?.(e);
      }}
    >
      {children}
    </button>
  );
}

function variantStyles(variant: Variant) {
  switch (variant) {
    case 'primary':
      return {
        base: {
          backgroundColor: 'var(--accent)',
          color: 'var(--accent-fg)',
          border: '1px solid var(--accent)',
        },
        hover: {
          backgroundColor: 'var(--accent-hover)',
          borderColor: 'var(--accent-hover)',
        },
      };
    case 'ghost':
      return {
        base: {
          backgroundColor: 'transparent',
          color: 'var(--text-secondary)',
          border: '1px solid transparent',
        },
        hover: {
          backgroundColor: 'var(--surface-hover)',
          color: 'var(--text-primary)',
        },
      };
    case 'secondary':
    default:
      return {
        base: {
          backgroundColor: 'var(--surface-bg)',
          color: 'var(--text-primary)',
          border: '1px solid var(--border-emphasis)',
        },
        hover: {
          backgroundColor: 'var(--surface-hover)',
        },
      };
  }
}

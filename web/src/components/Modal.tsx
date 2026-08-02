import { useEffect, useRef, type ReactNode } from 'react';

interface ModalProps {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  /** Footer slot — the modal's actions, right-aligned. */
  actions?: ReactNode;
  /** Max width in px. Default 440. */
  width?: number;
}

/**
 * Modal — overlay + focus trap + Esc, extracted from the pattern
 * `ShortcutsHelp` inlines. Three dialogs need it (spawn foreman, new ticket,
 * cancel confirm); a fourth copy of a hand-rolled dialog is where the focus
 * trap gets forgotten.
 */
export function Modal({ open, onClose, title, children, actions, width = 440 }: ModalProps) {
  const panelRef = useRef<HTMLDivElement | null>(null);
  const returnTo = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!open) return;
    returnTo.current = document.activeElement as HTMLElement | null;
    // Focus the first control so the keyboard path starts inside the dialog.
    const first = panelRef.current?.querySelector<HTMLElement>(
      'input, select, textarea, button, [tabindex]:not([tabindex="-1"])',
    );
    (first ?? panelRef.current)?.focus();
    return () => returnTo.current?.focus();
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onClose();
        return;
      }
      if (e.key !== 'Tab') return;
      // Trap: Tab off either end of the dialog wraps to the other end rather
      // than escaping to the page behind the overlay.
      const focusable = Array.from(
        panelRef.current?.querySelectorAll<HTMLElement>(
          'input:not([disabled]), select:not([disabled]), textarea:not([disabled]), button:not([disabled]), [tabindex]:not([tabindex="-1"])',
        ) ?? [],
      );
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    };
    // Capture phase: the global G/J/K handlers listen on document too, and a
    // "g" typed into the ticket-title field must not navigate the app away.
    document.addEventListener('keydown', onKey, true);
    return () => document.removeEventListener('keydown', onKey, true);
  }, [open, onClose]);

  if (!open) return null;

  const labelId = `modal-title-${title.replace(/\W+/g, '-').toLowerCase()}`;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      style={{ backgroundColor: 'var(--surface-overlay)' }}
      onClick={onClose}
    >
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={labelId}
        tabIndex={-1}
        className="w-full overflow-y-auto"
        style={{
          maxWidth: width,
          maxHeight: 'calc(100vh - 32px)',
          backgroundColor: 'var(--surface-bg)',
          color: 'var(--text-primary)',
          border: '1px solid var(--border-hairline)',
          borderRadius: 'var(--radius-lg)',
          boxShadow: 'var(--shadow-popover)',
        }}
        onClick={e => e.stopPropagation()}
      >
        <div
          className="px-4 py-3"
          style={{ borderBottom: '1px solid var(--border-hairline)' }}
        >
          <h2 id={labelId} style={{ fontSize: 14, fontWeight: 500 }}>{title}</h2>
        </div>
        <div className="px-4 py-4 space-y-3">{children}</div>
        {actions && (
          <div
            className="flex flex-wrap items-center justify-end gap-2 px-4 py-3"
            style={{ borderTop: '1px solid var(--border-hairline)' }}
          >
            {actions}
          </div>
        )}
      </div>
    </div>
  );
}

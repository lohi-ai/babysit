import { Kbd } from './Kbd';
import { BUCKET_LABEL, BUCKET_ORDER } from '../lib/priority';

interface ShortcutsHelpProps {
  open: boolean;
  onClose: () => void;
}

const BINDINGS: { keys: React.ReactNode; desc: string }[] = [
  { keys: <><Kbd>Cmd</Kbd>+<Kbd>K</Kbd></>, desc: 'Open command palette' },
  { keys: <><Kbd>?</Kbd></>,                desc: 'Toggle this help overlay' },
  { keys: <><Kbd>Esc</Kbd></>,              desc: 'Close overlay or palette' },
  { keys: <><Kbd>G</Kbd> <Kbd>H</Kbd></>,   desc: 'Go to Home' },
  { keys: <><Kbd>G</Kbd> <Kbd>T</Kbd></>,   desc: 'Go to Tickets' },
  { keys: <><Kbd>G</Kbd> <Kbd>L</Kbd></>,   desc: 'Go to Live' },
  { keys: <><Kbd>G</Kbd> <Kbd>D</Kbd></>,   desc: 'Go to Decisions' },
  { keys: <><Kbd>G</Kbd> <Kbd>S</Kbd></>,   desc: 'Go to Skill events' },
  { keys: <><Kbd>G</Kbd> <Kbd>M</Kbd></>,   desc: 'Go to Timeline' },
  { keys: <><Kbd>G</Kbd> <Kbd>A</Kbd></>,   desc: 'Go to Analytics' },
  { keys: <><Kbd>J</Kbd> / <Kbd>K</Kbd></>, desc: 'Move row focus down / up' },
  { keys: <><Kbd>↵</Kbd></>,                desc: 'Activate focused row' },
];

export function ShortcutsHelp({ open, onClose }: ShortcutsHelpProps) {
  if (!open) return null;
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Keyboard shortcuts"
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      style={{ backgroundColor: 'var(--surface-overlay)' }}
      onClick={onClose}
    >
      <div
        className="rounded-lg shadow-xl max-w-lg w-full p-6"
        style={{
          backgroundColor: 'var(--surface-bg)',
          color: 'var(--text-primary)',
          border: '1px solid var(--border-hairline)',
        }}
        onClick={e => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold">Keyboard shortcuts</h2>
          <button
            onClick={onClose}
            className="text-sm px-2 py-1 rounded"
            style={{ color: 'var(--text-muted)' }}
            aria-label="Close"
          >
            <Kbd>Esc</Kbd>
          </button>
        </div>

        <ul className="space-y-1.5 mb-6">
          {BINDINGS.map((b, i) => (
            <li key={i} className="flex items-center justify-between text-sm">
              <span style={{ color: 'var(--text-secondary)' }}>{b.desc}</span>
              <span className="flex items-center gap-1">{b.keys}</span>
            </li>
          ))}
        </ul>

        <div>
          <h3 className="text-xs font-semibold uppercase tracking-wide mb-2" style={{ color: 'var(--text-muted)' }}>
            Status buckets
          </h3>
          <ul className="grid grid-cols-2 gap-1.5 text-xs">
            {BUCKET_ORDER.map(b => (
              <li key={b} className="flex items-center gap-2">
                <span
                  className="inline-block w-3 h-3 rounded-full"
                  style={{ backgroundColor: `var(--status-${b}-text)` }}
                  aria-hidden="true"
                />
                <span style={{ color: 'var(--text-secondary)' }}>{BUCKET_LABEL[b]}</span>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </div>
  );
}

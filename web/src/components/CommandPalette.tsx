import { useEffect, useRef, useState, useCallback, useMemo } from 'react';
import { createPortal } from 'react-dom';
import { fuzzySearch, type PaletteItem } from '../lib/fuzzy';
import type { Snapshot } from '../lib/data';

// Static views registry
const STATIC_VIEWS: PaletteItem[] = [
  { id: 'home',         label: 'Home',         kind: 'view', route: '#/' },
  { id: 'live',         label: 'Live',          kind: 'view', route: '#/live' },
  { id: 'tickets',      label: 'Tickets',       kind: 'view', route: '#/tickets' },
  { id: 'decisions',    label: 'Decisions',     kind: 'view', route: '#/decisions' },
  { id: 'skill-events', label: 'Skill events',  kind: 'view', route: '#/skill-events' },
  { id: 'timeline',     label: 'Timeline',      kind: 'view', route: '#/timeline' },
  { id: 'analytics',    label: 'Analytics',     kind: 'view', route: '#/analytics' },
];

const PALETTE_INDEX_CAP = 2000;

function buildIndex(snapshot: Snapshot | null | undefined): { items: PaletteItem[]; truncated: number } {
  const items: PaletteItem[] = [...STATIC_VIEWS];

  if (!snapshot) return { items, truncated: 0 };

  let total = items.length;

  // Projects
  for (const slug of Object.keys(snapshot.projects)) {
    items.push({ id: slug, label: slug, kind: 'project', route: `#/?project=${encodeURIComponent(slug)}` });
    total += 1;
  }

  // Tickets (across all projects)
  for (const proj of Object.values(snapshot.projects)) {
    for (const t of proj.tickets) {
      total += 1;
      if (items.length < PALETTE_INDEX_CAP) {
        items.push({ id: t.id, label: t.title || t.id, kind: 'ticket', route: `#/tickets/${t.id}` });
      }
    }
  }

  return { items, truncated: Math.max(0, total - items.length) };
}

interface CommandPaletteProps {
  open: boolean;
  onClose: () => void;
  snapshot?: Snapshot | null;
}

export function CommandPalette({ open, onClose, snapshot }: CommandPaletteProps) {
  const [query, setQuery] = useState('');
  const [activeIdx, setActiveIdx] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const { items: index, truncated } = useMemo(() => buildIndex(snapshot), [snapshot]);
  const results = useMemo(() => fuzzySearch(query, index), [query, index]);

  // Reset state when opening
  useEffect(() => {
    if (open) {
      setQuery('');
      setActiveIdx(0);
      // Focus input on next tick after portal renders
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [open]);

  // Clamp activeIdx when results change
  useEffect(() => {
    setActiveIdx(prev => (results.length === 0 ? 0 : Math.min(prev, results.length - 1)));
  }, [results]);

  // Scroll active item into view
  useEffect(() => {
    if (!listRef.current) return;
    const el = listRef.current.querySelector<HTMLElement>('[data-active="true"]');
    el?.scrollIntoView({ block: 'nearest' });
  }, [activeIdx]);

  const navigate = useCallback((item: PaletteItem) => {
    window.location.hash = item.route.startsWith('#') ? item.route : `#${item.route}`;
    onClose();
  }, [onClose]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      onClose();
      return;
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setActiveIdx(i => (results.length === 0 ? 0 : (i + 1) % results.length));
      return;
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActiveIdx(i => (results.length === 0 ? 0 : (i - 1 + results.length) % results.length));
      return;
    }
    if (e.key === 'Enter') {
      e.preventDefault();
      const item = results[activeIdx];
      if (item) navigate(item);
      return;
    }
  }, [results, activeIdx, navigate, onClose]);

  if (!open) return null;

  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-start justify-center"
      style={{ paddingTop: '20vh' }}
      onMouseDown={(e) => {
        // Close on backdrop click
        if (e.target === e.currentTarget) onClose();
      }}
      aria-modal="true"
      role="dialog"
      aria-label="Command palette"
    >
      {/* Backdrop — blurred per §1.10 */}
      <div
        className="absolute inset-0"
        style={{
          backgroundColor: 'var(--surface-overlay)',
          backdropFilter: 'blur(4px)',
          WebkitBackdropFilter: 'blur(4px)',
          animation: 'bbs-backdrop-in var(--dur-fast) var(--ease-out)',
        }}
        aria-hidden="true"
      />

      {/* Panel — 640px, hairline border, shadow-modal, spring open */}
      <div
        className="relative w-full mx-4 overflow-hidden"
        style={{
          maxWidth: 640,
          backgroundColor: 'var(--surface-bg)',
          color: 'var(--text-primary)',
          border: '1px solid var(--border-hairline)',
          borderRadius: 'var(--radius-lg)',
          boxShadow: 'var(--shadow-modal)',
          transformOrigin: 'top center',
          animation: 'bbs-modal-in var(--dur-slow) var(--ease-spring)',
        }}
      >
        {/* Search input — 44px tall, 15px placeholder */}
        <div className="flex items-center px-4" style={{ borderBottom: '1px solid var(--border-hairline)', height: 44 }}>
          <svg className="w-4 h-4 shrink-0 mr-3" style={{ color: 'var(--text-muted)' }} viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
            <path fillRule="evenodd" d="M9 3a6 6 0 100 12A6 6 0 009 3zM1 9a8 8 0 1114.32 4.906l4.387 4.387a1 1 0 01-1.414 1.414l-4.387-4.387A8 8 0 011 9z" clipRule="evenodd" />
          </svg>
          <input
            ref={inputRef}
            type="text"
            className="flex-1 bg-transparent outline-none"
            style={{ color: 'var(--text-primary)', fontSize: 15, height: '100%' }}
            placeholder="Search projects, tickets, views…"
            value={query}
            onChange={e => { setQuery(e.target.value); setActiveIdx(0); }}
            onKeyDown={handleKeyDown}
            autoComplete="off"
            spellCheck={false}
          />
          {query && (
            <button
              type="button"
              onClick={() => { setQuery(''); inputRef.current?.focus(); }}
              className="text-xs px-1"
              style={{ color: 'var(--text-muted)' }}
              tabIndex={-1}
              aria-label="Clear"
            >
              ✕
            </button>
          )}
        </div>

        {truncated > 0 && (
          <div
            className="px-4 py-1.5 text-xs"
            style={{
              borderBottom: '1px solid var(--border-hairline)',
              backgroundColor: 'var(--status-started-bg)',
              color: 'var(--status-started-text)',
            }}
          >
            {truncated} more ticket{truncated === 1 ? '' : 's'} hidden — palette index capped at {PALETTE_INDEX_CAP}.
          </div>
        )}

        {/* Results — 36px row height */}
        <div ref={listRef} className="overflow-y-auto" style={{ maxHeight: 360 }} role="listbox">
          {!query ? (
            <div className="px-4 py-8 text-sm text-center" style={{ color: 'var(--text-muted)' }}>
              Type to search projects, tickets, views
            </div>
          ) : results.length === 0 ? (
            <div className="px-4 py-8 text-sm text-center" style={{ color: 'var(--text-muted)' }}>
              No matches for <span className="font-mono" style={{ color: 'var(--text-secondary)' }}>"{query}"</span>
            </div>
          ) : (
            results.map((item, i) => (
              <div
                key={`${item.kind}:${item.id}`}
                role="option"
                aria-selected={i === activeIdx}
                data-active={i === activeIdx ? 'true' : undefined}
                className="flex items-center gap-3 px-4 cursor-pointer text-sm"
                style={{
                  height: 36,
                  backgroundColor: i === activeIdx ? 'var(--surface-elevated)' : 'transparent',
                  transition: 'background-color var(--dur-fast) var(--ease-out)',
                }}
                onMouseEnter={() => setActiveIdx(i)}
                onClick={() => navigate(item)}
              >
                <KindBadge kind={item.kind} />
                <span className="flex-1 truncate" style={{ color: 'var(--text-primary)' }}>{item.label}</span>
                {item.id !== item.label && (
                  <span className="font-mono text-xs shrink-0 truncate max-w-[140px]" style={{ color: 'var(--text-muted)' }}>{item.id}</span>
                )}
              </div>
            ))
          )}
        </div>

        {/* Footer hint */}
        <div
          className="px-4 py-2 flex items-center gap-4 text-xs"
          style={{ borderTop: '1px solid var(--border-hairline)', color: 'var(--text-muted)' }}
        >
          <span><kbd className="font-mono px-1" style={{ backgroundColor: 'var(--surface-elevated)', borderRadius: 'var(--radius-sm)' }}>↑↓</kbd> navigate</span>
          <span><kbd className="font-mono px-1" style={{ backgroundColor: 'var(--surface-elevated)', borderRadius: 'var(--radius-sm)' }}>↵</kbd> jump</span>
          <span><kbd className="font-mono px-1" style={{ backgroundColor: 'var(--surface-elevated)', borderRadius: 'var(--radius-sm)' }}>Esc</kbd> close</span>
        </div>
      </div>
    </div>,
    document.body,
  );
}

function KindBadge({ kind }: { kind: 'project' | 'ticket' | 'view' }) {
  const label = kind === 'project' ? 'proj' : kind;
  return (
    <span
      className="inline-block px-1.5 py-0.5 rounded text-xs font-medium shrink-0"
      style={{
        backgroundColor: 'var(--surface-elevated)',
        color: 'var(--text-secondary)',
        border: '1px solid var(--border-hairline)',
      }}
    >
      {label}
    </span>
  );
}

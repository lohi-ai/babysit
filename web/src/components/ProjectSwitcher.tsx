import { useCallback, useEffect, useRef, useState } from 'react';
import { Check } from 'lucide-react';
import type { Snapshot } from '../lib/data';
import { useFilter } from '../contexts/FilterContext';
import { replaceHashQuery } from '../lib/hash';
import { serializeFilter } from '../lib/filter';

const DONE_STATUSES = new Set(['done', 'cancelled', 'duplicate']);

function openTicketCount(snapshot: Snapshot, slug: string): number {
  const proj = snapshot.projects[slug];
  if (!proj) return 0;
  return proj.tickets.filter(t => !DONE_STATUSES.has(t.status)).length;
}

interface ProjectSwitcherProps {
  snapshot: Snapshot;
}

export function ProjectSwitcher({ snapshot }: ProjectSwitcherProps) {
  const { state, dispatch } = useFilter();
  const slugs = Object.keys(snapshot.projects).sort();
  const currentLabel = state.project === 'all' ? 'All projects' : state.project;

  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);

  const close = useCallback(() => setOpen(false), []);

  const select = (slug: string) => {
    dispatch({ type: 'setProject', payload: slug });
    const next = { ...state, project: slug };
    replaceHashQuery(serializeFilter(next));
    close();
  };

  useEffect(() => {
    if (!open) return;
    const onMouseDown = (e: MouseEvent) => {
      const t = e.target as Node;
      if (panelRef.current?.contains(t)) return;
      if (triggerRef.current?.contains(t)) return;
      close();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        close();
      }
    };
    document.addEventListener('mousedown', onMouseDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onMouseDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open, close]);

  const itemBase = 'w-full text-left px-2 py-1.5 rounded text-xs flex items-center justify-between gap-2';

  const renderOption = (slug: string, label: string, count?: number) => {
    const active = state.project === slug;
    return (
      <button
        key={slug}
        type="button"
        onClick={() => select(slug)}
        className={itemBase}
        style={{
          backgroundColor: active ? 'var(--surface-nav-elevated)' : 'transparent',
          color: active ? 'var(--text-nav-active)' : 'var(--text-nav)',
          transition: 'background-color var(--dur-fast) var(--ease-out)',
        }}
        onMouseEnter={e => {
          if (!active) e.currentTarget.style.backgroundColor = 'var(--surface-hover)';
        }}
        onMouseLeave={e => {
          if (!active) e.currentTarget.style.backgroundColor = 'transparent';
        }}
      >
        <span className="flex items-center gap-2 min-w-0">
          <Check
            size={12}
            strokeWidth={2}
            aria-hidden="true"
            style={{ opacity: active ? 1 : 0, color: 'var(--accent)' }}
          />
          <span className="truncate">{label}</span>
        </span>
        {typeof count === 'number' && count > 0 && (
          <span className="font-mono shrink-0" style={{ color: 'var(--text-muted)' }}>{count}</span>
        )}
      </button>
    );
  };

  return (
    <div className="relative mb-4">
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setOpen(o => !o)}
        aria-haspopup="listbox"
        aria-expanded={open}
        className="w-full flex items-center justify-between px-2 py-1.5 rounded text-sm font-medium"
        style={{
          color: 'var(--text-nav-active)',
          fontFamily: 'var(--font-display)',
          border: '1px solid var(--border-nav)',
          borderRadius: 'var(--radius-sm)',
          backgroundColor: open ? 'var(--surface-nav-elevated)' : 'transparent',
          transition: 'background-color var(--dur-fast) var(--ease-out)',
        }}
        onMouseEnter={e => {
          if (!open) e.currentTarget.style.backgroundColor = 'var(--surface-nav-elevated)';
        }}
        onMouseLeave={e => {
          if (!open) e.currentTarget.style.backgroundColor = 'transparent';
        }}
      >
        <span className="truncate">{currentLabel}</span>
        <svg
          className="w-3.5 h-3.5 shrink-0 ml-1"
          style={{ color: 'var(--text-nav)' }}
          viewBox="0 0 16 16" fill="none"
          aria-hidden="true"
        >
          <path d="M4 6l4 4 4-4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>
      {open && (
        <div
          ref={panelRef}
          role="listbox"
          className="absolute left-0 right-0 mt-1 z-30 p-1 space-y-0.5 max-h-72 overflow-y-auto"
          style={{
            backgroundColor: 'var(--surface-elevated)',
            border: '1px solid var(--border-hairline)',
            borderRadius: 'var(--radius-md)',
            boxShadow: 'var(--shadow-popover)',
          }}
        >
          {renderOption('all', 'All projects')}
          <div style={{ borderTop: '1px solid var(--border-hairline)', margin: '4px 0' }} />
          {slugs.map(slug => renderOption(slug, slug, openTicketCount(snapshot, slug)))}
        </div>
      )}
    </div>
  );
}

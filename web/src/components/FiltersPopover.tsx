import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Filter, Check } from 'lucide-react';
import { useFilter } from '../contexts/FilterContext';
import { useRegisterFilterKey } from '../lib/keyboard';
import type { FilterState } from '../lib/filter';
import { Button } from './Button';
import { Kbd } from './Kbd';

export interface FacetDef {
  kind: 'status' | 'phase' | 'label';
  label: string;
  options: string[];
}

interface FiltersPopoverProps {
  facets: FacetDef[];
}

type Draft = Pick<FilterState, 'status' | 'phase' | 'label'>;

function snapshotDraft(state: FilterState): Draft {
  return { status: [...state.status], phase: [...state.phase], label: [...state.label] };
}

const EMPTY_DRAFT: Draft = { status: [], phase: [], label: [] };

function totalCount(draft: Draft): number {
  return draft.status.length + draft.phase.length + draft.label.length;
}

/**
 * FiltersPopover — Apply-only commit with click-outside / Esc discard.
 *
 *   Local draft initialised from FilterContext on open.
 *   Checkbox toggles mutate local draft only.
 *   Apply  → writes draft → FilterContext (URL hash via existing replaceState path).
 *   Clear  → resets local draft to empty (no commit yet).
 *   Outside click / Esc / programmatic close → discards local draft.
 *
 * Keyboard: registered through the global keyboard hook; `f` opens.
 */
export function FiltersPopover({ facets }: FiltersPopoverProps) {
  const { state, dispatch } = useFilter();

  const committed = useMemo(
    () => ({ status: state.status, phase: state.phase, label: state.label }),
    [state.status, state.phase, state.label],
  );
  const committedCount = committed.status.length + committed.phase.length + committed.label.length;

  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<Draft>(EMPTY_DRAFT);

  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);

  const close = useCallback(() => setOpen(false), []);

  const toggleOpen = useCallback(() => {
    setOpen(o => {
      if (!o) setDraft(snapshotDraft(state));
      return !o;
    });
  }, [state]);

  // Register `f` key with the global keyboard hook (last-mount wins).
  useRegisterFilterKey(toggleOpen);

  // Click-outside / Esc → discard.
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
        triggerRef.current?.focus();
      }
    };
    window.addEventListener('mousedown', onMouseDown);
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('mousedown', onMouseDown);
      window.removeEventListener('keydown', onKey);
    };
  }, [open, close]);

  // Focus first checkbox on open.
  useEffect(() => {
    if (!open) return;
    const t = window.setTimeout(() => {
      const first = panelRef.current?.querySelector<HTMLElement>('[data-filter-option]');
      first?.focus();
    }, 0);
    return () => window.clearTimeout(t);
  }, [open]);

  const draftCount = totalCount(draft);
  const triggerCount = open ? draftCount : committedCount;

  const toggleOption = (kind: FacetDef['kind'], value: string) => {
    setDraft(d => {
      const arr = d[kind];
      const next = arr.includes(value) ? arr.filter(v => v !== value) : [...arr, value];
      return { ...d, [kind]: next };
    });
  };

  const apply = () => {
    const facetKinds: FacetDef['kind'][] = ['status', 'phase', 'label'];
    for (const kind of facetKinds) {
      const live = state[kind];
      const next = draft[kind];
      for (const v of next) if (!live.includes(v)) dispatch({ type: 'toggle', facet: kind, value: v });
      for (const v of live) if (!next.includes(v)) dispatch({ type: 'toggle', facet: kind, value: v });
    }
    close();
    triggerRef.current?.focus();
  };

  const clear = () => setDraft(EMPTY_DRAFT);

  return (
    <div className="relative inline-block">
      <button
        ref={triggerRef}
        type="button"
        onClick={toggleOpen}
        aria-expanded={open}
        aria-haspopup="dialog"
        className="group inline-flex items-center gap-1.5 px-2 py-1"
        style={{
          height: 28,
          fontSize: 13,
          color: 'var(--text-secondary)',
          backgroundColor: open ? 'var(--surface-hover)' : 'transparent',
          border: '1px solid var(--border-emphasis)',
          borderRadius: 'var(--radius-sm)',
          transition: 'background-color var(--dur-fast) var(--ease-out)',
        }}
        onMouseEnter={(e) => { e.currentTarget.style.backgroundColor = 'var(--surface-hover)'; }}
        onMouseLeave={(e) => { if (!open) e.currentTarget.style.backgroundColor = 'transparent'; }}
      >
        <Filter size={14} strokeWidth={1.75} aria-hidden="true" />
        <span>Filter</span>
        {triggerCount > 0 && (
          <span
            className="font-mono"
            style={{
              fontSize: 11,
              lineHeight: '16px',
              padding: '0 6px',
              borderRadius: 'var(--radius-sm)',
              backgroundColor: 'var(--accent-bg-subtle)',
              color: 'var(--accent)',
              fontVariantNumeric: 'tabular-nums',
            }}
          >
            {triggerCount}
          </span>
        )}
        <span
          className="ml-1 opacity-0 group-hover:opacity-60"
          style={{ transition: 'opacity var(--dur-fast) var(--ease-out)' }}
        >
          <Kbd dim>F</Kbd>
        </span>
      </button>

      {open && (
        <div
          ref={panelRef}
          role="dialog"
          aria-label="Filters"
          className="absolute left-0 mt-1 z-30"
          style={{
            width: 280,
            maxHeight: '60vh',
            display: 'flex',
            flexDirection: 'column',
            backgroundColor: 'var(--surface-bg)',
            color: 'var(--text-primary)',
            border: '1px solid var(--border-hairline)',
            borderRadius: 'var(--radius-md)',
            boxShadow: 'var(--shadow-popover)',
            transformOrigin: 'top left',
            animation: 'bbs-popover-in var(--dur-fast) var(--ease-out)',
          }}
        >
          <div className="overflow-y-auto" style={{ flex: 1, minHeight: 0 }}>
            {facets.length === 0 ? (
              <div className="px-3 py-4 text-sm text-center" style={{ color: 'var(--text-muted)' }}>
                No filters available
              </div>
            ) : (
              facets.map((facet, i) => (
                <FacetSection
                  key={facet.kind}
                  facet={facet}
                  selected={draft[facet.kind]}
                  onToggle={(v) => toggleOption(facet.kind, v)}
                  isFirst={i === 0}
                />
              ))
            )}
          </div>

          <div
            className="flex items-center justify-between px-2 py-1.5"
            style={{ borderTop: '1px solid var(--border-hairline)' }}
          >
            <Button variant="ghost" size="sm" onClick={clear} disabled={draftCount === 0}>
              Clear
            </Button>
            <Button variant="primary" size="sm" onClick={apply}>
              Apply
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

function FacetSection({
  facet,
  selected,
  onToggle,
  isFirst,
}: {
  facet: FacetDef;
  selected: string[];
  onToggle: (v: string) => void;
  isFirst: boolean;
}) {
  return (
    <div style={{ borderTop: isFirst ? 'none' : '1px solid var(--border-hairline)' }}>
      <div
        className="px-3 pt-2 pb-1 font-medium uppercase"
        style={{
          fontSize: 11,
          letterSpacing: 'var(--tracking-caption)',
          color: 'var(--text-muted)',
        }}
      >
        {facet.label}
      </div>
      {facet.options.length === 0 ? (
        <div className="px-3 py-1.5 text-xs" style={{ color: 'var(--text-muted)' }}>
          No options
        </div>
      ) : (
        <div className="pb-1">
          {facet.options.map(opt => {
            const checked = selected.includes(opt);
            return (
              <button
                key={opt}
                type="button"
                role="checkbox"
                aria-checked={checked}
                data-filter-option
                onClick={() => onToggle(opt)}
                className="w-full flex items-center gap-2 px-3 py-1.5 text-sm text-left"
                style={{
                  background: 'none',
                  border: 'none',
                  color: 'var(--text-primary)',
                  cursor: 'pointer',
                  transition: 'background-color var(--dur-fast) var(--ease-out)',
                }}
                onMouseEnter={(e) => { e.currentTarget.style.backgroundColor = 'var(--surface-hover)'; }}
                onMouseLeave={(e) => { e.currentTarget.style.backgroundColor = 'transparent'; }}
              >
                <span
                  className="inline-flex items-center justify-center shrink-0"
                  style={{
                    width: 14,
                    height: 14,
                    borderRadius: 'var(--radius-sm)',
                    border: '1px solid',
                    borderColor: checked ? 'var(--accent)' : 'var(--border-emphasis)',
                    backgroundColor: checked ? 'var(--accent)' : 'transparent',
                    color: 'var(--accent-fg)',
                  }}
                >
                  {checked && <Check size={10} strokeWidth={3} aria-hidden="true" />}
                </span>
                <span className="truncate">{opt}</span>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}

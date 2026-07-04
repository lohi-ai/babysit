import { createContext, useContext, useEffect, useRef, useCallback, useState } from 'react';

// =============================================================================
// FocusScope — tracks the currently active focusable row list across views.
// `useRegisterFocusScope` lets a list view publish its rows + focus index.
// `useGlobalKeyboard` consumes the active scope to drive J/K/Enter.
// Last mount wins; only one row list is focus-active at a time.
// =============================================================================

export interface FocusScope {
  rows: HTMLElement[];
  index: number;
  setIndex: (i: number) => void;
}

interface FocusScopeRef {
  current: FocusScope | null;
}

export const FocusScopeContext = createContext<FocusScopeRef>({ current: null });

// =============================================================================
// FilterKeyContext — ref-based registry for the per-view `f` shortcut.
// FiltersPopover registers a handler when mounted; useGlobalKeyboard dispatches
// `f` to the active handler. Last mount wins. Lets the global handler stay
// the only `keydown` listener (no second addEventListener).
// =============================================================================

export interface FilterKeyRef {
  current: (() => void) | null;
}

export const FilterKeyContext = createContext<FilterKeyRef>({ current: null });

export function useRegisterFilterKey(handler: () => void) {
  const ctx = useContext(FilterKeyContext);
  useEffect(() => {
    ctx.current = handler;
    return () => {
      if (ctx.current === handler) ctx.current = null;
    };
  }, [ctx, handler]);
}

export function useRegisterFocusScope(rows: HTMLElement[]) {
  const ctx = useContext(FocusScopeContext);
  const [index, setIndexState] = useState(0);

  // Stable setIndex that also moves DOM focus to the row at that index.
  const setIndex = useCallback((i: number) => {
    const clamped = Math.max(0, Math.min(rows.length - 1, i));
    setIndexState(clamped);
    const el = rows[clamped];
    if (el) el.focus();
  }, [rows]);

  // Publish on every render where rows change.
  useEffect(() => {
    ctx.current = { rows, index, setIndex };
    return () => {
      // Only clear if we still own the scope (last-mount wins, so this is
      // typically true; a later mount having overwritten ctx.current will
      // skip the clear).
      if (ctx.current && ctx.current.rows === rows) {
        ctx.current = null;
      }
    };
  }, [ctx, rows, index, setIndex]);
}

// =============================================================================
// G-prefix router: G then H/T/L/D/S/M/A within 800 ms.
// =============================================================================

const G_ROUTES: Record<string, string> = {
  h: '#/',
  t: '#/tickets',
  l: '#/live',
  d: '#/decisions',
  s: '#/skill-events',
  m: '#/timeline',
  a: '#/analytics',
};

export interface GlobalKeyboardActions {
  togglePalette: () => void;
  toggleHelp: () => void;
  closeOverlays: () => void;
}

/**
 * Mount once at Layout. Drives:
 *  - Cmd/Ctrl+K  → palette toggle
 *  - G prefix    → route navigation (G H/T/L/D/S/M/A within 800 ms)
 *  - J / K       → row focus down/up in active focus scope
 *  - Enter       → activate focused row
 *  - ?           → toggle ShortcutsHelp overlay
 *  - Esc         → close overlays / palette
 *
 * Input-focus guard: ignore keys when target is input/textarea/contenteditable
 * /<summary>. Cmd+K is exempt from the <summary> rule.
 */
export function useGlobalKeyboard(
  scopeRef: FocusScopeRef,
  actions: GlobalKeyboardActions,
  filterKeyRef?: FilterKeyRef,
) {
  const gPendingRef = useRef<number | null>(null);

  useEffect(() => {
    const clearG = () => {
      if (gPendingRef.current !== null) {
        window.clearTimeout(gPendingRef.current);
        gPendingRef.current = null;
      }
    };

    const isInputTarget = (t: EventTarget | null, includeSummary: boolean): boolean => {
      if (!(t instanceof HTMLElement)) return false;
      if (t instanceof HTMLInputElement) return true;
      if (t instanceof HTMLTextAreaElement) return true;
      if (t.isContentEditable) return true;
      if (includeSummary && t.tagName === 'SUMMARY') return true;
      return false;
    };

    const handler = (e: KeyboardEvent) => {
      const cmd = e.metaKey || e.ctrlKey;
      const key = e.key;

      // Cmd+K — exempt from <summary> rule
      if ((key === 'k' || key === 'K') && cmd) {
        if (isInputTarget(e.target, false)) return;
        e.preventDefault();
        clearG();
        actions.togglePalette();
        return;
      }

      // No other shortcut should fire while modifiers are held
      if (cmd || e.altKey) return;

      // From here on, the input/<summary> guard applies
      if (isInputTarget(e.target, true)) return;

      // Esc — close overlays / palette
      if (key === 'Escape') {
        clearG();
        actions.closeOverlays();
        return;
      }

      // ? — toggle help overlay (Shift+/)
      if (key === '?') {
        e.preventDefault();
        clearG();
        actions.toggleHelp();
        return;
      }

      // G prefix — set/clear pending
      if (key === 'g' || key === 'G') {
        clearG();
        gPendingRef.current = window.setTimeout(() => {
          gPendingRef.current = null;
        }, 800);
        return;
      }

      // Pending G + route key
      if (gPendingRef.current !== null) {
        const lower = key.toLowerCase();
        const target = G_ROUTES[lower];
        if (target) {
          e.preventDefault();
          clearG();
          window.location.hash = target;
          return;
        }
        // Any other key cancels pending G
        clearG();
      }

      // F — open filters popover (per-view; last-mounted handler wins)
      if (key === 'f' || key === 'F') {
        const fh = filterKeyRef?.current;
        if (fh) {
          e.preventDefault();
          clearG();
          fh();
          return;
        }
      }

      // J / K — row navigation
      const scope = scopeRef.current;
      if ((key === 'j' || key === 'J') && scope && scope.rows.length > 0) {
        e.preventDefault();
        scope.setIndex(scope.index + 1);
        return;
      }
      if ((key === 'k' || key === 'K') && scope && scope.rows.length > 0) {
        e.preventDefault();
        scope.setIndex(scope.index - 1);
        return;
      }

      // Enter — activate focused row (null-guard activeElement before .click())
      if (key === 'Enter') {
        const focused = document.activeElement;
        if (focused instanceof HTMLElement && scope?.rows.includes(focused)) {
          e.preventDefault();
          focused.click();
        }
        return;
      }
    };

    window.addEventListener('keydown', handler);
    return () => {
      window.removeEventListener('keydown', handler);
      clearG();
    };
  }, [scopeRef, actions, filterKeyRef]);
}

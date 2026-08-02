// Whether this page can mutate anything, and the machinery every mutation
// control shares: refresh-after-write and the live region that announces the
// result.
//
// The three modes come straight from how the page was loaded (see data.ts):
// a file:// snapshot has no server to POST to, and a served page whose poll
// has started failing has one that is gone. Both render controls DISABLED
// rather than hidden — a control that vanishes teaches the human the feature
// does not exist.

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import type { DataSource } from '../lib/data';

export type MutationMode = 'served' | 'readonly' | 'lost';

const REASON: Record<MutationMode, string> = {
  served: '',
  readonly: 'Read-only snapshot. Run `bbs dashboard` to make changes.',
  lost: 'Lost connection to the dashboard server. Retry to re-enable changes.',
};

interface ControlPlane {
  mode: MutationMode;
  canMutate: boolean;
  /** Why mutations are unavailable; '' when they are. */
  reason: string;
  /** Re-read the snapshot. Called after every successful mutation. */
  refresh: () => void;
  /** Announce a mutation result to assistive tech. */
  announce: (msg: string) => void;
}

const ControlContext = createContext<ControlPlane | null>(null);

const REASON_ID = 'control-plane-reason';

/**
 * Props for a mutation control that cannot run right now. Disabled and
 * explained, never hidden.
 */
export function gateProps(reason: string) {
  return reason
    ? { disabled: true, 'aria-disabled': true, 'aria-describedby': REASON_ID, title: reason }
    : {};
}

export function ControlProvider({
  children,
  source,
  error,
  refresh,
}: {
  children: ReactNode;
  source: DataSource;
  error: string | null;
  refresh: () => void;
}) {
  const [message, setMessage] = useState('');

  const mode: MutationMode =
    source !== 'server' ? 'readonly' : error ? 'lost' : 'served';

  const announce = useCallback((msg: string) => setMessage(msg), []);

  const value = useMemo<ControlPlane>(
    () => ({
      mode,
      canMutate: mode === 'served',
      reason: REASON[mode],
      refresh,
      announce,
    }),
    [mode, refresh, announce],
  );

  return (
    <ControlContext.Provider value={value}>
      {children}
      {/* The single description every disabled mutation control points at, so
          the reason reaches a screen reader and not only a mouse hover. */}
      <div id={REASON_ID} hidden>
        {value.reason}
      </div>
      {/* One region for the whole app: every mutation result lands here, so a
          screen reader hears the outcome without the focus having to move. */}
      <div
        aria-live="polite"
        role="status"
        className="sr-only"
        style={{
          position: 'absolute',
          width: 1,
          height: 1,
          overflow: 'hidden',
          clip: 'rect(0 0 0 0)',
          whiteSpace: 'nowrap',
        }}
      >
        {message}
      </div>
    </ControlContext.Provider>
  );
}

/**
 * Views render outside a provider in exactly one case — the loading/no-snapshot
 * fallbacks in App — so this falls back to read-only rather than throwing. A
 * dashboard that crashes because it could not decide whether a button is
 * clickable is worse than one whose button is off.
 */
export function useControlPlane(): ControlPlane {
  return (
    useContext(ControlContext) ?? {
      mode: 'readonly',
      canMutate: false,
      reason: REASON.readonly,
      refresh: () => {},
      announce: () => {},
    }
  );
}

export interface MutationState {
  /** Fire the mutation. Resolves to true on success, false on failure. */
  run: (fn: () => Promise<unknown>, announce?: string) => Promise<boolean>;
  pending: boolean;
  /** The server's message, verbatim. Cleared on the next attempt. */
  error: string | null;
}

/**
 * The in-flight / failed pair every control in the spec needs. Failure keeps
 * the error next to the control that failed and never touches the form state,
 * so a retry starts from what the human already typed.
 */
export function useMutation(): MutationState {
  const { refresh, announce } = useControlPlane();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const run = useCallback(
    async (fn: () => Promise<unknown>, msg?: string) => {
      setPending(true);
      setError(null);
      try {
        await fn();
        // The poll would get there within 5s; refreshing now is what makes the
        // row settle at the moment the human acted.
        refresh();
        if (msg) announce(msg);
        return true;
      } catch (e: unknown) {
        const text = e instanceof Error ? e.message : String(e);
        setError(text);
        announce(`Failed: ${text}`);
        return false;
      } finally {
        setPending(false);
      }
    },
    [refresh, announce],
  );

  return { run, pending, error };
}

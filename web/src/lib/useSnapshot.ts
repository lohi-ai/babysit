import { useCallback, useEffect, useRef, useState } from 'react';
import { dataSource, fetchSnapshot, loadSnapshot, type DataSource, type LoadedSnapshot } from './data';

export interface SnapshotState {
  snapshot: LoadedSnapshot | null;
  /** True only until the FIRST load resolves — poll refreshes must not blank
   *  the views that are already rendered. */
  loading: boolean;
  error: string | null;
  source: DataSource;
  /** Re-read the snapshot now. Served mode only; a no-op over file://. */
  refresh: () => void;
}

const POLL_MS = 5000;

/**
 * The one place the two data sources meet.
 *
 * Snapshot mode resolves synchronously on the first render, exactly as the SPA
 * has always behaved — there is no loading state to see, because the data was
 * already in the document before the bundle ran. Served mode fetches, so every
 * consumer has to tolerate `snapshot === null` for one paint.
 *
 * Polling only runs while the tab is visible: a dashboard left open on another
 * desktop should not keep a reconcile pass running every 5 seconds all day.
 */
export function useSnapshot(): SnapshotState {
  const source = dataSource();
  const [snapshot, setSnapshot] = useState<LoadedSnapshot | null>(() => {
    if (source !== 'snapshot') return null;
    try {
      return loadSnapshot();
    } catch {
      return null;
    }
  });
  const [loading, setLoading] = useState(source === 'server');
  const [error, setError] = useState<string | null>(null);

  // A fetch in flight when the next one starts is abandoned, so a slow response
  // can never overwrite a newer one (a stale render after a mutation is exactly
  // the "acted on stale status" failure this dashboard exists to avoid).
  const inFlight = useRef<AbortController | null>(null);

  const refresh = useCallback(() => {
    if (source !== 'server') return;
    inFlight.current?.abort();
    const ctl = new AbortController();
    inFlight.current = ctl;
    fetchSnapshot(ctl.signal)
      .then((s) => {
        if (ctl.signal.aborted) return;
        setSnapshot(s);
        setError(null);
      })
      .catch((e: unknown) => {
        if (ctl.signal.aborted) return;
        setError(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (!ctl.signal.aborted) setLoading(false);
      });
  }, [source]);

  useEffect(() => {
    if (source !== 'server') return;
    refresh();
    const tick = () => {
      if (document.visibilityState === 'visible') refresh();
    };
    const timer = window.setInterval(tick, POLL_MS);
    // Coming back to the tab should show current state immediately rather than
    // up to POLL_MS of whatever was on screen when it was hidden.
    document.addEventListener('visibilitychange', tick);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener('visibilitychange', tick);
      inFlight.current?.abort();
    };
  }, [source, refresh]);

  return { snapshot, loading, error, source, refresh };
}

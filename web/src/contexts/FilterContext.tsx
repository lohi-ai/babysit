import {
  createContext,
  useContext,
  useEffect,
  useReducer,
  type Dispatch,
  type ReactNode,
} from 'react';
import {
  filterReducer,
  initialFilterState,
  parseFilterQuery,
  serializeFilter,
  type FilterAction,
  type FilterState,
} from '../lib/filter';
import { parseHash, replaceHashQuery } from '../lib/hash';

interface FilterContextValue {
  state: FilterState;
  dispatch: Dispatch<FilterAction>;
}

const FilterContext = createContext<FilterContextValue | null>(null);

function deriveInitialState(activeProject: string | null | undefined): FilterState {
  const { query } = parseHash(window.location.hash);
  if (query) {
    return parseFilterQuery(query, activeProject ?? 'all');
  }
  return initialFilterState(activeProject ?? 'all');
}

export function FilterProvider({
  children,
  activeProject,
}: {
  children: ReactNode;
  activeProject?: string | null;
}) {
  const [state, dispatch] = useReducer(
    filterReducer,
    undefined,
    () => deriveInitialState(activeProject),
  );

  // Write filter state to hash query whenever state changes
  useEffect(() => {
    const query = serializeFilter(state);
    replaceHashQuery(query);
  }, [state]);

  // Sync from hash (e.g., user pastes deep-link URL).
  // Empty-query nav (in-app ticket link clicks, sidebar nav without project=,
  // window.location.hash assignments) preserves current filter state and
  // re-writes the URL to include it. Without this, parseFilterQuery('', default)
  // would reset state to meta.active_project on every empty-query transition,
  // making the active project auto-flip whenever any link omits ?project=.
  // Explicit ?project=all still clears (parsed as project='all'); deep-links
  // with empty query intentionally inherit the current session's project.
  useEffect(() => {
    const onHashChange = () => {
      const { query } = parseHash(window.location.hash);
      if (!query) {
        const serialized = serializeFilter(state);
        if (serialized) replaceHashQuery(serialized);
        return;
      }
      const incoming = parseFilterQuery(query, activeProject ?? 'all');
      const serialized = serializeFilter(state);
      const incomingSerial = serializeFilter(incoming);
      if (incomingSerial === serialized) return;
      dispatch({ type: 'replace', payload: incoming });
    };
    window.addEventListener('hashchange', onHashChange);
    return () => window.removeEventListener('hashchange', onHashChange);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state, activeProject]);

  return (
    <FilterContext.Provider value={{ state, dispatch }}>
      {children}
    </FilterContext.Provider>
  );
}

export function useFilter(): FilterContextValue {
  const ctx = useContext(FilterContext);
  if (!ctx) throw new Error('useFilter must be used inside <FilterProvider>');
  return ctx;
}

// Non-throwing variant for components rendered both inside and outside the
// provider (e.g. Layout in the no-snapshot fallback path).
export function useFilterOptional(): FilterContextValue | null {
  return useContext(FilterContext);
}

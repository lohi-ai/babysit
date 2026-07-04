// FilterContext state, reducer, and serialization helpers.

export interface FilterState {
  project: string;
  status: string[];
  phase: string[];
  label: string[];
}

export type FilterAction =
  | { type: 'replace'; payload: FilterState }
  | { type: 'setProject'; payload: string }
  | { type: 'toggle'; facet: 'status' | 'phase' | 'label'; value: string }
  | { type: 'clear' };

export function initialFilterState(activeProject?: string): FilterState {
  return { project: activeProject ?? 'all', status: [], phase: [], label: [] };
}

export function filterReducer(state: FilterState, action: FilterAction): FilterState {
  switch (action.type) {
    case 'replace':
      return action.payload;
    case 'setProject':
      // Reset per-view filter chips when switching project
      return { project: action.payload, status: [], phase: [], label: [] };
    case 'toggle': {
      const arr = state[action.facet];
      const next = arr.includes(action.value)
        ? arr.filter(v => v !== action.value)
        : [...arr, action.value];
      return { ...state, [action.facet]: next };
    }
    case 'clear':
      return { ...state, status: [], phase: [], label: [] };
    default:
      return state;
  }
}

/** Serialize filter state to a query string (without leading '?'). */
export function serializeFilter(state: FilterState): string {
  const params: string[] = [];
  if (state.project && state.project !== 'all') {
    params.push(`project=${encodeURIComponent(state.project)}`);
  }
  if (state.status.length > 0) {
    params.push(`status=${state.status.map(encodeURIComponent).join(',')}`);
  }
  if (state.phase.length > 0) {
    params.push(`phase=${state.phase.map(encodeURIComponent).join(',')}`);
  }
  if (state.label.length > 0) {
    params.push(`label=${state.label.map(encodeURIComponent).join(',')}`);
  }
  return params.join('&');
}

/** Parse a query string (without leading '?') into FilterState. */
export function parseFilterQuery(query: string, defaultProject = 'all'): FilterState {
  const state = initialFilterState(defaultProject);
  if (!query) return state;
  for (const part of query.split('&')) {
    const eq = part.indexOf('=');
    if (eq < 0) continue;
    const key = part.slice(0, eq);
    const rawVal = part.slice(eq + 1);
    // URL-decode the full value before comma-splitting
    const decoded = decodeURIComponent(rawVal);
    if (key === 'project') {
      state.project = decoded || defaultProject;
    } else if (key === 'status' || key === 'phase' || key === 'label') {
      state[key] = decoded.split(',').filter(Boolean);
    }
  }
  return state;
}

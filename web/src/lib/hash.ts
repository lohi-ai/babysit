// Hash router: parse/serialize #/<route>?<query> with self-trigger guard.

export interface ParsedHash {
  route: string;
  query: string;
}

/**
 * Parse window.location.hash into route + query parts.
 * Handles: #/, #/tickets, #/tickets?status=in_progress, #/tickets/bs-abc123
 */
export function parseHash(hash: string): ParsedHash {
  // Strip leading '#'
  const withoutHash = hash.startsWith('#') ? hash.slice(1) : hash;
  const qIdx = withoutHash.indexOf('?');
  if (qIdx < 0) {
    return { route: withoutHash || '/', query: '' };
  }
  return { route: withoutHash.slice(0, qIdx) || '/', query: withoutHash.slice(qIdx + 1) };
}

/**
 * Build a hash string from route + query.
 * Writes via history.replaceState to avoid polluting the back-stack.
 */
export function writeHash(route: string, query: string): void {
  const newHash = query ? `#${route}?${query}` : `#${route}`;
  // replaceState — chip toggles must NOT create back-button entries
  history.replaceState(null, '', newHash);
  // Also update location.hash so hashchange listeners fire if needed,
  // but since replaceState doesn't fire hashchange we do nothing extra here.
}

/**
 * Update the query portion of the current hash, keeping the route intact.
 * Uses replaceState so chip toggles don't pollute history.
 */
export function replaceHashQuery(query: string): void {
  const { route } = parseHash(window.location.hash);
  writeHash(route, query);
}

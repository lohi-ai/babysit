import { useEffect, useState } from 'react';

/**
 * Media query as state. The dense list is a CSS grid whose column count is a
 * prop, not a class, so the breakpoint has to be readable in JS — a media
 * query in the stylesheet cannot change `grid-template-columns` that arrives
 * inline.
 */
export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() =>
    typeof window !== 'undefined' && window.matchMedia(query).matches,
  );
  useEffect(() => {
    const mql = window.matchMedia(query);
    const on = () => setMatches(mql.matches);
    on();
    mql.addEventListener('change', on);
    return () => mql.removeEventListener('change', on);
  }, [query]);
  return matches;
}

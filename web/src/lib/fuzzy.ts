// Hand-rolled fuzzy matcher — no fuse.js dependency.
// Subsequence scoring with prefix-match bonus + exact-substring bonus.
// Default ranking: projects > tickets > views.

export type ItemKind = 'project' | 'ticket' | 'view';

export interface PaletteItem {
  id: string;
  label: string;
  kind: ItemKind;
  route: string;
}

const KIND_BONUS: Record<ItemKind, number> = {
  project: 300,
  ticket: 200,
  view: 100,
};

/**
 * Score `query` against `text`. Returns a negative number (lower = no match).
 * Scoring:
 *  - exact substring match: +200
 *  - prefix match: +150
 *  - subsequence match: sum of (position bonuses) where consecutive chars score higher
 *  - returns -Infinity if not a subsequence match
 */
function scoreMatch(query: string, text: string): number {
  if (!query) return 0;
  const q = query.toLowerCase();
  const t = text.toLowerCase();

  // Exact substring — must dominate any accumulated subsequence score
  // (long-text subsequence runs can otherwise exceed a small constant).
  const subIdx = t.indexOf(q);
  if (subIdx !== -1) {
    return 2000 + (subIdx === 0 ? 500 : 0);
  }

  // Subsequence check
  let qi = 0;
  let score = 0;
  let consecutive = 0;
  for (let ti = 0; ti < t.length && qi < q.length; ti++) {
    if (t[ti] === q[qi]) {
      // Bonus: characters earlier in string score more
      const posBonus = Math.max(0, 100 - ti * 2);
      // Bonus: consecutive characters
      consecutive++;
      score += posBonus + consecutive * 10;
      qi++;
    } else {
      consecutive = 0;
    }
  }

  if (qi < q.length) return -Infinity; // not a subsequence
  return score;
}

/**
 * Fuzzy match `query` against `items`, returning ranked matches.
 * Items with no match are excluded. Ties broken by kind order (project > ticket > view).
 */
export function fuzzySearch(query: string, items: PaletteItem[]): PaletteItem[] {
  if (!query.trim()) return [];

  const scored: { item: PaletteItem; score: number }[] = [];

  for (const item of items) {
    // Score against both id and label, take best
    const s1 = scoreMatch(query, item.label);
    const s2 = scoreMatch(query, item.id);
    const best = Math.max(s1, s2);
    if (best === -Infinity) continue;
    scored.push({ item, score: best + KIND_BONUS[item.kind] });
  }

  scored.sort((a, b) => b.score - a.score);
  return scored.map(s => s.item);
}

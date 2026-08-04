import { useFilter } from '../contexts/FilterContext';
import { Button } from './Button';

/**
 * The way out of a filtered-empty view. Rendered in the `action` slot of the
 * *no-results* EmptyState only — a view with no data at all has nothing to
 * clear, and offering the button there would be a lie.
 *
 * `clear` resets the facet chips but spreads `...state`, so the active project
 * survives: clearing filters must not silently widen the scope the user is
 * looking at.
 */
export function ClearFiltersAction() {
  const { dispatch } = useFilter();
  return (
    <Button variant="secondary" size="sm" onClick={() => dispatch({ type: 'clear' })}>
      Clear filters
    </Button>
  );
}

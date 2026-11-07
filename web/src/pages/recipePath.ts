import { RESERVED_RECIPE_IDS } from '../planner/keys'

/**
 * URL of a recipe's detail page. Ids are plain slugs, but a recipe saved before `new` and `edit` were reserved
 * could have id "new", which `/recipes/new` (the add form) shadows; those open at `/recipes/by-id/<id>` instead.
 */
export function recipePath(id: string): string {
  return RESERVED_RECIPE_IDS.includes(id) ? `/recipes/by-id/${encodeURIComponent(id)}` : `/recipes/${encodeURIComponent(id)}`
}

/** URL of a recipe's edit form. `/recipes/<id>/edit` has no static sibling, so every id works here. */
export function recipeEditPath(id: string): string {
  return `/recipes/${encodeURIComponent(id)}/edit`
}

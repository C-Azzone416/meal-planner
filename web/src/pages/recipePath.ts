import { RESERVED_RECIPE_IDS } from '../planner/keys'

const isReserved = (id: string) => RESERVED_RECIPE_IDS.includes(id)

/**
 * URL of a recipe's detail page. Ids are plain slugs, but a recipe saved before `new`, `edit` and `by-id` were
 * reserved can carry one of those ids, which `/recipes/new` (the add form) or the by-id routes would capture.
 * Those open under `/recipes/by-id/<id>` instead.
 */
export function recipePath(id: string): string {
  return isReserved(id) ? `/recipes/by-id/${encodeURIComponent(id)}` : `/recipes/${encodeURIComponent(id)}`
}

/**
 * URL of a recipe's edit form. `/recipes/<id>/edit` would be read as the by-id detail route for the id `by-id`
 * (`/recipes/by-id/edit` = detail of recipe "edit"), so reserved ids use `/recipes/by-id/<id>/edit`.
 */
export function recipeEditPath(id: string): string {
  return isReserved(id) ? `/recipes/by-id/${encodeURIComponent(id)}/edit` : `/recipes/${encodeURIComponent(id)}/edit`
}

import { fillOrder } from '../planner/plan'
import type { DayIndex, DeepReadonly, PlannerState } from '../planner/types'

/**
 * The night "Add to week" should use: the first unpinned open night in Plan my week's order
 * (Mon, Wed, Thu, then the earliest other open nights, prep day last).
 */
export function addToWeekNight(plan: DeepReadonly<PlannerState['plan']>, prepDay: DayIndex): DayIndex | undefined {
  return fillOrder(plan, prepDay).find((day) => !plan.nights[day].pinned)
}

import { DEFAULT_PLAN_DAYS } from '../planner/seed'
import type { DayIndex, PlannerState } from '../planner/types'

/**
 * The night "Add to week" should use: open and unpinned, in the same order as Plan my week
 * (Mon, Wed, Thu first, then the earliest other open nights, prep day last).
 *
 * plan.ts does not export its ordering (it is inline in `fillWeek`), so this mirrors it using the
 * exported DEFAULT_PLAN_DAYS. Replace with a shared export if plan.ts grows one.
 */
export function addToWeekNight(plan: PlannerState['plan'], prepDay: DayIndex): DayIndex | undefined {
  const open = plan.nights.filter((night) => night.status === 'open' && !night.pinned).map((night) => night.day)
  const preferred = DEFAULT_PLAN_DAYS.filter((day) => open.includes(day))
  const rest = open
    .filter((day) => !preferred.includes(day as (typeof DEFAULT_PLAN_DAYS)[number]))
    .sort((a, b) => Number(a === prepDay) - Number(b === prepDay) || a - b)
  return [...preferred, ...rest][0]
}

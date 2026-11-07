/** Test helpers shared by planner tests. */
import { reducer, type PlannerAction } from './reducer'
import { createSeedState } from './seed'
import type { DayIndex, Night, PlannerState, Rng } from './types'

/** Seed state with every night open and no progress. */
export function emptyWeek(): PlannerState {
  const state = createSeedState()
  return {
    ...state,
    plan: { ...state.plan, nights: state.plan.nights.map((night): Night => ({ day: night.day, status: 'open', pinned: false })) },
    progress: { shoppingChecked: {}, prepDone: {} },
  }
}

/** Seed state without done prep or low pantry items, so list math is easy to read. */
export function cleanSeed(): PlannerState {
  const state = createSeedState()
  return { ...state, pantryLow: [], progress: { shoppingChecked: {}, prepDone: {} } }
}

export function run(state: PlannerState, ...actions: PlannerAction[]): PlannerState {
  return actions.reduce(reducer, state)
}

/** An Rng that replays the given values (cycling). */
export function sequence(values: number[]): Rng {
  let i = 0
  return () => values[i++ % values.length]
}

export function recipeOn(state: PlannerState, day: DayIndex): string | undefined {
  return state.plan.nights[day].recipeId
}

export function plannedDays(state: PlannerState): DayIndex[] {
  return state.plan.nights.filter((night) => night.status === 'planned').map((night) => night.day)
}

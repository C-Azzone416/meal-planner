/**
 * Pure derivations from PlannerState (docs/ux-decisions.md §3).
 * PHASE 1: output types are final; implementations land in phase 2.
 */
import type { Aisle, DayIndex, PlannerState } from './types'

/** A planned meal that needs an ingredient (used for "chili + chicken" labels). */
export interface MealRef {
  day: DayIndex
  recipeId: string
  title: string
  /** Short name ("chili"), falling back to the title. */
  label: string
}

export type ShoppingRowStatus = 'todo' | 'checked' | 'needs-more'

export interface ShoppingRow {
  /** Stable item key: normalised name + '|' + unit. Progress is stored under this. */
  key: string
  name: string
  unit: string
  /** Total required (scaled + rounded); null for "running low" pantry rows. */
  quantity: number | null
  /** Display amount, e.g. "2 lb", "½ bunch", "3", "running low". */
  amount: string
  aisle: Aisle
  meals: MealRef[]
  /** "chili + chicken" */
  mealsLabel: string
  source: 'recipe' | 'pantry-low'
  status: ShoppingRowStatus
  /** True only when status === 'checked'. A 'needs-more' row renders unchecked. */
  checked: boolean
  checkedQuantity: number | null
  /** For 'needs-more': how much more to buy. */
  shortfall: number | null
  /** For 'needs-more': "need 1 more lb (you checked 2 lb)". */
  note: string | null
}

export interface ShoppingGroup {
  aisle: Aisle
  rows: ShoppingRow[]
}

export interface MealShopping {
  meal: MealRef
  rows: ShoppingRow[]
}

export interface NoLongerNeededRow {
  key: string
  name: string
  unit: string
  aisle: Aisle
  checkedQuantity: number | null
  amount: string
}

export interface ShoppingList {
  rows: ShoppingRow[]
  /** Grouped by aisle in AISLES order; empty aisles omitted. */
  groups: ShoppingGroup[]
  /** Grouped by planned meal (an ingredient shared by two meals appears under both). */
  byMeal: MealShopping[]
  /** Checked rows that are no longer required; still shown (struck through) until dismissed. */
  noLongerNeeded: NoLongerNeededRow[]
  counts: { total: number; toBuy: number; checked: number }
}

export interface PantryItem {
  /** Normalised name; the key used by `togglePantryLow`. */
  key: string
  name: string
  low: boolean
  /** This week's planned meals that use it. */
  usedBy: MealRef[]
  /** Every saved recipe that uses it. */
  usedByRecipes: { id: string; title: string }[]
}

export type PrepTaskStatus = 'todo' | 'done' | 'topup'

export interface PrepWarning {
  /** "Marinade keeps 1 day — prep it Wednesday instead" */
  message: string
  suggestedPrepDay: DayIndex
  daysAhead: number
}

export interface PrepTask {
  /** `${recipeId}:${stepId}` */
  key: string
  recipeId: string
  stepId: string
  text: string
  label: string
  minutes: number
  keepsDays: number | null
  storageNote: string
  /** The night this prep is for. */
  day: DayIndex
  meal: MealRef
  /** Night servings / recipe default servings. */
  factor: number
  /** 'topup' = marked done, but a serving increase means more is needed. Counts as done for progress. */
  status: PrepTaskStatus
  done: boolean
  warning: PrepWarning | null
}

export interface RemovedPrepTask {
  key: string
  text: string
  minutes: number
  mealTitle: string
}

export interface PrepPlan {
  prepDay: DayIndex
  tasks: PrepTask[]
  /** Done tasks whose meal is no longer planned; dismissable. */
  doneNoLongerPlanned: RemovedPrepTask[]
  totals: { tasks: number; done: number; minutes: number; minutesLeft: number }
}

export interface PlanSummary {
  planned: number
  open: number
  skipped: number
  pinned: number
  target: number
  /** How many more dinners the week needs to reach the target (>= 0). */
  remaining: number
  /** True when planned >= target. */
  full: boolean
  /** "3 nights planned" */
  label: string
}

export function deriveShopping(state: PlannerState): ShoppingList {
  void state
  return { rows: [], groups: [], byMeal: [], noLongerNeeded: [], counts: { total: 0, toBuy: 0, checked: 0 } }
}

export function derivePantry(state: PlannerState): PantryItem[] {
  void state
  return []
}

export function derivePrep(state: PlannerState): PrepPlan {
  return { prepDay: state.houseRules.prepDay, tasks: [], doneNoLongerPlanned: [], totals: { tasks: 0, done: 0, minutes: 0, minutesLeft: 0 } }
}

/** Warnings per night (prep-timing), keyed by day index. */
export function deriveNightWarnings(state: PlannerState): Record<DayIndex, string[]> {
  void state
  return { 0: [], 1: [], 2: [], 3: [], 4: [], 5: [], 6: [] }
}

export function planSummary(state: PlannerState): PlanSummary {
  void state
  return { planned: 0, open: 0, skipped: 0, pinned: 0, target: 3, remaining: 3, full: false, label: '' }
}

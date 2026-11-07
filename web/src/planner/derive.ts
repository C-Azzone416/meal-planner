/**
 * Pure derivations from PlannerState (docs/ux-decisions.md §3).
 */
import { formatAmount, formatQuantity } from './format'
import { normalizeName, normalizeUnit, pantryLowKey, prepKey, shoppingKey } from './keys'
import { AISLES, type Aisle, DAY_NAMES, type DayIndex, type PlannerState, type Progress, type Recipe } from './types'

/** Each derivation reads only these slices, so callers can memoise it against exactly them. */
export type PlanInput = Pick<PlannerState, 'recipes' | 'plan'>
export type ShoppingInput = PlanInput & Pick<PlannerState, 'pantryLow'> & { progress: Pick<Progress, 'shoppingChecked'> }
export type PrepInput = PlanInput & { houseRules: Pick<PlannerState['houseRules'], 'prepDay'>; progress: Pick<Progress, 'prepDone'> }
export type PantryInput = PlanInput & Pick<PlannerState, 'pantryLow'>
export type SummaryInput = Pick<PlannerState, 'plan'> & { houseRules: Pick<PlannerState['houseRules'], 'targetDinners'> }

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
  /** Rows no planned meal needs (running-low pantry items); show them as "Pantry top-ups" in the by-meal view. */
  unassigned: ShoppingRow[]
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


const EPSILON = 1e-9

/** Units measured in fractions: round up to quarter steps. */
const QUARTER_UNITS = new Set(['cup', 'lb', 'oz', 'tbsp', 'tsp', 'quart', 'gallon', 'g', 'kg', 'ml', 'l'])
/** Units sold in halves. */
const HALF_UNITS = new Set(['bunch'])

/**
 * Round a *total* up to something you can buy (§3): ¼ steps for measured units,
 * ½ for bunches, whole numbers for counts. Always rounds up so nobody under-buys.
 */
export function roundQuantity(quantity: number, unit: string): number {
  const u = normalizeUnit(unit)
  const step = QUARTER_UNITS.has(u) ? 0.25 : HALF_UNITS.has(u) ? 0.5 : 1
  const rounded = Math.ceil(quantity / step - EPSILON) * step
  return Math.max(rounded, step)
}

function mealsOf(state: PlanInput): { meal: MealRef; recipe: Recipe; servings: number; factor: number }[] {
  const meals: { meal: MealRef; recipe: Recipe; servings: number; factor: number }[] = []
  for (const night of state.plan.nights) {
    if (night.status !== 'planned') continue
    const recipe = state.recipes.find((candidate) => candidate.id === night.recipeId)
    if (!recipe || night.recipeId === undefined) continue
    const servings = night.servings ?? recipe.defaultServings
    // A corrupt default (0, negative, NaN) must never produce Infinity/NaN quantities: don't scale.
    const validDefault = Number.isFinite(recipe.defaultServings) && recipe.defaultServings > 0
    meals.push({
      meal: { day: night.day, recipeId: recipe.id, title: recipe.title, label: recipe.shortName ?? recipe.title },
      recipe,
      servings,
      factor: validDefault && Number.isFinite(servings) ? servings / recipe.defaultServings : 1,
    })
  }
  return meals
}

function mealsLabel(meals: MealRef[]): string {
  return [...new Set(meals.map((meal) => meal.label))].join(' + ')
}

/** "1 more lb", "2 more", "½ more bunch" */
function moreLabel(quantity: number, unit: string): string {
  const amount = formatAmount(quantity, unit)
  const q = formatQuantity(quantity)
  return unit ? `${q} more ${amount.slice(q.length + 1)}` : `${q} more`
}

interface Accumulator {
  key: string
  name: string
  unit: string
  aisle: Aisle
  total: number | null
  meals: MealRef[]
}

/**
 * Shopping list derived from the plan + recipes (§3). Consolidates by normalised name + unit,
 * scales by servings, excludes pantry staples (unless running low), and layers per-item
 * progress on top: a checked row whose requirement grows becomes 'needs-more'; a checked
 * row that is no longer required moves to `noLongerNeeded`.
 */
export function deriveShopping(state: ShoppingInput): ShoppingList {
  const planned = mealsOf(state)
  const accumulators = new Map<string, Accumulator>()
  const pantryUse = new Map<string, MealRef[]>()
  const pantryNames = new Map<string, string>()

  for (const { meal, recipe, factor } of planned) {
    for (const ingredient of recipe.ingredients) {
      if (ingredient.isPantry) {
        const pantryKey = normalizeName(ingredient.name)
        pantryNames.set(pantryKey, pantryNames.get(pantryKey) ?? ingredient.name)
        const users = pantryUse.get(pantryKey) ?? []
        if (!users.some((existing) => existing.day === meal.day)) users.push(meal)
        pantryUse.set(pantryKey, users)
        continue
      }
      const key = shoppingKey(ingredient.name, ingredient.unit)
      const entry = accumulators.get(key) ?? {
        key,
        name: ingredient.name,
        unit: ingredient.unit,
        aisle: ingredient.aisle,
        total: null,
        meals: [],
      }
      if (ingredient.quantity !== null) entry.total = (entry.total ?? 0) + ingredient.quantity * factor
      if (!entry.meals.some((existing) => existing.day === meal.day)) entry.meals.push(meal)
      accumulators.set(key, entry)
    }
  }

  const { shoppingChecked } = state.progress
  const makeRow = (
    base: { key: string; name: string; unit: string; aisle: Aisle; quantity: number | null; meals: MealRef[]; source: ShoppingRow['source'] },
  ): ShoppingRow => {
    const progress = shoppingChecked[base.key]
    let status: ShoppingRowStatus = 'todo'
    let shortfall: number | null = null
    if (progress) {
      status = 'checked'
      if (base.quantity !== null && progress.checkedQuantity !== null && base.quantity > progress.checkedQuantity + EPSILON) {
        status = 'needs-more'
        shortfall = base.quantity - progress.checkedQuantity
      }
    }
    return {
      ...base,
      amount: base.source === 'pantry-low' ? 'running low' : formatAmount(base.quantity, base.unit),
      mealsLabel: mealsLabel(base.meals),
      status,
      checked: status === 'checked',
      checkedQuantity: progress?.checkedQuantity ?? null,
      shortfall,
      note:
        status === 'needs-more' && shortfall !== null && progress
          ? `need ${moreLabel(shortfall, base.unit)} (you checked ${formatAmount(progress.checkedQuantity, base.unit)})`
          : null,
    }
  }

  const rows: ShoppingRow[] = []
  for (const entry of accumulators.values()) {
    rows.push(
      makeRow({
        key: entry.key,
        name: entry.name,
        unit: entry.unit,
        aisle: entry.aisle,
        quantity: entry.total === null ? null : roundQuantity(entry.total, entry.unit),
        meals: entry.meals,
        source: 'recipe',
      }),
    )
  }
  for (const low of state.pantryLow) {
    const pantryKey = normalizeName(low)
    // Its own identity (never merged with a regular row for the same word), so the signal and its progress survive.
    const key = pantryLowKey(low)
    const name = pantryNames.get(pantryKey) ?? pantryDisplayName(state, pantryKey) ?? low
    rows.push(
      makeRow({ key, name, unit: '', aisle: 'Pantry aisle', quantity: null, meals: pantryUse.get(pantryKey) ?? [], source: 'pantry-low' }),
    )
  }
  rows.sort((a, b) => a.name.localeCompare(b.name))

  const groups: ShoppingGroup[] = AISLES.map((aisle) => ({ aisle, rows: rows.filter((row) => row.aisle === aisle) })).filter(
    (group) => group.rows.length > 0,
  )
  const byMeal: MealShopping[] = planned.map(({ meal }) => ({
    meal,
    rows: rows.filter((row) => row.meals.some((candidate) => candidate.day === meal.day)),
  }))

  const liveKeys = new Set(rows.map((row) => row.key))
  const noLongerNeeded: NoLongerNeededRow[] = Object.entries(shoppingChecked)
    .filter(([key]) => !liveKeys.has(key))
    .map(([key, progress]) => ({
      key,
      name: progress.name,
      unit: progress.unit,
      aisle: progress.aisle,
      checkedQuantity: progress.checkedQuantity,
      amount: formatAmount(progress.checkedQuantity, progress.unit),
    }))
    .sort((a, b) => a.name.localeCompare(b.name))

  const checked = rows.filter((row) => row.checked).length
  const unassigned = rows.filter((row) => row.meals.length === 0)
  return { rows, groups, byMeal, unassigned, noLongerNeeded, counts: { total: rows.length, toBuy: rows.length - checked, checked } }
}

function pantryDisplayName(state: PlanInput, pantryKey: string): string | undefined {
  for (const recipe of state.recipes) {
    for (const ingredient of recipe.ingredients) {
      if (ingredient.isPantry && normalizeName(ingredient.name) === pantryKey) return ingredient.name
    }
  }
  return undefined
}

/** The pantry shelf: every staple any saved recipe uses (plus anything marked low), sorted by name. */
export function derivePantry(state: PantryInput): PantryItem[] {
  const items = new Map<string, PantryItem>()
  const lows = new Set(state.pantryLow.map(normalizeName))
  for (const recipe of state.recipes) {
    for (const ingredient of recipe.ingredients) {
      if (!ingredient.isPantry) continue
      const key = normalizeName(ingredient.name)
      const item = items.get(key) ?? { key, name: ingredient.name, low: lows.has(key), usedBy: [], usedByRecipes: [] }
      if (!item.usedByRecipes.some((used) => used.id === recipe.id)) item.usedByRecipes.push({ id: recipe.id, title: recipe.title })
      items.set(key, item)
    }
  }
  for (const { meal, recipe } of mealsOf(state)) {
    for (const ingredient of recipe.ingredients) {
      const item = ingredient.isPantry ? items.get(normalizeName(ingredient.name)) : undefined
      if (item && !item.usedBy.some((used) => used.day === meal.day)) item.usedBy.push(meal)
    }
  }
  for (const key of lows) {
    if (!items.has(key)) items.set(key, { key, name: key, low: true, usedBy: [], usedByRecipes: [] })
  }
  return [...items.values()].sort((a, b) => a.name.localeCompare(b.name))
}

/**
 * Prep-ahead tasks for every planned meal, with done state and keep-time warnings (§3).
 * A done task whose serving factor has since grown is 'topup' (still counted done).
 */
export function derivePrep(state: PrepInput): PrepPlan {
  const { prepDay } = state.houseRules
  const { prepDone } = state.progress
  const tasks: PrepTask[] = []

  for (const { meal, recipe, factor } of mealsOf(state)) {
    for (const step of recipe.prepAhead) {
      const key = prepKey(recipe.id, step.id)
      const record = prepDone[key]
      const status: PrepTaskStatus = !record ? 'todo' : factor > record.doneFactor + EPSILON ? 'topup' : 'done'
      const label = step.label ?? step.text
      let warning: PrepWarning | null = null
      if (step.keepsDays !== null) {
        const daysAhead = (meal.day - prepDay + 7) % 7
        if (daysAhead > step.keepsDays) {
          const suggestedPrepDay = (((meal.day - step.keepsDays) % 7) + 7) % 7 as DayIndex
          warning = {
            message: `${label} keeps ${step.keepsDays} ${step.keepsDays === 1 ? 'day' : 'days'} — prep it ${DAY_NAMES[suggestedPrepDay]} instead`,
            suggestedPrepDay,
            daysAhead,
          }
        }
      }
      tasks.push({
        key,
        recipeId: recipe.id,
        stepId: step.id,
        text: step.text,
        label,
        minutes: step.minutes,
        keepsDays: step.keepsDays,
        storageNote: step.storageNote,
        day: meal.day,
        meal,
        factor,
        status,
        done: status !== 'todo',
        warning,
      })
    }
  }

  const live = new Set(tasks.map((task) => task.key))
  const doneNoLongerPlanned: RemovedPrepTask[] = Object.entries(prepDone)
    .filter(([key]) => !live.has(key))
    .map(([key, record]) => ({ key, text: record.text, minutes: record.minutes, mealTitle: record.mealTitle }))

  return {
    prepDay,
    tasks,
    doneNoLongerPlanned,
    totals: {
      tasks: tasks.length,
      done: tasks.filter((task) => task.done).length,
      minutes: tasks.reduce((sum, task) => sum + task.minutes, 0),
      minutesLeft: tasks.reduce((sum, task) => sum + (task.done ? 0 : task.minutes), 0),
    },
  }
}

/** Prep-timing warnings per night, keyed by day index. */
export function deriveNightWarnings(state: PrepInput): Record<DayIndex, string[]> {
  return nightWarningsFromPrep(derivePrep(state))
}

/** Same as `deriveNightWarnings` for callers that already hold the prep plan (avoids deriving it twice). */
export function nightWarningsFromPrep(prep: PrepPlan): Record<DayIndex, string[]> {
  const warnings: Record<DayIndex, string[]> = { 0: [], 1: [], 2: [], 3: [], 4: [], 5: [], 6: [] }
  for (const task of prep.tasks) {
    if (task.warning) warnings[task.day].push(task.warning.message)
  }
  return warnings
}

export function planSummary(state: SummaryInput): PlanSummary {
  const { nights } = state.plan
  const planned = nights.filter((night) => night.status === 'planned').length
  const target = state.houseRules.targetDinners
  return {
    planned,
    open: nights.filter((night) => night.status === 'open').length,
    skipped: nights.filter((night) => night.status === 'skipped').length,
    pinned: nights.filter((night) => night.pinned).length,
    target,
    remaining: Math.max(0, target - planned),
    full: planned >= target,
    label: `${planned} night${planned === 1 ? '' : 's'} planned`,
  }
}

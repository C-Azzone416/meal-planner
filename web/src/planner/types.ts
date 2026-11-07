/**
 * Domain types for the client-side planner store.
 *
 * Everything here is plain JSON-serialisable data so the whole state can be
 * persisted to localStorage and, later, swapped for server responses.
 * See docs/ux-decisions.md (§0–§3) for the product rules these types encode.
 */

/** 0 = Sunday … 6 = Saturday. */
export type DayIndex = 0 | 1 | 2 | 3 | 4 | 5 | 6

export const DAY_NAMES = [
  'Sunday',
  'Monday',
  'Tuesday',
  'Wednesday',
  'Thursday',
  'Friday',
  'Saturday',
] as const

export const DAY_SHORT_NAMES = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'] as const

export const DAYS: readonly DayIndex[] = [0, 1, 2, 3, 4, 5, 6]

/** Store aisles, in the order a shopping list shows them. */
export const AISLES = [
  'Produce',
  'Meat & fish',
  'Dairy',
  'Bakery',
  'Pantry aisle',
  'Frozen',
  'Other',
] as const
export type Aisle = (typeof AISLES)[number]

export type Attention = 'low' | 'medium' | 'high'

export interface Ingredient {
  /** Unique within its recipe/draft. */
  id: string
  name: string
  /** Amount for `defaultServings`; null = "to taste" / no amount. */
  quantity: number | null
  /** '' for plain counts ("3 onions"). Examples: 'lb', 'oz', 'cup', 'tbsp', 'tsp', 'can', 'head', 'bunch', 'pint'. */
  unit: string
  aisle: Aisle
  /** Pantry staples (oil, salt, spices…) never appear on the shopping list unless marked running low. */
  isPantry: boolean
}

/** A step that can be done before the meal day. */
export interface PrepAheadStep {
  /** Unique within its recipe/draft; with the recipe id it forms the prep task key. */
  id: string
  text: string
  /** Short noun used in warnings ("Marinade keeps 1 day…"). Falls back to `text`. */
  label?: string
  minutes: number
  /** How many days the result keeps once done; null = no limit worth warning about. */
  keepsDays: number | null
  storageNote: string
}

export interface PrepTonightStep {
  id: string
  text: string
  minutes: number
}

export interface CookStep {
  id: string
  text: string
  timerSeconds: number | null
}

export interface Recipe {
  /** Stable id (slug). Titles can change without breaking URLs. */
  id: string
  title: string
  /** Optional short name for compact lists ("chili"); falls back to `title`. */
  shortName?: string
  icon: string
  activeMinutes: number
  totalMinutes: number
  attention: Attention
  tags: string[]
  defaultServings: number
  ingredients: Ingredient[]
  prepAhead: PrepAheadStep[]
  prepTonight: PrepTonightStep[]
  cookTonight: CookStep[]
}

/** Fields the user supplies when adding a recipe; the store assigns the id. */
export type NewRecipe = Omit<Recipe, 'id'>

/** The five agent guesses a person must confirm before a draft can be approved. */
export const DRAFT_GUESS_KEYS = [
  'activeMinutes',
  'totalMinutes',
  'attention',
  'prepAhead',
  'storage',
] as const
export type DraftGuessKey = (typeof DRAFT_GUESS_KEYS)[number]

/**
 * Agent-created content awaiting review. Deliberately a separate type from
 * Recipe: drafts never appear in recipe lists or the planner until approved.
 */
export interface Draft {
  id: string
  revision: number
  title: string
  icon: string
  activeMinutes: number
  totalMinutes: number
  attention: Attention
  tags: string[]
  defaultServings: number
  ingredients: Ingredient[]
  prepAhead: PrepAheadStep[]
  prepTonight: PrepTonightStep[]
  cookTonight: CookStep[]
  /** Structural-validation warnings from the draft checker (not food-safety review). */
  warnings: string[]
  /** Which guesses the person has confirmed so far. */
  confirmed: Record<DraftGuessKey, boolean>
}

export type NightStatus = 'planned' | 'open' | 'skipped'

export interface Night {
  day: DayIndex
  status: NightStatus
  /** Present only when status === 'planned'. */
  recipeId?: string
  /** Present only when status === 'planned'. 1–12. */
  servings?: number
  /** Pinned nights are never changed by Plan my week / Surprise me. */
  pinned: boolean
}

export interface WeekPlan {
  /** Always 7 nights, index === day (Sunday first). */
  nights: Night[]
  /** Bumped by every plan mutation; the future API will use it for optimistic concurrency. */
  revision: number
}

/** Shopping progress, keyed by shopping item key (see `shoppingKey`). */
export interface ShoppingProgress {
  /** Amount the person had checked off when they ticked it; null if the row had no amount. */
  checkedQuantity: number | null
  /** Snapshots so a row can still be shown after it is no longer needed. */
  name: string
  unit: string
  aisle: Aisle
}

/** Prep progress, keyed by prep task key (see `prepKey`). */
export interface PrepProgress {
  /** Serving factor (night servings / recipe default) when marked done. A higher factor later = "needs a top-up". */
  doneFactor: number
  /** Snapshots so the task can still be shown after its meal is removed. */
  text: string
  minutes: number
  mealTitle: string
}

export interface Progress {
  shoppingChecked: Record<string, ShoppingProgress>
  prepDone: Record<string, PrepProgress>
}

export interface HouseRules {
  /** Dinners per week that Plan my week fills up to. */
  targetDinners: number
  /** Day the weekend prep happens. 0 = Sunday. */
  prepDay: DayIndex
}

/** A pending Surprise me suggestion. Nothing changes until it is kept. */
export interface SurpriseProposal {
  day: DayIndex
  recipeId: string
  /** Recipes already offered for this night, so "Try another" does not repeat. */
  tried: string[]
}

/** One level of undo: the plan + progress as they were before the last plan-changing action. */
export interface UndoEntry {
  /** Monotonic; use as a React key / timer reset so a new change restarts the 10 s window. */
  seq: number
  /** Human message, e.g. "Swapped Thursday to Red lentil coconut soup". */
  message: string
  plan: WeekPlan
  progress: Progress
  /** Used to coalesce repeated servings tweaks on the same night into one undo step. */
  coalesceKey?: string
  /** `progress` as it was right after the change; if it differs later, coalescing must start a new step. */
  progressAfter?: Progress
}

export type StatusKind = 'info' | 'success' | 'warning'

/** Latest user-facing message from a store action (render with role="status"). */
export interface StatusMessage {
  seq: number
  kind: StatusKind
  text: string
}

export interface PlannerState {
  recipes: Recipe[]
  drafts: Draft[]
  plan: WeekPlan
  progress: Progress
  /** Normalised pantry ingredient names currently marked "running low". */
  pantryLow: string[]
  houseRules: HouseRules
  /** Transient (not persisted). */
  proposal: SurpriseProposal | null
  /** Transient (not persisted). */
  undo: UndoEntry | null
  /** Transient (not persisted). */
  status: StatusMessage | null
  /** Counter feeding `undo.seq` / `status.seq`. */
  seq: number
}

/** Random source: returns a float in [0, 1). Injectable for tests. */
export type Rng = () => number

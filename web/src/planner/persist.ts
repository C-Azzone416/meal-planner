/**
 * localStorage persistence for the preview store (docs/ux-decisions.md §0).
 * One versioned key; anything unreadable is discarded and reseeded, never thrown.
 */
import { isWellFormed } from './keys'
import { createSeedState } from './seed'
import { AISLES, DRAFT_GUESS_KEYS, type PlannerState } from './types'

export const STORAGE_KEY = 'mealplanner.preview.v1'
/** Provider user id, not email: account renames keep their own data. Legacy data has no proven owner. */
export function accountStorageKey(accountId: string): string {
  return `${STORAGE_KEY}.account.${encodeURIComponent(accountId)}`
}
export const LEGACY_NOTICE = 'Older browser data has no recorded account owner. It is preserved but has not been loaded into this account.'
/** Bumped to 2 when key encoding changed (old progress keys would silently orphan). The storage key name is unchanged. */
export const STORAGE_VERSION = 2
export const RESET_NOTICE = 'We reset the preview data because it could not be read.'

/** The subset of Storage the store needs; lets tests inject an in-memory fake. */
export interface StorageLike {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
  removeItem(key: string): void
}

export type PersistedState = Pick<PlannerState, 'recipes' | 'drafts' | 'plan' | 'progress' | 'pantryLow' | 'houseRules'>

export function toPersisted(state: PersistedState): { version: number; state: PersistedState } {
  const { recipes, drafts, plan, progress, pantryLow, houseRules } = state
  return { version: STORAGE_VERSION, state: { recipes, drafts, plan, progress, pantryLow, houseRules } }
}

export function serialize(state: PersistedState): string {
  return JSON.stringify(toPersisted(state))
}

export interface LoadResult {
  state: PlannerState
  /** Set when stored data existed but could not be used. */
  resetNotice: string | null
  readFailed?: boolean
}

/** Browser storage if usable (access can throw in private modes), else null. */
export function defaultStorage(): StorageLike | null {
  try {
    return typeof window !== 'undefined' && window.localStorage ? window.localStorage : null
  } catch {
    return null
  }
}

export function loadState(storage: StorageLike | null, key = STORAGE_KEY): LoadResult {
  if (!storage) return { state: createSeedState(), resetNotice: null }
  let raw: string | null
  try {
    raw = storage.getItem(key)
  } catch {
    return { state: createSeedState(), resetNotice: null, readFailed: true }
  }
  if (raw === null) return { state: createSeedState(), resetNotice: null }
  const parsed = parsePersisted(raw)
  if (!parsed) return { state: createSeedState(), resetNotice: RESET_NOTICE }
  return { state: { ...createSeedState(), ...parsed }, resetNotice: null }
}

export function saveState(storage: StorageLike | null, state: PersistedState, key = STORAGE_KEY): boolean {
  if (!storage) return false
  try {
    storage.setItem(key, serialize(state))
    return true
  } catch {
    // Quota exceeded or storage disabled: the app keeps working in memory, visibly unsaved.
    return false
  }
}

type Json = Record<string, unknown>

const isRecord = (value: unknown): value is Json => typeof value === 'object' && value !== null && !Array.isArray(value)
const isString = (value: unknown): value is string => typeof value === 'string' && isWellFormed(value)
const isNum = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value)
const isArrayOf = <T,>(value: unknown, guard: (item: unknown) => item is T): value is T[] => Array.isArray(value) && value.every(guard)
const isStringArray = (value: unknown): value is string[] => isArrayOf(value, isString)

function isIngredient(value: unknown): boolean {
  return (
    isRecord(value) &&
    isString(value.id) &&
    isString(value.name) &&
    (value.quantity === null || isNum(value.quantity)) &&
    isString(value.unit) &&
    (AISLES as readonly unknown[]).includes(value.aisle) &&
    typeof value.isPantry === 'boolean'
  )
}

const isPrepAhead = (value: unknown): boolean =>
  isRecord(value) &&
  isString(value.id) &&
  isString(value.text) &&
  isNum(value.minutes) &&
  (value.keepsDays === null || isNum(value.keepsDays)) &&
  isString(value.storageNote) &&
  (value.label === undefined || isString(value.label))

const isPrepTonight = (value: unknown): boolean => isRecord(value) && isString(value.id) && isString(value.text) && isNum(value.minutes)

const isCookStep = (value: unknown): boolean =>
  isRecord(value) && isString(value.id) && isString(value.text) && (value.timerSeconds === null || isNum(value.timerSeconds))

function hasUniqueIds(items: unknown): boolean {
  const ids = (items as Json[]).map((item) => item.id)
  return new Set(ids).size === ids.length
}

function hasRecipeBody(value: Json): boolean {
  return (
    isString(value.id) &&
    value.id !== '' &&
    isString(value.title) &&
    isString(value.icon) &&
    isNum(value.activeMinutes) &&
    isNum(value.totalMinutes) &&
    (value.attention === 'low' || value.attention === 'medium' || value.attention === 'high') &&
    isStringArray(value.tags) &&
    isNum(value.defaultServings) &&
    value.defaultServings >= 1 &&
    Array.isArray(value.ingredients) && value.ingredients.every(isIngredient) && hasUniqueIds(value.ingredients) &&
    Array.isArray(value.prepAhead) && value.prepAhead.every(isPrepAhead) && hasUniqueIds(value.prepAhead) &&
    Array.isArray(value.prepTonight) && value.prepTonight.every(isPrepTonight) &&
    Array.isArray(value.cookTonight) && value.cookTonight.every(isCookStep)
  )
}

const isRecipe = (value: unknown): boolean => isRecord(value) && hasRecipeBody(value)

const isDraft = (value: unknown): boolean =>
  isRecord(value) &&
  hasRecipeBody(value) &&
  isNum(value.revision) &&
  isStringArray(value.warnings) &&
  isRecord(value.confirmed) &&
  DRAFT_GUESS_KEYS.every((key) => typeof (value.confirmed as Json)[key] === 'boolean')

// Hosted recipe UUID references survive reload without persisting database recipe content.
// Their content is fetched separately; an unavailable database must not reset the local plan.
function isPlan(value: unknown, recipeIds: Set<string>): boolean {
  if (!isRecord(value) || !isNum(value.revision) || !Array.isArray(value.nights) || value.nights.length !== 7) return false
  // A recipe may appear on only one night per week.
  const planned = value.nights.filter((night: unknown) => isRecord(night) && night.status === 'planned').map((night: Json) => night.recipeId)
  if (new Set(planned).size !== planned.length) return false
  return value.nights.every((night: unknown, index) => {
    if (!isRecord(night) || night.day !== index || typeof night.pinned !== 'boolean') return false
    if (night.status === 'planned') {
      return (
        isString(night.recipeId) && (recipeIds.has(night.recipeId) || /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(night.recipeId)) &&
        Number.isInteger(night.servings) && (night.servings as number) >= 1 && (night.servings as number) <= 12
      )
    }
    return night.status === 'open' || night.status === 'skipped'
  })
}

function isRecordOf(value: unknown, guard: (item: Json) => boolean): boolean {
  return isRecord(value) && Object.entries(value).every(([key, item]) => isWellFormed(key) && isRecord(item) && guard(item))
}

/** Strictly validate a stored blob; returns the persisted subset or null. */
export function parsePersisted(raw: string): PersistedState | null {
  let data: unknown
  try {
    data = JSON.parse(raw)
  } catch {
    return null
  }
  if (!isRecord(data) || data.version !== STORAGE_VERSION || !isRecord(data.state)) return null
  const s = data.state
  if (!Array.isArray(s.recipes) || !s.recipes.every(isRecipe)) return null
  if (!Array.isArray(s.drafts) || !s.drafts.every(isDraft)) return null
  const recipeIds = new Set((s.recipes as Json[]).map((recipe) => recipe.id as string))
  if (recipeIds.size !== s.recipes.length) return null
  if (!isPlan(s.plan, recipeIds)) return null
  if (!isRecord(s.progress)) return null
  const shopping = isRecordOf(s.progress.shoppingChecked, (item) =>
    (item.checkedQuantity === null || isNum(item.checkedQuantity)) &&
    isString(item.name) && isString(item.unit) && (AISLES as readonly unknown[]).includes(item.aisle))
  const prep = isRecordOf(s.progress.prepDone, (item) =>
    isNum(item.doneFactor) && isString(item.text) && isNum(item.minutes) && isString(item.mealTitle))
  if (!shopping || !prep) return null
  if (!isStringArray(s.pantryLow)) return null
  const rules = s.houseRules
  if (!isRecord(rules)) return null
  const { targetDinners, prepDay } = rules
  if (!Number.isInteger(targetDinners) || (targetDinners as number) < 1 || (targetDinners as number) > 7) return null
  if (!Number.isInteger(prepDay) || (prepDay as number) < 0 || (prepDay as number) > 6) return null
  // Return ONLY the validated fields: a stored blob must never smuggle in transient state (undo, proposal, status…).
  const plan = s.plan as Json
  return {
    recipes: s.recipes as unknown as PersistedState['recipes'],
    drafts: s.drafts as unknown as PersistedState['drafts'],
    plan: {
      revision: plan.revision as number,
      nights: (plan.nights as Json[]).map((night) =>
        night.status === 'planned'
          ? { day: night.day, status: 'planned', recipeId: night.recipeId, servings: night.servings, pinned: night.pinned }
          : { day: night.day, status: night.status, pinned: night.pinned },
      ) as unknown as PersistedState['plan']['nights'],
    },
    progress: {
      shoppingChecked: s.progress.shoppingChecked as PersistedState['progress']['shoppingChecked'],
      prepDone: s.progress.prepDone as PersistedState['progress']['prepDone'],
    },
    pantryLow: s.pantryLow,
    houseRules: { targetDinners: targetDinners as number, prepDay: prepDay as PersistedState['houseRules']['prepDay'] },
  }
}

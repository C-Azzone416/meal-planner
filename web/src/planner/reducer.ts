/** The planner reducer. Pure: random choices arrive as seeds drawn by the store. */
import { deriveShopping, derivePrep } from './derive'
import { normalizeName, uniqueId } from './keys'
import {
  assignRecipe,
  cancelProposal,
  fillWeek,
  keepProposal,
  moveNight,
  mulberry32,
  proposeSurprise,
  setOpen,
  setServings,
  setSkipped,
  togglePin,
  undoLast,
  withStatus,
} from './plan'
import { createSeedState } from './seed'
import {
  DRAFT_GUESS_KEYS,
  type DayIndex,
  type Draft,
  type DraftGuessKey,
  type HouseRules,
  type PlannerState,
  type Recipe,
} from './types'

export type DraftPatch = Partial<
  Omit<Draft, 'id' | 'revision' | 'confirmed' | 'warnings'>
>

export type PlannerAction =
  | { type: 'recipe/add'; recipe: Recipe }
  | { type: 'recipe/update'; recipe: Recipe }
  | { type: 'draft/confirm'; id: string; key: DraftGuessKey; value: boolean }
  | { type: 'draft/update'; id: string; patch: DraftPatch }
  | { type: 'draft/approve'; id: string; recipeId: string }
  | { type: 'draft/discard'; id: string }
  | { type: 'plan/fill'; seed: number }
  | { type: 'plan/propose'; day: DayIndex; seed: number }
  | { type: 'plan/keep' }
  | { type: 'plan/cancel' }
  | { type: 'plan/move'; from: DayIndex; to: DayIndex }
  | { type: 'plan/skip'; day: DayIndex }
  | { type: 'plan/open'; day: DayIndex }
  | { type: 'plan/pin'; day: DayIndex }
  | { type: 'plan/servings'; day: DayIndex; servings: number }
  | { type: 'plan/assign'; day: DayIndex; recipeId: string; servings?: number }
  | { type: 'shopping/toggle'; key: string }
  | { type: 'prep/toggle'; key: string }
  | { type: 'pantry/toggleLow'; name: string }
  | { type: 'dismiss'; kind: 'shopping' | 'prep'; key: string }
  | { type: 'rules/set'; patch: Partial<HouseRules> }
  | { type: 'undo' }
  | { type: 'undo/dismiss' }
  | { type: 'status/clear' }
  | { type: 'reset' }

/** Which guess each draft field invalidates when edited. */
const FIELD_GUESSES: Partial<Record<keyof DraftPatch, DraftGuessKey[]>> = {
  activeMinutes: ['activeMinutes'],
  totalMinutes: ['totalMinutes'],
  attention: ['attention'],
  prepAhead: ['prepAhead', 'storage'],
}

export function canApproveDraft(draft: Draft): boolean {
  return DRAFT_GUESS_KEYS.every((key) => draft.confirmed[key])
}

function draftToRecipe(draft: Draft, id: string): Recipe {
  return {
    id,
    title: draft.title,
    icon: draft.icon,
    activeMinutes: draft.activeMinutes,
    totalMinutes: draft.totalMinutes,
    attention: draft.attention,
    tags: draft.tags,
    defaultServings: draft.defaultServings,
    ingredients: draft.ingredients,
    prepAhead: draft.prepAhead,
    prepTonight: draft.prepTonight,
    cookTonight: draft.cookTonight,
  }
}

function without<T>(record: Record<string, T>, key: string): Record<string, T> {
  const { [key]: removed, ...rest } = record
  void removed
  return rest
}

function isPlanned(state: PlannerState, recipeId: string): boolean {
  return state.plan.nights.some((night) => night.status === 'planned' && night.recipeId === recipeId)
}

export function reducer(state: PlannerState, action: PlannerAction): PlannerState {
  switch (action.type) {
    case 'recipe/add': {
      const id = uniqueId(action.recipe.id, state.recipes.map((recipe) => recipe.id))
      return { ...state, recipes: [...state.recipes, { ...action.recipe, id }] }
    }
    case 'recipe/update': {
      if (!state.recipes.some((recipe) => recipe.id === action.recipe.id)) return state
      const next = {
        ...state,
        recipes: state.recipes.map((recipe) => (recipe.id === action.recipe.id ? action.recipe : recipe)),
      }
      return isPlanned(state, action.recipe.id)
        ? withStatus(next, 'info', 'This week’s shopping and prep were updated.')
        : next
    }

    case 'draft/confirm':
      return {
        ...state,
        drafts: state.drafts.map((draft) =>
          draft.id === action.id ? { ...draft, confirmed: { ...draft.confirmed, [action.key]: action.value } } : draft,
        ),
      }
    case 'draft/update':
      return {
        ...state,
        drafts: state.drafts.map((draft) => {
          if (draft.id !== action.id) return draft
          const confirmed = { ...draft.confirmed }
          for (const field of Object.keys(action.patch) as (keyof DraftPatch)[]) {
            for (const guess of FIELD_GUESSES[field] ?? []) confirmed[guess] = false
          }
          return { ...draft, ...action.patch, confirmed, revision: draft.revision + 1 }
        }),
      }
    case 'draft/approve': {
      const draft = state.drafts.find((candidate) => candidate.id === action.id)
      if (!draft) return state
      if (!canApproveDraft(draft)) return withStatus(state, 'warning', 'Confirm every guess before approving this draft.')
      const id = uniqueId(action.recipeId, state.recipes.map((recipe) => recipe.id))
      return withStatus(
        {
          ...state,
          recipes: [...state.recipes, draftToRecipe(draft, id)],
          drafts: state.drafts.filter((candidate) => candidate.id !== action.id),
        },
        'success',
        `Saved “${draft.title}” to your recipes.`,
      )
    }
    case 'draft/discard':
      return { ...state, drafts: state.drafts.filter((draft) => draft.id !== action.id) }

    case 'plan/fill':
      return fillWeek(state, mulberry32(action.seed))
    case 'plan/propose':
      return proposeSurprise(state, action.day, mulberry32(action.seed))
    case 'plan/keep':
      return keepProposal(state)
    case 'plan/cancel':
      return cancelProposal(state)
    case 'plan/move':
      return moveNight(state, action.from, action.to)
    case 'plan/skip':
      return setSkipped(state, action.day)
    case 'plan/open':
      return setOpen(state, action.day)
    case 'plan/pin':
      return togglePin(state, action.day)
    case 'plan/servings':
      return setServings(state, action.day, action.servings)
    case 'plan/assign':
      return assignRecipe(state, action.day, action.recipeId, action.servings)

    case 'shopping/toggle': {
      const { shoppingChecked } = state.progress
      const row = deriveShopping(state).rows.find((candidate) => candidate.key === action.key)
      if (!row) {
        return action.key in shoppingChecked
          ? { ...state, progress: { ...state.progress, shoppingChecked: without(shoppingChecked, action.key) } }
          : state
      }
      const next = row.checked
        ? without(shoppingChecked, row.key)
        : { ...shoppingChecked, [row.key]: { checkedQuantity: row.quantity, name: row.name, unit: row.unit, aisle: row.aisle } }
      return { ...state, progress: { ...state.progress, shoppingChecked: next } }
    }
    case 'prep/toggle': {
      const { prepDone } = state.progress
      const task = derivePrep(state).tasks.find((candidate) => candidate.key === action.key)
      if (!task) {
        return action.key in prepDone
          ? { ...state, progress: { ...state.progress, prepDone: without(prepDone, action.key) } }
          : state
      }
      const next = task.status === 'done'
        ? without(prepDone, task.key)
        : {
            ...prepDone,
            [task.key]: { doneFactor: task.factor, text: task.text, minutes: task.minutes, mealTitle: task.meal.title },
          }
      return { ...state, progress: { ...state.progress, prepDone: next } }
    }
    case 'pantry/toggleLow': {
      const name = normalizeName(action.name)
      return {
        ...state,
        pantryLow: state.pantryLow.includes(name)
          ? state.pantryLow.filter((existing) => existing !== name)
          : [...state.pantryLow, name],
      }
    }
    case 'dismiss': {
      if (action.kind === 'shopping') {
        const live = deriveShopping(state).rows.some((row) => row.key === action.key)
        if (live || !(action.key in state.progress.shoppingChecked)) return state
        return { ...state, progress: { ...state.progress, shoppingChecked: without(state.progress.shoppingChecked, action.key) } }
      }
      const live = derivePrep(state).tasks.some((task) => task.key === action.key)
      if (live || !(action.key in state.progress.prepDone)) return state
      return { ...state, progress: { ...state.progress, prepDone: without(state.progress.prepDone, action.key) } }
    }

    case 'rules/set': {
      const { targetDinners, prepDay } = { ...state.houseRules, ...action.patch }
      return {
        ...state,
        houseRules: {
          targetDinners: Math.min(7, Math.max(1, Math.round(targetDinners))),
          prepDay: Math.min(6, Math.max(0, Math.round(prepDay))) as DayIndex,
        },
      }
    }

    case 'undo':
      return undoLast(state)
    case 'undo/dismiss':
      return state.undo ? { ...state, undo: null } : state
    case 'status/clear':
      return state.status ? { ...state, status: null } : state
    case 'reset':
      return createSeedState()
  }
}

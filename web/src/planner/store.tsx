/**
 * Client-side planner store: React context + reducer persisted to localStorage.
 *
 * Pages read and write ONLY through `usePlanner()`. This is the seam a future
 * recipe/plan API will replace (docs/ux-decisions.md §0). See ./README.md.
 */
import {
  createContext,
  type ReactNode,
  useContext,
  useEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
} from 'react'
import {
  deriveNightWarnings,
  derivePantry,
  derivePrep,
  deriveShopping,
  type PantryItem,
  type PlanSummary,
  type PrepPlan,
  planSummary,
  type ShoppingList,
} from './derive'
import { uniqueId } from './keys'
import { defaultStorage, loadState, saveState, type StorageLike } from './persist'
import { seedFrom } from './plan'
import { type DraftPatch, type PlannerAction, reducer } from './reducer'
import {
  type DayIndex,
  type Draft,
  type DraftGuessKey,
  type HouseRules,
  type NewRecipe,
  type PlannerState,
  type Recipe,
  type Rng,
  type StatusMessage,
  type SurpriseProposal,
} from './types'

/** Every way a page can change planner data. All are synchronous and safe to call from event handlers. */
export interface PlannerActions {
  // — Recipes —
  /**
   * Save a person-authored recipe directly (not a draft). Returns the new recipe's stable id, which is already
   * unique even across several calls in the same tick. Returns '' (and sets `status`) only if the recipe was
   * rejected because `defaultServings` is not a positive number.
   */
  addRecipe(recipe: NewRecipe): string
  /** Replace a saved recipe by id. If it is on this week's plan, `status` says shopping/prep were updated. */
  updateRecipe(recipe: Recipe): void

  // — Drafts (agent-created; separate from recipes until approved) —
  /** Turn a draft into a saved recipe. Returns the new recipe id, or null if some guesses are still unconfirmed. */
  approveDraft(draftId: string): string | null
  discardDraft(draftId: string): void
  setDraftConfirmed(draftId: string, key: DraftGuessKey, confirmed: boolean): void
  /** Edit draft fields; bumps its revision and clears the confirmations those fields affect. */
  updateDraft(draftId: string, patch: DraftPatch): void

  // — Plan —
  /** Plan my week: fill open nights up to the dinner target. Never touches planned/skipped nights. */
  fillWeek(rng?: Rng): void
  /** Surprise me: propose (but do not apply) a random alternative for `day`. Refused on pinned/skipped nights. */
  proposeSurprise(day: DayIndex, rng?: Rng): void
  /** "Keep it": apply the pending proposal. */
  keepProposal(): void
  /** "Cancel": drop the pending proposal. ("Try another" = call proposeSurprise again.) */
  cancelProposal(): void
  /** Move a planned dinner; swaps if the target night is planned. */
  moveNight(from: DayIndex, to: DayIndex): void
  setSkipped(day: DayIndex): void
  /** Clear a night (removes a planned meal, or un-skips). */
  setOpen(day: DayIndex): void
  togglePin(day: DayIndex): void
  /** 1–12; clamped. */
  setServings(day: DayIndex, servings: number): void
  /** Put a specific recipe on a night ("Add to week"). Works on open or unpinned planned nights. */
  assignRecipe(day: DayIndex, recipeId: string, servings?: number): void
  setHouseRules(patch: Partial<HouseRules>): void

  // — Progress —
  /** `key` is `ShoppingRow.key`. Also un-checks a "no longer needed" row. */
  toggleShoppingChecked(key: string): void
  /** `key` is `PrepTask.key`. A top-up task counts as done, so toggling it clears it like any done task. */
  togglePrepDone(key: string): void
  /** Confirm a 'topup' prep task is topped up (keeps it done and records the new serving factor). */
  confirmPrepTopUp(key: string): void
  /** `name` is `PantryItem.key` or any spelling of the ingredient name. */
  togglePantryLow(name: string): void
  /** Remove a struck-through "No longer needed" shopping row, or a "Done, no longer planned" prep task. */
  dismissNoLongerNeeded(kind: 'shopping' | 'prep', key: string): void

  // — Undo / status / reset —
  /** Restore plan + progress from before the last plan-changing action. One level. */
  undo(): void
  /** Hide the undo affordance (e.g. after its 10 s window) without restoring anything. */
  dismissUndo(): void
  clearStatus(): void
  dismissResetNotice(): void
  /** Discard everything and reseed. Pages must confirm first. */
  resetPreviewData(): void
}

export interface PlannerApi extends PlannerActions {
  recipes: Recipe[]
  drafts: Draft[]
  plan: PlannerState['plan']
  houseRules: HouseRules
  progress: PlannerState['progress']
  /** Pending Surprise me suggestion, if any. */
  proposal: SurpriseProposal | null
  /** Present after a plan-changing action until the next change/undo. `seq` changes on every new change. */
  undoInfo: { message: string; seq: number } | null
  /** Latest refusal/info/success message from an action (render with role="status"). */
  status: StatusMessage | null
  /** Set when stored data was unreadable and the preview was reseeded. */
  resetNotice: string | null

  shopping: ShoppingList
  prep: PrepPlan
  pantry: PantryItem[]
  summary: PlanSummary
  /** Prep-timing warnings per night, e.g. "Marinade keeps 1 day — prep it Wednesday instead". */
  nightWarnings: Record<DayIndex, string[]>

  getRecipe(id: string | undefined): Recipe | undefined
  getDraft(id: string | undefined): Draft | undefined
  /** Whole raw state, for tests and advanced use. Prefer the fields above. */
  state: PlannerState
}

const PlannerContext = createContext<PlannerApi | null>(null)

export interface PlannerProviderProps {
  children: ReactNode
  /** Defaults to window.localStorage (null if unavailable). Pass `null` to disable persistence. */
  storage?: StorageLike | null
  /** Default random source for fillWeek/proposeSurprise. Defaults to Math.random. */
  random?: Rng
}

export function PlannerProvider({ children, storage, random = Math.random }: PlannerProviderProps) {
  const [store] = useState<StorageLike | null>(() => (storage === undefined ? defaultStorage() : storage))
  const [boot] = useState(() => loadState(store))
  const [state, dispatch] = useReducer(reducer, boot.state)
  const [resetNotice, setResetNotice] = useState<string | null>(boot.resetNotice)

  // Latest state for actions that return a value (ids); updated after each commit.
  const latest = useRef(state)
  useEffect(() => {
    latest.current = state
  }, [state])

  const { recipes, drafts, plan, progress, pantryLow, houseRules } = state
  useEffect(() => {
    saveState(store, { recipes, drafts, plan, progress, pantryLow, houseRules })
  }, [store, recipes, drafts, plan, progress, pantryLow, houseRules])

  const randomRef = useRef(random)
  useEffect(() => {
    randomRef.current = random
  }, [random])

  const actions = useMemo<PlannerActions>(() => {
    // Apply each action to `latest` immediately (the reducer is pure) so that actions returning ids
    // see the effect of earlier calls made in the same tick; then hand it to React.
    const send = (action: PlannerAction) => {
      latest.current = reducer(latest.current, action)
      dispatch(action)
    }
    return {
      addRecipe(recipe) {
        const id = uniqueId(recipe.title, latest.current.recipes.map((existing) => existing.id))
        send({ type: 'recipe/add', recipe: { ...recipe, id } })
        return latest.current.recipes.some((existing) => existing.id === id) ? id : ''
      },
      updateRecipe: (recipe) => send({ type: 'recipe/update', recipe }),
      approveDraft(draftId) {
        const draft = latest.current.drafts.find((candidate) => candidate.id === draftId)
        const id = draft ? uniqueId(draft.title, latest.current.recipes.map((recipe) => recipe.id)) : null
        send({ type: 'draft/approve', id: draftId, recipeId: id ?? '' })
        return id !== null && latest.current.recipes.some((recipe) => recipe.id === id) ? id : null
      },
      discardDraft: (id) => send({ type: 'draft/discard', id }),
      setDraftConfirmed: (id, key, value) => send({ type: 'draft/confirm', id, key, value }),
      updateDraft: (id, patch) => send({ type: 'draft/update', id, patch }),

      fillWeek: (rng) => send({ type: 'plan/fill', seed: seedFrom(rng ?? randomRef.current) }),
      proposeSurprise: (day, rng) => send({ type: 'plan/propose', day, seed: seedFrom(rng ?? randomRef.current) }),
      keepProposal: () => send({ type: 'plan/keep' }),
      cancelProposal: () => send({ type: 'plan/cancel' }),
      moveNight: (from, to) => send({ type: 'plan/move', from, to }),
      setSkipped: (day) => send({ type: 'plan/skip', day }),
      setOpen: (day) => send({ type: 'plan/open', day }),
      togglePin: (day) => send({ type: 'plan/pin', day }),
      setServings: (day, servings) => send({ type: 'plan/servings', day, servings }),
      assignRecipe: (day, recipeId, servings) => send({ type: 'plan/assign', day, recipeId, servings }),
      setHouseRules: (patch) => send({ type: 'rules/set', patch }),

      toggleShoppingChecked: (key) => send({ type: 'shopping/toggle', key }),
      togglePrepDone: (key) => send({ type: 'prep/toggle', key }),
      confirmPrepTopUp: (key) => send({ type: 'prep/topup', key }),
      togglePantryLow: (name) => send({ type: 'pantry/toggleLow', name }),
      dismissNoLongerNeeded: (kind, key) => send({ type: 'dismiss', kind, key }),

      undo: () => send({ type: 'undo' }),
      dismissUndo: () => send({ type: 'undo/dismiss' }),
      clearStatus: () => send({ type: 'status/clear' }),
      dismissResetNotice: () => setResetNotice(null),
      resetPreviewData() {
        send({ type: 'reset' })
        setResetNotice(null)
      },
    }
  }, [])

  const shopping = useMemo(() => deriveShopping(state), [state])
  const prep = useMemo(() => derivePrep(state), [state])
  const pantry = useMemo(() => derivePantry(state), [state])
  const summary = useMemo(() => planSummary(state), [state])
  const nightWarnings = useMemo(() => deriveNightWarnings(state), [state])

  const value = useMemo<PlannerApi>(
    () => ({
      ...actions,
      recipes: state.recipes,
      drafts: state.drafts,
      plan: state.plan,
      houseRules: state.houseRules,
      progress: state.progress,
      proposal: state.proposal,
      undoInfo: state.undo ? { message: state.undo.message, seq: state.undo.seq } : null,
      status: state.status,
      resetNotice,
      shopping,
      prep,
      pantry,
      summary,
      nightWarnings,
      getRecipe: (id) => state.recipes.find((recipe) => recipe.id === id),
      getDraft: (id) => state.drafts.find((draft) => draft.id === id),
      state,
    }),
    [actions, state, resetNotice, shopping, prep, pantry, summary, nightWarnings],
  )

  return <PlannerContext.Provider value={value}>{children}</PlannerContext.Provider>
}

export function usePlanner(): PlannerApi {
  const context = useContext(PlannerContext)
  if (!context) throw new Error('usePlanner must be used inside <PlannerProvider>')
  return context
}

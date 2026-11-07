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
import { type DraftPatch, canApproveDraft, reducer } from './reducer'
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
  /** Save a person-authored recipe directly (not a draft). Returns the new recipe's stable id. */
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
  /** `key` is `PrepTask.key`. Marking a "top-up" task done confirms the top-up. */
  togglePrepDone(key: string): void
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

  const actions = useMemo<PlannerActions>(
    () => ({
      addRecipe(recipe) {
        const id = uniqueId(recipe.title, latest.current.recipes.map((existing) => existing.id))
        dispatch({ type: 'recipe/add', recipe: { ...recipe, id } })
        return id
      },
      updateRecipe: (recipe) => dispatch({ type: 'recipe/update', recipe }),
      approveDraft(draftId) {
        const draft = latest.current.drafts.find((candidate) => candidate.id === draftId)
        const id = draft ? uniqueId(draft.title, latest.current.recipes.map((recipe) => recipe.id)) : null
        dispatch({ type: 'draft/approve', id: draftId, recipeId: id ?? '' })
        return draft && canApproveDraft(draft) ? id : null
      },
      discardDraft: (id) => dispatch({ type: 'draft/discard', id }),
      setDraftConfirmed: (id, key, value) => dispatch({ type: 'draft/confirm', id, key, value }),
      updateDraft: (id, patch) => dispatch({ type: 'draft/update', id, patch }),

      fillWeek: (rng) => dispatch({ type: 'plan/fill', seed: seedFrom(rng ?? randomRef.current) }),
      proposeSurprise: (day, rng) => dispatch({ type: 'plan/propose', day, seed: seedFrom(rng ?? randomRef.current) }),
      keepProposal: () => dispatch({ type: 'plan/keep' }),
      cancelProposal: () => dispatch({ type: 'plan/cancel' }),
      moveNight: (from, to) => dispatch({ type: 'plan/move', from, to }),
      setSkipped: (day) => dispatch({ type: 'plan/skip', day }),
      setOpen: (day) => dispatch({ type: 'plan/open', day }),
      togglePin: (day) => dispatch({ type: 'plan/pin', day }),
      setServings: (day, servings) => dispatch({ type: 'plan/servings', day, servings }),
      assignRecipe: (day, recipeId, servings) => dispatch({ type: 'plan/assign', day, recipeId, servings }),
      setHouseRules: (patch) => dispatch({ type: 'rules/set', patch }),

      toggleShoppingChecked: (key) => dispatch({ type: 'shopping/toggle', key }),
      togglePrepDone: (key) => dispatch({ type: 'prep/toggle', key }),
      togglePantryLow: (name) => dispatch({ type: 'pantry/toggleLow', name }),
      dismissNoLongerNeeded: (kind, key) => dispatch({ type: 'dismiss', kind, key }),

      undo: () => dispatch({ type: 'undo' }),
      dismissUndo: () => dispatch({ type: 'undo/dismiss' }),
      clearStatus: () => dispatch({ type: 'status/clear' }),
      dismissResetNotice: () => setResetNotice(null),
      resetPreviewData() {
        dispatch({ type: 'reset' })
        setResetNotice(null)
      },
    }),
    [],
  )

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

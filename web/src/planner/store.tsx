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
  nightWarningsFromPrep,
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
import { deepFreeze } from './immutable'
import { accountStorageKey, defaultStorage, LEGACY_NOTICE, loadState, saveState, STORAGE_KEY, type StorageLike } from './persist'
import { seedFrom } from './plan'
import { type DraftPatch, type PlannerAction, reducer } from './reducer'
import {
  type DayIndex,
  type DeepReadonly,
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
  addRecipe(recipe: DeepReadonly<NewRecipe>): string
  /** Replace a saved recipe by id. If it is on this week's plan, `status` says shopping/prep were updated. */
  updateRecipe(recipe: DeepReadonly<Recipe>): void

  // — Drafts (agent-created; separate from recipes until approved) —
  /** Turn a draft into a saved recipe. Returns the new recipe id, or null if some guesses are still unconfirmed. */
  approveDraft(draftId: string): string | null
  discardDraft(draftId: string): void
  setDraftConfirmed(draftId: string, key: DraftGuessKey, confirmed: boolean): void
  /** Edit draft fields; bumps its revision and clears the confirmations those fields affect. */
  updateDraft(draftId: string, patch: DeepReadonly<DraftPatch>): void

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
  /** Retry a failed browser save without changing planner data. */
  retryPersistence(): void
}

/**
 * Everything a page can read. Treat it as read-only: it is the store's own state, shared by all consumers,
 * so change it only through the actions.
 */
export interface PlannerData {
  /** Transient Cook checklist lifetime; replaced on reset and provider/account remount. */
  readonly cookProgressScope: object
  readonly recipes: DeepReadonly<Recipe[]>
  readonly drafts: DeepReadonly<Draft[]>
  readonly plan: DeepReadonly<PlannerState['plan']>
  readonly houseRules: DeepReadonly<HouseRules>
  readonly progress: DeepReadonly<PlannerState['progress']>
  /** Pending Surprise me suggestion, if any. */
  readonly proposal: DeepReadonly<SurpriseProposal> | null
  /** Present after a plan-changing action until the next change/undo. `seq` changes on every new change. */
  readonly undoInfo: { readonly message: string; readonly seq: number } | null
  /** Latest refusal/info/success message from an action (render with role="status"). */
  readonly status: DeepReadonly<StatusMessage> | null
  /** Set when stored data was unreadable and the preview was reseeded. */
  readonly resetNotice: string | null
  readonly persistenceStatus: 'saved' | 'unsaved'
  readonly persistenceNotice: string | null
  readonly legacyNotice: string | null

  readonly shopping: DeepReadonly<ShoppingList>
  readonly prep: DeepReadonly<PrepPlan>
  readonly pantry: DeepReadonly<PantryItem[]>
  readonly summary: DeepReadonly<PlanSummary>
  /** Prep-timing warnings per night, e.g. "Marinade keeps 1 day — prep it Wednesday instead". */
  readonly nightWarnings: DeepReadonly<Record<DayIndex, readonly string[]>>

  getRecipe(id: string | undefined): DeepReadonly<Recipe> | undefined
  getDraft(id: string | undefined): DeepReadonly<Draft> | undefined
  /** Whole raw state, for tests and advanced use. Prefer the fields above. */
  readonly state: DeepReadonly<PlannerState>
}

export type PlannerApi = PlannerData & PlannerActions

const PlannerDataContext = createContext<PlannerData | null>(null)
const PlannerActionsContext = createContext<PlannerActions | null>(null)
const ApiContext = createContext<PlannerApi | null>(null)

export interface PlannerProviderProps {
  children: ReactNode
  /** Defaults to window.localStorage (null if unavailable). Pass `null` to disable persistence. */
  storage?: StorageLike | null
  /** Required in the authenticated app. Remounts immediately when the account changes. */
  accountId?: string
  /** Default random source for fillWeek/proposeSurprise. Defaults to Math.random. */
  random?: Rng
  /** Hosted records are a read-through overlay, never written into browser recipe storage. */
  externalRecipes?: Recipe[]
}

export function PlannerProvider(props: PlannerProviderProps) {
  return <PlannerStore key={props.accountId ?? 'test-preview'} {...props} />
}

const NO_EXTERNAL_RECIPES: Recipe[] = []

function PlannerStore({ children, storage, accountId, random = Math.random, externalRecipes = NO_EXTERNAL_RECIPES }: PlannerProviderProps) {
  const [store] = useState<StorageLike | null>(() => (storage === undefined ? defaultStorage() : storage))
  const storageKey = accountId === undefined ? STORAGE_KEY : accountStorageKey(accountId)
  const [boot] = useState(() => loadState(store, storageKey))
  const [legacyNotice] = useState(() => {
    try { return accountId !== undefined && store?.getItem(STORAGE_KEY) !== null && store ? LEGACY_NOTICE : null } catch { return null }
  })
  const [persistenceStatus, setPersistenceStatus] = useState<'saved' | 'unsaved'>('unsaved')
  const [saveAttempt, setSaveAttempt] = useState(0)
  const [readBlocked, setReadBlocked] = useState(boot.readFailed ?? false)
  const [localState, dispatch] = useReducer((local: PlannerState, action: PlannerAction) => {
    if (!externalRecipes.length) return reducer(local, action)
    const remoteIds = new Set(externalRecipes.map((recipe) => recipe.id))
    const next = reducer({ ...local, recipes: [...local.recipes.filter((recipe) => !remoteIds.has(recipe.id)), ...externalRecipes] }, action)
    const localRecipes = next.recipes.filter((recipe) => !remoteIds.has(recipe.id))
    // Preserve legacy browser records even if an old custom id happens to equal a hosted UUID.
    localRecipes.push(...local.recipes.filter((recipe) => remoteIds.has(recipe.id)))
    return { ...next, recipes: localRecipes }

  }, boot.state)
  const combinedRecipes = useMemo(() => externalRecipes.length ? [...localState.recipes.filter((recipe) => !externalRecipes.some((remote) => remote.id === recipe.id)), ...externalRecipes] : localState.recipes, [localState.recipes, externalRecipes])
  const state = useMemo(() => ({ ...localState, recipes: combinedRecipes }), [localState, combinedRecipes])
  const [resetNotice, setResetNotice] = useState<string | null>(boot.resetNotice)
  const [cookProgressScope, setCookProgressScope] = useState<object>(() => ({}))

  // Latest state for actions that return a value (ids); updated after each commit.
  const latest = useRef(state)
  useEffect(() => {
    latest.current = state
  }, [state])

  const { recipes, drafts, plan, progress, pantryLow, houseRules } = state
  useEffect(() => {
    // A failed initial read might hide existing data. Never overwrite that unknown snapshot.
    let blocked = readBlocked
    if (blocked) {
      try {
        if (store?.getItem(storageKey) === null) blocked = false
      } catch { /* keep blocked */ }
    }
    const saved = !blocked && saveState(store, { recipes: localState.recipes, drafts, plan, progress, pantryLow, houseRules }, storageKey)
    // Report the synchronous external storage result, rather than optimistic save claims.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setReadBlocked(blocked)
    setPersistenceStatus(saved ? 'saved' : 'unsaved')
  }, [store, storageKey, localState.recipes, drafts, plan, progress, pantryLow, houseRules, saveAttempt, readBlocked])
  const persistenceNotice = persistenceStatus === 'saved' ? null : readBlocked
    ? 'Browser data could not be read. Changes are not saved. Enable browser storage and reload to recover existing data; unsaved changes will be lost.'
    : 'Changes are not saved. Browser storage may be disabled or full. Keep this page open and retry after freeing space or enabling storage.'

  const randomRef = useRef(random)
  useEffect(() => {
    randomRef.current = random
  }, [random])

  const actions = useMemo<PlannerActions>(() => {
    // Apply each action to `latest` immediately (the reducer is pure) so that actions returning ids
    // see the effect of earlier calls made in the same tick; then hand it to React.
    const send = (action: DeepReadonly<PlannerAction>) => {
      // Snapshot first: whatever the caller does to its object afterwards must not change what commits,
      // neither in the immediate reducer call nor when React replays the action.
      // The copy is owned by the reducer; no caller-owned readonly references escape.
      const snapshot = structuredClone(action) as PlannerAction
      latest.current = reducer(latest.current, snapshot)
      dispatch(snapshot)
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
      retryPersistence: () => setSaveAttempt((attempt) => attempt + 1),
      resetPreviewData() {
        setCookProgressScope({})
        send({ type: 'reset' })
        setResetNotice(null)
      },
    }
  }, [])

  // Each derivation is memoised against only the slices it reads, so e.g. ticking a shopping item does not redo prep.
  const { shoppingChecked, prepDone } = progress
  const { targetDinners, prepDay } = houseRules
  const shopping = useMemo(
    () => deriveShopping({ recipes, plan, pantryLow, progress: { shoppingChecked } }),
    [recipes, plan, pantryLow, shoppingChecked],
  )
  const prep = useMemo(
    () => derivePrep({ recipes, plan, houseRules: { prepDay }, progress: { prepDone } }),
    [recipes, plan, prepDay, prepDone],
  )
  const pantry = useMemo(() => derivePantry({ recipes, plan, pantryLow }), [recipes, plan, pantryLow])
  const summary = useMemo(() => planSummary({ plan, houseRules: { targetDinners } }), [plan, targetDinners])
  const nightWarnings = useMemo(() => nightWarningsFromPrep(prep), [prep])

  const undoInfo = useMemo(
    () => (state.undo ? { message: state.undo.message, seq: state.undo.seq } : null),
    [state.undo],
  )
  const data = useMemo<PlannerData>(
    () => deepFreeze({
      cookProgressScope,
      recipes,
      drafts,
      plan,
      houseRules,
      progress,
      proposal: state.proposal,
      undoInfo,
      status: state.status,
      resetNotice,
      persistenceStatus,
      persistenceNotice,
      legacyNotice,
      shopping,
      prep,
      pantry,
      summary,
      nightWarnings,
      getRecipe: (id) => recipes.find((recipe) => recipe.id === id),
      getDraft: (id) => drafts.find((draft) => draft.id === id),
      state,
    }),
    [cookProgressScope, recipes, drafts, plan, houseRules, progress, state, undoInfo, resetNotice, persistenceStatus, persistenceNotice, legacyNotice, shopping, prep, pantry, summary, nightWarnings],
  )
  const api = useMemo<PlannerApi>(() => ({ ...actions, ...data }), [actions, data])

  return (
    <PlannerActionsContext.Provider value={actions}>
      <PlannerDataContext.Provider value={data}>
        <ApiContext.Provider value={api}>{children}</ApiContext.Provider>
      </PlannerDataContext.Provider>
    </PlannerActionsContext.Provider>
  )
}

/** Everything: data + actions. Re-renders when any planner data changes. */
export function usePlanner(): PlannerApi {
  const context = useContext(ApiContext)
  if (!context) throw new Error('usePlanner must be used inside <PlannerProvider>')
  return context
}

/** Actions only. The object is stable for the life of the provider, so consumers never re-render because of data changes. */
export function usePlannerActions(): PlannerActions {
  const context = useContext(PlannerActionsContext)
  if (!context) throw new Error('usePlannerActions must be used inside <PlannerProvider>')
  return context
}

/** Data only (no actions). */
export function usePlannerData(): PlannerData {
  const context = useContext(PlannerDataContext)
  if (!context) throw new Error('usePlannerData must be used inside <PlannerProvider>')
  return context
}

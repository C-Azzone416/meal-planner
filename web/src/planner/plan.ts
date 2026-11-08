/**
 * Pure plan operations (docs/ux-decisions.md §2). Each takes a PlannerState and
 * returns a new one; randomness always comes from an injected `Rng`.
 *
 * Plan-changing operations record one level of undo (plan + progress before the
 * change) and clear any pending Surprise me proposal. Operations that cannot run
 * leave the plan alone and set a `status` explaining why.
 */
import { joinList } from './format'
import { DEFAULT_PLAN_DAYS } from './seed'
import {
  DAY_NAMES,
  type DayIndex,
  type DeepReadonly,
  type Night,
  type PlannerState,
  type Recipe,
  type Rng,
  type StatusKind,
  type WeekPlan,
} from './types'

/** Small, fast, seedable PRNG so reducers stay pure: the store draws one seed per action. */
export function mulberry32(seed: number): Rng {
  let a = seed >>> 0
  return () => {
    a = (a + 0x6d2b79f5) >>> 0
    let t = a
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

/** Turn any Rng into a 32-bit seed. */
export function seedFrom(rng: Rng): number {
  return Math.floor(rng() * 4294967296) >>> 0
}

export function shuffle<T>(items: readonly T[], rng: Rng): T[] {
  const copy = items.slice()
  for (let i = copy.length - 1; i > 0; i -= 1) {
    const j = Math.min(i, Math.floor(rng() * (i + 1)))
    ;[copy[i], copy[j]] = [copy[j], copy[i]]
  }
  return copy
}

export const MIN_SERVINGS = 1
export const MAX_SERVINGS = 12

export function clampServings(value: number): number {
  if (!Number.isFinite(value)) return MIN_SERVINGS
  return Math.min(MAX_SERVINGS, Math.max(MIN_SERVINGS, Math.round(value)))
}

export function findRecipe(state: PlannerState, id: string | undefined): Recipe | undefined {
  return id === undefined ? undefined : state.recipes.find((recipe) => recipe.id === id)
}

function dayName(day: DayIndex): string {
  return DAY_NAMES[day]
}

/** The night (other than `except`) a recipe is already planned on, if any. A recipe may be on only one night per week. */
export function dayWithRecipe(state: PlannerState, recipeId: string, except?: DayIndex): DayIndex | undefined {
  return state.plan.nights.find((night) => night.status === 'planned' && night.recipeId === recipeId && night.day !== except)?.day
}

function plannedRecipeIds(state: PlannerState): Set<string> {
  const ids = new Set<string>()
  for (const night of state.plan.nights) {
    if (night.status === 'planned' && night.recipeId) ids.add(night.recipeId)
  }
  return ids
}

export function withStatus(state: PlannerState, kind: StatusKind, text: string): PlannerState {
  const seq = state.seq + 1
  return { ...state, seq, status: { seq, kind, text } }
}

interface CommitOptions {
  /** Repeated changes with the same key keep the earliest undo snapshot. */
  coalesceKey?: string
  status?: { kind: StatusKind; text: string }
}

/** Apply new nights as one undoable plan change. */
function commit(state: PlannerState, nights: Night[], message: string, options: CommitOptions = {}): PlannerState {
  const seq = state.seq + 1
  // Repeated tweaks of the same thing share one undo step, but only while nothing else has happened since:
  // if progress changed in between (a ticked item, a finished prep task), the snapshot must be taken afresh.
  const coalesce =
    options.coalesceKey !== undefined &&
    state.undo?.coalesceKey === options.coalesceKey &&
    state.undo.progressAfter === state.progress
  const undo = coalesce && state.undo
    ? { ...state.undo, seq, message }
    : {
        seq,
        message,
        plan: state.plan,
        progress: state.progress,
        coalesceKey: options.coalesceKey,
        progressAfter: options.coalesceKey === undefined ? undefined : state.progress,
      }
  return {
    ...state,
    seq,
    plan: { nights, revision: state.plan.revision + 1 },
    proposal: null,
    undo,
    status: options.status ? { seq, ...options.status } : null,
  }
}

function replaceNight(nights: Night[], night: Night): Night[] {
  return nights.map((existing) => (existing.day === night.day ? night : existing))
}

function planned(day: DayIndex, recipe: Recipe, servings = recipe.defaultServings, pinned = false): Night {
  return { day, status: 'planned', recipeId: recipe.id, servings: clampServings(servings), pinned }
}

const OPEN = (day: DayIndex): Night => ({ day, status: 'open', pinned: false })

/**
 * The order in which open nights get dinners (Plan my week, Add to week): Mon/Wed/Thu first when open, then the
 * remaining open nights earliest-first with the prep day last. Only `open` nights are returned.
 */
export function fillOrder(plan: DeepReadonly<Pick<WeekPlan, 'nights'>>, prepDay: DayIndex): DayIndex[] {
  const open = plan.nights.filter((night) => night.status === 'open').map((night) => night.day)
  const preferred = DEFAULT_PLAN_DAYS.filter((day) => open.includes(day))
  const rest = open
    .filter((day) => !(DEFAULT_PLAN_DAYS as readonly DayIndex[]).includes(day))
    .sort((a, b) => Number(a === prepDay) - Number(b === prepDay) || a - b)
  return [...preferred, ...rest]
}

/**
 * Plan my week = fill only. Fills open nights (never planned, never skipped, never
 * pinned-planned) up to the dinner target with random saved recipes not already in the week.
 * Preferred nights are Mon/Wed/Thu; then the earliest open nights, prep day last.
 */
export function fillWeek(state: PlannerState, rng: Rng): PlannerState {
  const { targetDinners, prepDay } = state.houseRules
  if (state.recipes.length === 0) return withStatus(state, 'warning', 'You have no saved recipes yet.')
  const nights = state.plan.nights
  const plannedCount = nights.filter((night) => night.status === 'planned').length
  const need = targetDinners - plannedCount
  if (need <= 0) {
    return withStatus(state, 'info', `Your week already has ${targetDinners} dinners. Use Surprise me on a night to swap one.`)
  }

  const slots = fillOrder(state.plan, prepDay)
  if (slots.length === 0) {
    return withStatus(state, 'warning', 'There are no open nights to fill. Open a night first.')
  }

  const used = plannedRecipeIds(state)
  const pool = shuffle(state.recipes.filter((recipe) => !used.has(recipe.id)), rng)
  if (pool.length === 0) {
    return withStatus(state, 'warning', 'Every saved recipe is already in your week, so there is nothing new to plan.')
  }

  const take = Math.min(need, slots.length, pool.length)
  const chosen = slots.slice(0, take)
  let next = nights
  chosen.forEach((day, index) => {
    next = replaceNight(next, planned(day, pool[index]))
  })
  const days = [...chosen].sort((a, b) => a - b)
  const message = `Planned ${joinList(days.map(dayName))}`

  let status: CommitOptions['status']
  if (take < need) {
    const recipeLimited = pool.length < need && pool.length <= slots.length
    const why = recipeLimited
      ? `Only ${pool.length} saved recipe${pool.length === 1 ? ' was' : 's were'} available`
      : `Only ${slots.length} night${slots.length === 1 ? ' was' : 's were'} open`
    status = { kind: 'warning', text: `${why}, so we planned ${take} of the ${need} dinners needed.` }
  }
  return commit(state, next, message, { status })
}

/** Surprise me: propose (not apply) one random alternative for a night. */
export function proposeSurprise(state: PlannerState, day: DayIndex, rng: Rng): PlannerState {
  const night = state.plan.nights[day]
  const name = dayName(day)
  if (night.status === 'skipped') {
    return withStatus(state, 'warning', `${name} is skipped — make it open first to let Surprise me plan a dinner.`)
  }
  if (night.pinned) {
    return withStatus(state, 'warning', `${name} is pinned. Unpin it to let Surprise me swap it.`)
  }

  const inWeek = plannedRecipeIds(state)
  const previous = state.proposal?.day === day ? state.proposal : null
  const tried = previous?.tried ?? []
  let candidates = state.recipes.filter((recipe) => !inWeek.has(recipe.id) && !tried.includes(recipe.id))
  let nextTried = tried
  if (candidates.length === 0 && tried.length > 0) {
    // Everything has been offered once: start over, but never repeat the one on screen.
    candidates = state.recipes.filter((recipe) => !inWeek.has(recipe.id) && recipe.id !== previous?.recipeId)
    nextTried = previous ? [previous.recipeId] : []
  }
  if (candidates.length === 0) {
    return withStatus(state, 'warning', 'There are no other saved recipes to try.')
  }
  const pick = candidates[Math.min(candidates.length - 1, Math.floor(rng() * candidates.length))]
  const seq = state.seq + 1
  return { ...state, seq, status: null, proposal: { day, recipeId: pick.id, tried: [...nextTried, pick.id] } }
}

export function cancelProposal(state: PlannerState): PlannerState {
  return state.proposal ? { ...state, proposal: null } : state
}

/** Keep it: apply the pending proposal. */
export function keepProposal(state: PlannerState): PlannerState {
  const proposal = state.proposal
  if (!proposal) return state
  const recipe = findRecipe(state, proposal.recipeId)
  const night = state.plan.nights[proposal.day]
  if (!recipe || night.status === 'skipped' || night.pinned) {
    return withStatus({ ...state, proposal: null }, 'warning', 'That suggestion is no longer available.')
  }
  const name = dayName(proposal.day)
  const elsewhere = dayWithRecipe(state, recipe.id, proposal.day)
  if (elsewhere !== undefined) {
    return withStatus({ ...state, proposal: null }, 'warning', `${recipe.title} is already on ${dayName(elsewhere)}`)
  }
  const message = night.status === 'planned' ? `Swapped ${name} to ${recipe.title}` : `Planned ${name}: ${recipe.title}`
  return commit(state, replaceNight(state.plan.nights, planned(proposal.day, recipe)), message)
}

/** Explicitly put a recipe on a night (open, or planned-but-not-pinned). Skipped and pinned nights refuse. */
export function assignRecipe(state: PlannerState, day: DayIndex, recipeId: string, servings?: number): PlannerState {
  const recipe = findRecipe(state, recipeId)
  const night = state.plan.nights[day]
  const name = dayName(day)
  if (!recipe) return withStatus(state, 'warning', 'That recipe could not be found.')
  if (night.status === 'skipped') return withStatus(state, 'warning', `${name} is skipped — make it open first.`)
  if (night.pinned) return withStatus(state, 'warning', `${name} is pinned. Unpin it first.`)
  if (night.status === 'planned' && night.recipeId === recipeId) return state // already there
  const elsewhere = dayWithRecipe(state, recipe.id, day)
  if (elsewhere !== undefined) return withStatus(state, 'warning', `${recipe.title} is already on ${dayName(elsewhere)}`)
  const message = night.status === 'planned' ? `Swapped ${name} to ${recipe.title}` : `Planned ${name}: ${recipe.title}`
  const count = servings === undefined ? recipe.defaultServings : clampServings(servings)
  return commit(state, replaceNight(state.plan.nights, planned(day, recipe, count)), message, {
    status: { kind: 'success', text: message },
  })
}

/** Move a planned night to another night; swaps if the target is planned. Pins travel with the meal. */
export function moveNight(state: PlannerState, from: DayIndex, to: DayIndex): PlannerState {
  if (from === to) return state
  const source = state.plan.nights[from]
  const target = state.plan.nights[to]
  if (source.status !== 'planned') {
    return withStatus(state, 'warning', `${dayName(from)} has no dinner to move.`)
  }
  if (target.status === 'skipped') {
    return withStatus(state, 'warning', `${dayName(to)} is skipped — make it open first.`)
  }
  let nights = replaceNight(state.plan.nights, { ...source, day: to })
  nights = replaceNight(nights, target.status === 'planned' ? { ...target, day: from } : OPEN(from))
  const message = target.status === 'planned' ? `Swapped ${dayName(from)} and ${dayName(to)}` : `Moved ${dayName(from)} to ${dayName(to)}`
  return commit(state, nights, message)
}

export function setSkipped(state: PlannerState, day: DayIndex): PlannerState {
  const night = state.plan.nights[day]
  if (night.status === 'skipped') return state
  const recipe = findRecipe(state, night.recipeId)
  const message = recipe ? `Skipped ${dayName(day)} (removed ${recipe.title})` : `Skipped ${dayName(day)}`
  return commit(state, replaceNight(state.plan.nights, { day, status: 'skipped', pinned: false }), message)
}

/** Clear a night: removes a planned meal or un-skips. */
export function setOpen(state: PlannerState, day: DayIndex): PlannerState {
  const night = state.plan.nights[day]
  if (night.status === 'open') return state
  const recipe = findRecipe(state, night.recipeId)
  const message = recipe ? `Removed ${recipe.title} from ${dayName(day)}` : `Opened ${dayName(day)}`
  return commit(state, replaceNight(state.plan.nights, OPEN(day)), message)
}

export function togglePin(state: PlannerState, day: DayIndex): PlannerState {
  const night = state.plan.nights[day]
  if (night.status !== 'planned') {
    return withStatus(state, 'warning', 'Only a planned dinner can be pinned.')
  }
  const message = night.pinned ? `Unpinned ${dayName(day)}` : `Pinned ${dayName(day)}`
  return commit(state, replaceNight(state.plan.nights, { ...night, pinned: !night.pinned }), message)
}

export function setServings(state: PlannerState, day: DayIndex, servings: number): PlannerState {
  const night = state.plan.nights[day]
  if (night.status !== 'planned') return state
  const next = clampServings(servings)
  if (next === night.servings) return state
  return commit(state, replaceNight(state.plan.nights, { ...night, servings: next }), `Changed ${dayName(day)} to ${next} servings`, {
    coalesceKey: `servings:${day}`,
  })
}

/** Restore plan + progress from before the last plan-changing action. One level. */
export function undoLast(state: PlannerState): PlannerState {
  const entry = state.undo
  if (!entry) return state
  const seq = state.seq + 1
  return {
    ...state,
    seq,
    plan: { nights: entry.plan.nights, revision: state.plan.revision + 1 },
    progress: entry.progress,
    proposal: null,
    undo: null,
    status: { seq, kind: 'info', text: `Undone: ${entry.message}` },
  }
}

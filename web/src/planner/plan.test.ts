import { describe, expect, test } from 'vitest'
import { fillOrder, fillWeek, mulberry32, proposeSurprise, shuffle } from './plan'
import { reducer } from './reducer'
import { createSeedState } from './seed'
import { cleanSeed, emptyWeek, plannedDays, recipeOn, run } from './testing'
import type { Night, PlannerState } from './types'

const rng = () => mulberry32(42)

function withNight(state: PlannerState, night: Night): PlannerState {
  return { ...state, plan: { ...state.plan, nights: state.plan.nights.map((n) => (n.day === night.day ? night : n)) } }
}

describe('seed', () => {
  test('has at least 8 full recipes and the Mon/Wed/Thu plan', () => {
    const state = createSeedState()
    expect(state.recipes.length).toBeGreaterThanOrEqual(8)
    for (const recipe of state.recipes) {
      expect(recipe.ingredients.length).toBeGreaterThan(0)
      expect(recipe.cookTonight.length).toBeGreaterThan(0)
      expect(recipe.ingredients.some((i) => typeof i.quantity === 'number')).toBe(true)
    }
    expect(plannedDays(state)).toEqual([1, 3, 4])
  })
  test('returns independent copies', () => {
    const a = createSeedState()
    a.recipes[0].title = 'changed'
    expect(createSeedState().recipes[0].title).not.toBe('changed')
  })
})

describe('fillWeek (Plan my week = fill only)', () => {
  test('empty week plans Mon, Wed, Thu with distinct recipes', () => {
    const next = fillWeek(emptyWeek(), rng())
    expect(plannedDays(next)).toEqual([1, 3, 4])
    const ids = [1, 3, 4].map((d) => recipeOn(next, d as 1))
    expect(new Set(ids).size).toBe(3)
    expect(next.undo?.message).toBe('Planned Monday, Wednesday and Thursday')
    expect(next.plan.revision).toBe(1)
  })

  test('empty week falls back to earliest open nights when defaults are skipped, prep day last', () => {
    let state = emptyWeek()
    for (const day of [1, 3, 4] as const) state = withNight(state, { day, status: 'skipped', pinned: false })
    const next = fillWeek(state, rng())
    // Open: Sun(prep day), Tue, Fri, Sat -> Tue, Fri, Sat; Sunday is last.
    expect(plannedDays(next)).toEqual([2, 5, 6])
  })

  test('partial week adds only what is missing and leaves the planned night alone', () => {
    let state = emptyWeek()
    state = withNight(state, { day: 2, status: 'planned', recipeId: 'red-lentil-coconut-soup', servings: 6, pinned: false })
    const next = fillWeek(state, rng())
    expect(plannedDays(next)).toEqual([1, 2, 3])
    expect(next.plan.nights[2]).toEqual(state.plan.nights[2])
    expect(new Set([1, 2, 3].map((d) => recipeOn(next, d as 1))).size).toBe(3)
  })

  test('full week changes nothing and says so', () => {
    const state = createSeedState()
    const next = fillWeek(state, rng())
    expect(next.plan).toBe(state.plan)
    expect(next.undo).toBeNull()
    expect(next.status?.text).toBe('Your week already has 3 dinners. Use Surprise me on a night to swap one.')
  })

  test('pinned and skipped nights are untouched', () => {
    let state = emptyWeek()
    state = withNight(state, { day: 3, status: 'planned', recipeId: 'pork-kimchi-fried-rice', servings: 4, pinned: true })
    state = withNight(state, { day: 1, status: 'skipped', pinned: false })
    const next = fillWeek(state, rng())
    expect(next.plan.nights[3]).toEqual(state.plan.nights[3])
    expect(next.plan.nights[1].status).toBe('skipped')
    expect(plannedDays(next)).toHaveLength(3)
    expect(plannedDays(next)).toContain(3)
  })

  test('does not reuse recipes already in the week', () => {
    let state = emptyWeek()
    state = withNight(state, { day: 3, status: 'planned', recipeId: 'pork-kimchi-fried-rice', servings: 4, pinned: false })
    for (let seed = 0; seed < 25; seed += 1) {
      const next = fillWeek(state, mulberry32(seed))
      const ids = plannedDays(next).map((d) => recipeOn(next, d))
      expect(new Set(ids).size).toBe(ids.length)
    }
  })

  test('with too few saved recipes it fills what it can and says so', () => {
    const state = { ...emptyWeek(), recipes: createSeedState().recipes.slice(0, 2) }
    const next = fillWeek(state, rng())
    expect(plannedDays(next)).toHaveLength(2)
    expect(next.status?.kind).toBe('warning')
    expect(next.status?.text).toBe('Only 2 saved recipes were available, so we planned 2 of the 3 dinners needed.')
  })

  test('with no open nights it refuses', () => {
    let state = emptyWeek()
    for (let day = 0; day < 7; day += 1) state = withNight(state, { day: day as 0, status: 'skipped', pinned: false })
    const next = fillWeek(state, rng())
    expect(next.plan).toBe(state.plan)
    expect(next.status?.kind).toBe('warning')
  })

  test('respects the house-rule target', () => {
    const state = run(emptyWeek(), { type: 'rules/set', patch: { targetDinners: 5 } })
    expect(plannedDays(fillWeek(state, rng()))).toHaveLength(5)
  })

  test('is deterministic for a given rng and varies across seeds', () => {
    const picks = new Set<string>()
    for (let seed = 0; seed < 20; seed += 1) {
      const a = fillWeek(emptyWeek(), mulberry32(seed))
      const b = fillWeek(emptyWeek(), mulberry32(seed))
      expect(a.plan).toEqual(b.plan)
      picks.add(recipeOn(a, 1) ?? '')
    }
    expect(picks.size).toBeGreaterThan(1)
  })

  test('the reducer is pure: same action twice gives the same plan', () => {
    const action = { type: 'plan/fill', seed: 7 } as const
    expect(reducer(emptyWeek(), action).plan).toEqual(reducer(emptyWeek(), action).plan)
  })
})

describe('shuffle', () => {
  test('is a permutation and does not mutate its input', () => {
    const input = [1, 2, 3, 4, 5]
    const out = shuffle(input, mulberry32(3))
    expect([...out].sort()).toEqual(input)
    expect(input).toEqual([1, 2, 3, 4, 5])
  })
})

describe('Surprise me (replace one selected night)', () => {
  test('proposes without changing the plan, then Keep it replaces and is undoable', () => {
    const state = createSeedState()
    const proposed = proposeSurprise(state, 4, rng())
    expect(proposed.plan).toBe(state.plan)
    expect(proposed.proposal?.day).toBe(4)
    const recipeId = proposed.proposal?.recipeId as string
    const inWeek = [1, 3, 4].map((d) => recipeOn(state, d as 1))
    expect(inWeek).not.toContain(recipeId)

    const kept = reducer(proposed, { type: 'plan/keep' })
    expect(recipeOn(kept, 4)).toBe(recipeId)
    expect(kept.plan.nights[4].servings).toBe(kept.recipes.find((r) => r.id === recipeId)?.defaultServings)
    expect(kept.proposal).toBeNull()
    const title = kept.recipes.find((r) => r.id === recipeId)?.title
    expect(kept.undo?.message).toBe(`Swapped Thursday to ${title}`)

    const undone = reducer(kept, { type: 'undo' })
    expect(recipeOn(undone, 4)).toBe('salmon-lemon-herb-orzo')
  })

  test('cancel leaves the plan untouched', () => {
    const proposed = proposeSurprise(createSeedState(), 4, rng())
    const cancelled = reducer(proposed, { type: 'plan/cancel' })
    expect(cancelled.proposal).toBeNull()
    expect(recipeOn(cancelled, 4)).toBe('salmon-lemon-herb-orzo')
    expect(cancelled.undo).toBeNull()
  })

  test('on an open night it proposes a dinner for that night', () => {
    const state = createSeedState()
    const kept = run(state, { type: 'plan/propose', day: 2, seed: 5 }, { type: 'plan/keep' })
    expect(kept.plan.nights[2].status).toBe('planned')
    expect(kept.undo?.message).toMatch(/^Planned Tuesday: /)
  })

  test('is refused on a pinned night', () => {
    const state = run(createSeedState(), { type: 'plan/pin', day: 4 })
    const next = proposeSurprise(state, 4, rng())
    expect(next.proposal).toBeNull()
    expect(next.status).toMatchObject({ kind: 'warning', text: 'Thursday is pinned. Unpin it to let Surprise me swap it.' })
  })

  test('is refused on a skipped night', () => {
    const state = run(createSeedState(), { type: 'plan/skip', day: 5 })
    const next = proposeSurprise(state, 5, rng())
    expect(next.proposal).toBeNull()
    expect(next.status?.kind).toBe('warning')
  })

  test('Try another never repeats the same suggestion until the pool is exhausted', () => {
    let state = createSeedState() // 5 recipes outside the week
    const seen: string[] = []
    for (let i = 0; i < 5; i += 1) {
      state = proposeSurprise(state, 4, mulberry32(i))
      seen.push(state.proposal?.recipeId as string)
    }
    expect(new Set(seen).size).toBe(5)
    state = proposeSurprise(state, 4, mulberry32(9)) // exhausted: restarts, but not the one on screen
    expect(state.proposal?.recipeId).not.toBe(seen[4])
  })

  test('says so when no other recipe exists', () => {
    const base = createSeedState()
    const state = { ...base, recipes: base.recipes.filter((r) => ['gochujang-chicken', 'bean-sweet-potato-chili', 'salmon-lemon-herb-orzo'].includes(r.id)) }
    const next = proposeSurprise(state, 4, rng())
    expect(next.proposal).toBeNull()
    expect(next.status?.text).toBe('There are no other saved recipes to try.')
  })

  test('any other plan change drops a pending proposal', () => {
    const proposed = proposeSurprise(createSeedState(), 4, rng())
    expect(reducer(proposed, { type: 'plan/pin', day: 1 }).proposal).toBeNull()
  })

  test('Keep it refuses if the night was pinned meanwhile (state-level guard)', () => {
    const proposed = proposeSurprise(createSeedState(), 4, rng())
    const pinned = withNight(proposed, { ...proposed.plan.nights[4], pinned: true })
    const next = reducer(pinned, { type: 'plan/keep' })
    expect(recipeOn(next, 4)).toBe('salmon-lemon-herb-orzo')
    expect(next.status?.kind).toBe('warning')
  })
})

describe('moving, skipping, pinning, servings', () => {
  test('moving to an open night leaves the source open', () => {
    const next = run(createSeedState(), { type: 'plan/move', from: 1, to: 2 })
    expect(recipeOn(next, 2)).toBe('gochujang-chicken')
    expect(next.plan.nights[1]).toEqual({ day: 1, status: 'open', pinned: false })
    expect(next.undo?.message).toBe('Moved Monday to Tuesday')
  })

  test('moving onto a planned night swaps, carrying servings and pins', () => {
    const state = run(createSeedState(), { type: 'plan/pin', day: 1 })
    const next = run(state, { type: 'plan/move', from: 1, to: 4 })
    expect(recipeOn(next, 4)).toBe('gochujang-chicken')
    expect(recipeOn(next, 1)).toBe('salmon-lemon-herb-orzo')
    expect(next.plan.nights[4].pinned).toBe(true)
    expect(next.plan.nights[1].pinned).toBe(false)
    expect(next.undo?.message).toBe('Swapped Monday and Thursday')
  })

  test('pinned nights can still be moved by the user', () => {
    const state = run(createSeedState(), { type: 'plan/pin', day: 1 }, { type: 'plan/move', from: 1, to: 2 })
    expect(recipeOn(state, 2)).toBe('gochujang-chicken')
  })

  test('cannot move an open night or onto a skipped night', () => {
    const state = createSeedState()
    expect(run(state, { type: 'plan/move', from: 2, to: 1 }).plan).toBe(state.plan)
    const skipped = run(state, { type: 'plan/skip', day: 2 })
    const next = run(skipped, { type: 'plan/move', from: 1, to: 2 })
    expect(next.plan.nights[1].status).toBe('planned')
    expect(next.status).toMatchObject({ kind: 'warning', text: 'Tuesday is skipped — make it open first.' })
  })

  test('skipping a planned night removes the meal (undoable); setOpen clears it', () => {
    const skipped = run(createSeedState(), { type: 'plan/skip', day: 1 })
    expect(skipped.plan.nights[1]).toEqual({ day: 1, status: 'skipped', pinned: false })
    expect(skipped.undo?.message).toBe('Skipped Monday (removed Sheet-pan gochujang chicken & broccoli)')
    const open = run(skipped, { type: 'plan/open', day: 1 })
    expect(open.plan.nights[1].status).toBe('open')
  })

  test('pinning is only for planned nights', () => {
    const state = createSeedState()
    expect(run(state, { type: 'plan/pin', day: 2 }).status?.kind).toBe('warning')
    const pinned = run(state, { type: 'plan/pin', day: 1 })
    expect(pinned.plan.nights[1].pinned).toBe(true)
    expect(run(pinned, { type: 'plan/pin', day: 1 }).undo?.message).toBe('Unpinned Monday')
  })

  test('servings are clamped to 1–12 and coalesce into one undo step', () => {
    let state = createSeedState()
    state = run(state, { type: 'plan/servings', day: 4, servings: 5 }, { type: 'plan/servings', day: 4, servings: 6 }, { type: 'plan/servings', day: 4, servings: 99 })
    expect(state.plan.nights[4].servings).toBe(12)
    expect(state.undo?.message).toBe('Changed Thursday to 12 servings')
    expect(run(state, { type: 'plan/servings', day: 4, servings: 0 }).plan.nights[4].servings).toBe(1)
    const undone = run(state, { type: 'undo' })
    expect(undone.plan.nights[4].servings).toBe(4)
  })

  test('assignRecipe works on open or unpinned nights, refuses pinned/skipped', () => {
    const state = createSeedState()
    const placed = run(state, { type: 'plan/assign', day: 2, recipeId: 'tortilla-soup', servings: 6 })
    expect(placed.plan.nights[2]).toMatchObject({ status: 'planned', recipeId: 'tortilla-soup', servings: 6 })
    expect(run(run(state, { type: 'plan/pin', day: 1 }), { type: 'plan/assign', day: 1, recipeId: 'tortilla-soup' }).status?.kind).toBe('warning')
    expect(run(run(state, { type: 'plan/skip', day: 2 }), { type: 'plan/assign', day: 2, recipeId: 'tortilla-soup' }).status?.kind).toBe('warning')
    expect(run(state, { type: 'plan/assign', day: 2, recipeId: 'nope' }).plan).toBe(state.plan)
  })
})

describe('undo', () => {
  test('restores the plan and progress and is one level only', () => {
    let state = cleanSeed()
    state = run(state, { type: 'prep/toggle', key: 'gochujang-chicken:marinade' })
    expect(Object.keys(state.progress.prepDone)).toHaveLength(1)
    state = run(state, { type: 'plan/skip', day: 1 }) // removes the chicken meal
    const afterChange = state
    state = run(state, { type: 'undo' })
    expect(recipeOn(state, 1)).toBe('gochujang-chicken')
    expect(state.progress).toBe(afterChange.undo?.progress)
    expect(state.status?.text).toBe('Undone: Skipped Monday (removed Sheet-pan gochujang chicken & broccoli)')
    expect(state.undo).toBeNull()
    expect(run(state, { type: 'undo' })).toBe(state) // nothing further to undo
  })

  test('revision keeps increasing, including through undo', () => {
    let state = run(createSeedState(), { type: 'plan/skip', day: 1 })
    expect(state.plan.revision).toBe(1)
    state = run(state, { type: 'undo' })
    expect(state.plan.revision).toBe(2)
  })

  test('dismissUndo hides the entry without restoring', () => {
    const state = run(createSeedState(), { type: 'plan/skip', day: 1 }, { type: 'undo/dismiss' })
    expect(state.undo).toBeNull()
    expect(state.plan.nights[1].status).toBe('skipped')
  })

  test('refused actions do not consume the undo slot', () => {
    const state = run(createSeedState(), { type: 'plan/skip', day: 1 }, { type: 'plan/pin', day: 2 })
    expect(state.undo?.message).toMatch(/^Skipped Monday/)
  })
})

describe('undo coalescing only spans uninterrupted servings tweaks', () => {
  test('a progress change between two servings tweaks starts a new undo step (judge repro)', () => {
    const key = 'broccoli|head'
    let state = cleanSeed()
    state = run(state, { type: 'plan/servings', day: 4, servings: 5 })
    state = run(state, { type: 'shopping/toggle', key })
    expect(state.progress.shoppingChecked[key]).toBeDefined()
    state = run(state, { type: 'plan/servings', day: 4, servings: 6 })
    const undone = run(state, { type: 'undo' })
    expect(undone.plan.nights[4].servings).toBe(5)
    expect(undone.progress.shoppingChecked[key]).toBeDefined() // the tick is not lost
  })

  test('tweaks with nothing in between still collapse into one step', () => {
    let state = cleanSeed()
    state = run(state, { type: 'plan/servings', day: 4, servings: 5 }, { type: 'plan/servings', day: 4, servings: 6 })
    expect(run(state, { type: 'undo' }).plan.nights[4].servings).toBe(4)
  })

  test('a different night or a different plan change does not coalesce', () => {
    let state = cleanSeed()
    state = run(state, { type: 'plan/servings', day: 4, servings: 5 }, { type: 'plan/servings', day: 3, servings: 6 })
    expect(run(state, { type: 'undo' }).plan.nights[4].servings).toBe(5)
  })
})

describe('a recipe is on at most one night per week', () => {
  test('assignRecipe refuses a recipe already on another night', () => {
    const state = createSeedState()
    const next = run(state, { type: 'plan/assign', day: 2, recipeId: 'gochujang-chicken' })
    expect(next.plan).toBe(state.plan)
    expect(next.status).toMatchObject({ kind: 'warning', text: 'Sheet-pan gochujang chicken & broccoli is already on Monday' })
  })

  test('assigning the recipe a night already has is a quiet no-op', () => {
    const state = createSeedState()
    const next = run(state, { type: 'plan/assign', day: 1, recipeId: 'gochujang-chicken' })
    expect(next).toBe(state)
  })

  test('keepProposal refuses (and clears the proposal) if the recipe has since been planned elsewhere', () => {
    const proposed = proposeSurprise(createSeedState(), 2, rng())
    const recipeId = proposed.proposal?.recipeId as string
    const title = proposed.recipes.find((r) => r.id === recipeId)?.title
    // Another night takes that recipe before "Keep it".
    const raced = withNight(proposed, { day: 5, status: 'planned', recipeId, servings: 4, pinned: false })
    const next = reducer(raced, { type: 'plan/keep' })
    expect(next.plan).toBe(raced.plan)
    expect(next.proposal).toBeNull()
    expect(next.status?.text).toBe(`${title} is already on Friday`)
  })

  test('fill and surprise never pick a recipe already in the week', () => {
    for (let seed = 0; seed < 60; seed += 1) {
      const filled = fillWeek(run(createSeedState(), { type: 'plan/open', day: 3 }), mulberry32(seed))
      const ids = plannedDays(filled).map((d) => recipeOn(filled, d))
      expect(new Set(ids).size).toBe(ids.length)

      const proposed = proposeSurprise(createSeedState(), 2, mulberry32(seed))
      expect(['gochujang-chicken', 'bean-sweet-potato-chili', 'salmon-lemon-herb-orzo']).not.toContain(proposed.proposal?.recipeId)
    }
  })
})

describe('fillOrder', () => {
  test('defaults first, then earliest open nights, prep day last; only open nights', () => {
    expect(fillOrder(emptyWeek().plan, 0)).toEqual([1, 3, 4, 2, 5, 6, 0])
    let state = emptyWeek()
    state = withNight(state, { day: 3, status: 'planned', recipeId: 'tortilla-soup', servings: 4, pinned: false })
    state = withNight(state, { day: 1, status: 'skipped', pinned: false })
    expect(fillOrder(state.plan, 0)).toEqual([4, 2, 5, 6, 0])
  })
  test('a different prep day moves to the end instead', () => {
    expect(fillOrder(emptyWeek().plan, 2)).toEqual([1, 3, 4, 0, 5, 6, 2])
  })
  test('fillWeek follows it', () => {
    let state = emptyWeek()
    for (const day of [1, 3, 4] as const) state = withNight(state, { day, status: 'skipped', pinned: false })
    expect(plannedDays(fillWeek(state, rng()))).toEqual(fillOrder(state.plan, 0).slice(0, 3).sort((a, b) => a - b))
  })
})

describe('QA #50 planner fixes', () => {
  test('assignRecipe sets a status and registers an undo entry', () => {
    const state = createSeedState()
    const next = run(state, { type: 'plan/assign', day: 2, recipeId: 'tortilla-soup' })
    expect(next.status).toMatchObject({ kind: 'success', text: 'Planned Tuesday: Slow-cooker chicken tortilla soup' })
    expect(next.undo?.message).toBe('Planned Tuesday: Slow-cooker chicken tortilla soup')
    expect(recipeOn(run(next, { type: 'undo' }), 2)).toBeUndefined()
  })

  test('Plan my week with no saved recipes says so', () => {
    const state = { ...emptyWeek(), recipes: [] }
    const next = fillWeek(state, rng())
    expect(next.plan).toBe(state.plan)
    expect(next.status).toMatchObject({ kind: 'warning', text: 'You have no saved recipes yet.' })
  })
})

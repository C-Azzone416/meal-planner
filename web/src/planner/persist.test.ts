import { describe, expect, test } from 'vitest'
import { parsePersisted, serialize } from './persist'
import { createSeedState } from './seed'
import type { PlannerState } from './types'

function stored(mutate: (state: PlannerState) => void): string {
  const state = createSeedState()
  mutate(state)
  return serialize(state)
}

describe('parsePersisted range checks', () => {
  test('accepts the seed', () => {
    expect(parsePersisted(serialize(createSeedState()))).not.toBeNull()
  })

  test.each([
    ['fractional prepDay', (s: PlannerState) => { s.houseRules.prepDay = 2.5 as 2 }],
    ['out-of-range prepDay', (s: PlannerState) => { s.houseRules.prepDay = 7 as 6 }],
    ['negative prepDay', (s: PlannerState) => { s.houseRules.prepDay = -1 as 0 }],
    ['fractional targetDinners', (s: PlannerState) => { s.houseRules.targetDinners = 3.5 }],
    ['zero targetDinners', (s: PlannerState) => { s.houseRules.targetDinners = 0 }],
    ['targetDinners over 7', (s: PlannerState) => { s.houseRules.targetDinners = 8 }],
    ['fractional night servings', (s: PlannerState) => { s.plan.nights[1].servings = 4.5 }],
    ['night servings over 12', (s: PlannerState) => { s.plan.nights[1].servings = 13 }],
    ['zero night servings', (s: PlannerState) => { s.plan.nights[1].servings = 0 }],
    ['duplicate ingredient ids', (s: PlannerState) => { s.recipes[0].ingredients[1].id = s.recipes[0].ingredients[0].id }],
    ['duplicate prep-ahead step ids', (s: PlannerState) => { s.recipes[0].prepAhead[1].id = s.recipes[0].prepAhead[0].id }],
    ['duplicate ingredient ids in a draft', (s: PlannerState) => { s.drafts[0].ingredients[1].id = s.drafts[0].ingredients[0].id }],
    ['duplicate recipe ids', (s: PlannerState) => { s.recipes[1].id = s.recipes[0].id }],
  ])('rejects %s', (_name, mutate) => {
    expect(parsePersisted(stored(mutate))).toBeNull()
  })

  test('boundary values are accepted', () => {
    const raw = stored((s) => {
      s.houseRules = { targetDinners: 7, prepDay: 6 }
      s.plan.nights[1].servings = 12
      s.plan.nights[3].servings = 1
    })
    expect(parsePersisted(raw)).not.toBeNull()
  })
})

describe('hydration only trusts validated fields', () => {
  test('a stored blob cannot smuggle in transient state', async () => {
    const { loadState, STORAGE_KEY } = await import('./persist')
    const state = createSeedState()
    const blob = JSON.parse(serialize(state))
    blob.state.undo = { seq: 99, message: 'hi', plan: {}, progress: null }
    blob.state.proposal = { day: 'x' }
    blob.state.status = { text: 5 }
    blob.state.seq = 'NaN'
    blob.state.extra = true
    const storage = { getItem: (k: string) => (k === STORAGE_KEY ? JSON.stringify(blob) : null), setItem: () => undefined, removeItem: () => undefined }
    const loaded = loadState(storage)
    expect(loaded.resetNotice).toBeNull()
    expect(loaded.state.undo).toBeNull()
    expect(loaded.state.proposal).toBeNull()
    expect(loaded.state.status).toBeNull()
    expect(loaded.state.seq).toBe(0)
    expect(loaded.state).not.toHaveProperty('extra')
    expect(loaded.state.recipes).toHaveLength(8)
  })

  test('rejects a recipe planned on two nights', () => {
    expect(parsePersisted(stored((s) => { s.plan.nights[3].recipeId = s.plan.nights[1].recipeId }))).toBeNull()
  })
})

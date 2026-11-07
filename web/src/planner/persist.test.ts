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

import { expect, test } from 'vitest'
import { createSeedState } from '../planner/seed'
import type { DayIndex } from '../planner/types'
import { addToWeekNight } from './addToWeek'

const { plan } = createSeedState()

test('picks the first open night in Plan my week order, with the prep day last', () => {
  // Seed: Mon/Wed/Thu planned, Sunday is the prep day. Open nights: Tue, Fri, Sat, Sun.
  expect(addToWeekNight(plan, 0)).toBe(2)
})

test('prefers Mon, Wed, Thu when they are open', () => {
  const nights = plan.nights.map((night) => ({ day: night.day, status: 'open' as const, pinned: false }))
  expect(addToWeekNight({ ...plan, nights }, 0)).toBe(1)
})

test('puts the configured prep day last', () => {
  const nights = plan.nights.map((night) =>
    [0, 2, 5].includes(night.day) ? { day: night.day, status: 'open' as const, pinned: false } : { day: night.day, status: 'skipped' as const, pinned: false },
  )
  // Open: Sun, Tue, Fri. Prep day Tuesday goes last, so Sunday is first.
  expect(addToWeekNight({ ...plan, nights }, 2 as DayIndex)).toBe(0)
  // Prep day Sunday goes last, so Tuesday is first.
  expect(addToWeekNight({ ...plan, nights }, 0)).toBe(2)
})

test('returns undefined when no night is open', () => {
  const nights = plan.nights.map((night) => ({ day: night.day, status: 'skipped' as const, pinned: false }))
  expect(addToWeekNight({ ...plan, nights }, 0)).toBeUndefined()
})

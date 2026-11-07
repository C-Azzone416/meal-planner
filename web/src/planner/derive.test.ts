import { describe, expect, test } from 'vitest'
import {
  deriveNightWarnings,
  derivePantry,
  derivePrep,
  deriveShopping,
  planSummary,
  roundQuantity,
} from './derive'
import { formatAmount, formatQuantity } from './format'
import { normalizeName, prepKey, shoppingKey } from './keys'
import { createSeedState } from './seed'
import { cleanSeed, run } from './testing'

const row = (state: ReturnType<typeof cleanSeed>, name: string, unit = '') => {
  const found = deriveShopping(state).rows.find((r) => r.key === shoppingKey(name, unit))
  if (!found) throw new Error(`no row for ${name}`)
  return found
}

describe('keys and formatting', () => {
  test('normalises names so plurals and case match', () => {
    expect(normalizeName('Yellow Onions')).toBe(normalizeName('yellow onion'))
    expect(normalizeName('cherry tomatoes')).toBe(normalizeName('Cherry Tomato'))
    expect(normalizeName('  Olive   oil (extra virgin)')).toBe('olive oil')
    expect(shoppingKey('Lemons', '')).toBe('lemon|')
    expect(prepKey('chili', 'dice')).toBe('chili:dice')
  })
  test('formats quarter fractions', () => {
    expect(formatQuantity(1.25)).toBe('1¼')
    expect(formatQuantity(0.5)).toBe('½')
    expect(formatQuantity(3)).toBe('3')
    expect(formatAmount(2, 'can')).toBe('2 cans')
    expect(formatAmount(1, 'small tub')).toBe('1 small tub')
    expect(formatAmount(0.5, 'bunch')).toBe('½ bunch')
    expect(formatAmount(2, 'lb')).toBe('2 lb')
    expect(formatAmount(null, 'lb')).toBe('')
  })
  test('rounds up: ¼ for measures, ½ for bunches, whole for counts', () => {
    expect(roundQuantity(1.875, 'cup')).toBe(2)
    expect(roundQuantity(2.6, 'lb')).toBe(2.75)
    expect(roundQuantity(2.5, 'lb')).toBe(2.5)
    expect(roundQuantity(0.6, 'bunch')).toBe(1)
    expect(roundQuantity(0.5, 'bunch')).toBe(0.5)
    expect(roundQuantity(2.2, '')).toBe(3)
    expect(roundQuantity(2, '')).toBe(2)
    expect(roundQuantity(0.01, 'tsp')).toBe(0.25)
  })
})

describe('deriveShopping', () => {
  test('seed week reproduces the sample list (Mon/Wed/Thu)', () => {
    const list = deriveShopping(cleanSeed())
    const amount = (name: string, unit = '') => row(cleanSeed(), name, unit).amount
    expect(amount('yellow onions')).toBe('3')
    expect(amount('sweet potatoes')).toBe('2')
    expect(amount('broccoli', 'head')).toBe('2 heads')
    expect(amount('lemons')).toBe('2')
    expect(amount('parsley', 'bunch')).toBe('1 bunch')
    expect(amount('dill', 'bunch')).toBe('½ bunch')
    expect(amount('cherry tomatoes', 'pint')).toBe('1 pint')
    expect(amount('chicken thighs', 'lb')).toBe('2 lb')
    expect(amount('salmon fillets', 'lb')).toBe('2 lb')
    expect(amount('black beans', 'can')).toBe('2 cans')
    expect(amount('crushed tomatoes', 'can')).toBe('1 can')
    expect(amount('sharp cheddar', 'oz')).toBe('4 oz')
    expect(list.groups.map((g) => g.aisle)).toEqual(['Produce', 'Meat & fish', 'Dairy', 'Pantry aisle'])
  })

  test('shows which meals need a consolidated row', () => {
    const onions = row(cleanSeed(), 'yellow onions')
    expect(onions.mealsLabel).toBe('chicken + chili')
    expect(onions.meals.map((m) => m.day)).toEqual([1, 3])
  })

  test('pantry staples never appear on the list', () => {
    const names = deriveShopping(cleanSeed()).rows.map((r) => r.name)
    for (const staple of ['olive oil', 'soy sauce', 'gochujang', 'chili powder', 'ground cumin', 'salt']) {
      expect(names).not.toContain(staple)
    }
  })

  test('a running-low pantry item shows under Pantry aisle', () => {
    const state = run(cleanSeed(), { type: 'pantry/toggleLow', name: 'Olive Oil' })
    const oil = deriveShopping(state).rows.find((r) => r.name === 'olive oil')
    expect(oil).toMatchObject({ aisle: 'Pantry aisle', source: 'pantry-low', amount: 'running low', mealsLabel: 'chicken + salmon' })
    const toggledOff = run(state, { type: 'pantry/toggleLow', name: 'olive oil' })
    expect(deriveShopping(toggledOff).rows.find((r) => r.name === 'olive oil')).toBeUndefined()
  })

  test('serving changes scale linearly with sensible rounding', () => {
    // Salmon 2 lb for 4 -> 6 servings = 3 lb; orzo 1¼ cup * 1.5 = 1.875 -> 2 cup; dill ½ * 1.5 -> 1 bunch.
    const state = run(cleanSeed(), { type: 'plan/servings', day: 4, servings: 6 })
    expect(row(state, 'salmon fillets', 'lb').quantity).toBe(3)
    expect(row(state, 'orzo', 'cup').amount).toBe('2 cups')
    expect(row(state, 'dill', 'bunch').amount).toBe('1 bunch')
    // 5 servings: 2.5 lb
    expect(row(run(cleanSeed(), { type: 'plan/servings', day: 4, servings: 5 }), 'salmon fillets', 'lb').amount).toBe('2½ lb')
  })

  test('totals are rounded once, not per meal', () => {
        const state = run(cleanSeed(), { type: 'plan/assign', day: 2, recipeId: 'red-lentil-coconut-soup', servings: 6 })
    // onions: chicken 1 + chili 2 + lentil 1 * 1.5 = 4.5 -> 5
    expect(row(state, 'yellow onions').quantity).toBe(5)
  })

  test('different units for the same name stay separate rows', () => {
    const state = cleanSeed()
    const recipes = state.recipes.map((r) =>
      r.id === 'gochujang-chicken'
        ? { ...r, ingredients: [...r.ingredients, { id: 'x', name: 'Broccoli', quantity: 1, unit: 'lb', aisle: 'Produce' as const, isPantry: false }] }
        : r,
    )
    const rows = deriveShopping({ ...state, recipes }).rows.filter((r) => normalizeName(r.name) === 'broccoli')
    expect(rows.map((r) => r.unit).sort()).toEqual(['head', 'lb'])
  })

  test('removing or skipping a meal drops its ingredients', () => {
    const state = run(cleanSeed(), { type: 'plan/skip', day: 4 })
    expect(deriveShopping(state).rows.find((r) => r.name === 'salmon fillets')).toBeUndefined()
    expect(row(state, 'yellow onions').mealsLabel).toBe('chicken + chili')
  })

  test('toggling checks a row; unchecking removes the progress', () => {
    const key = shoppingKey('broccoli', 'head')
    const checked = run(cleanSeed(), { type: 'shopping/toggle', key })
    expect(row(checked, 'broccoli', 'head')).toMatchObject({ status: 'checked', checked: true, checkedQuantity: 2 })
    expect(deriveShopping(checked).counts).toMatchObject({ checked: 1 })
    expect(row(run(checked, { type: 'shopping/toggle', key }), 'broccoli', 'head').status).toBe('todo')
  })

  test('§3 example: increase unchecks and shows "need 1 more lb (you checked 2 lb)"', () => {
    // Salmon 2 lb for 4. Check it, then raise to 6 servings (3 lb).
    const key = shoppingKey('salmon fillets', 'lb')
    let state = run(cleanSeed(), { type: 'shopping/toggle', key })
    expect(row(state, 'salmon fillets', 'lb').checked).toBe(true)
    state = run(state, { type: 'plan/servings', day: 4, servings: 6 })
    const salmon = row(state, 'salmon fillets', 'lb')
    expect(salmon).toMatchObject({ status: 'needs-more', checked: false, shortfall: 1, checkedQuantity: 2 })
    expect(salmon.note).toBe('need 1 more lb (you checked 2 lb)')
    expect(deriveShopping(state).counts.toBuy).toBe(deriveShopping(state).counts.total)
    // Ticking it again confirms the full amount.
    const confirmed = run(state, { type: 'shopping/toggle', key })
    expect(row(confirmed, 'salmon fillets', 'lb')).toMatchObject({ status: 'checked', checkedQuantity: 3 })
  })

  test('count units: "need 1 more" without a unit', () => {
    const key = shoppingKey('lemons', '')
    const state = run(cleanSeed(), { type: 'shopping/toggle', key }, { type: 'plan/servings', day: 4, servings: 8 })
    expect(row(state, 'lemons').note).toBe('need 2 more (you checked 2)')
  })

  test('a decrease or no change keeps the row checked', () => {
    const key = shoppingKey('salmon fillets', 'lb')
    const checked = run(cleanSeed(), { type: 'shopping/toggle', key })
    expect(row(run(checked, { type: 'plan/servings', day: 4, servings: 2 }), 'salmon fillets', 'lb').status).toBe('checked')
    expect(row(run(checked, { type: 'plan/servings', day: 4, servings: 4 }), 'salmon fillets', 'lb').status).toBe('checked')
  })

  test('a checked item that is no longer needed moves to "No longer needed" and can be dismissed', () => {
    const key = shoppingKey('salmon fillets', 'lb')
    let state = run(cleanSeed(), { type: 'shopping/toggle', key }, { type: 'plan/open', day: 4 })
    const list = deriveShopping(state)
    expect(list.rows.find((r) => r.key === key)).toBeUndefined()
    expect(list.noLongerNeeded).toEqual([{ key, name: 'salmon fillets', unit: 'lb', aisle: 'Meat & fish', checkedQuantity: 2, amount: '2 lb' }])
    // Progress is kept, so bringing the meal back re-checks it.
    const restored = run(state, { type: 'undo' })
    expect(row(restored, 'salmon fillets', 'lb').checked).toBe(true)
    expect(deriveShopping(restored).noLongerNeeded).toHaveLength(0)
    // Dismiss removes it for good.
    state = run(state, { type: 'dismiss', kind: 'shopping', key })
    expect(deriveShopping(state).noLongerNeeded).toHaveLength(0)
  })

  test('dismiss ignores rows that are still needed', () => {
    const key = shoppingKey('salmon fillets', 'lb')
    const state = run(cleanSeed(), { type: 'shopping/toggle', key }, { type: 'dismiss', kind: 'shopping', key })
    expect(row(state, 'salmon fillets', 'lb').checked).toBe(true)
  })

  test('an unchecked row that vanishes leaves no trace', () => {
    expect(deriveShopping(run(cleanSeed(), { type: 'plan/open', day: 4 })).noLongerNeeded).toEqual([])
  })

  test('byMeal groups rows under each planned meal', () => {
    const { byMeal } = deriveShopping(cleanSeed())
    expect(byMeal.map((m) => m.meal.label)).toEqual(['chicken', 'chili', 'salmon'])
    expect(byMeal[0].rows.map((r) => r.name)).toContain('yellow onions')
    expect(byMeal[1].rows.map((r) => r.name)).toContain('yellow onions')
  })

  test('recipe edits recalculate and surface a status', () => {
    const state = cleanSeed()
    const salmon = state.recipes.find((r) => r.id === 'salmon-lemon-herb-orzo')!
    const edited = run(state, {
      type: 'recipe/update',
      recipe: { ...salmon, ingredients: salmon.ingredients.map((i) => (i.id === 'salmon' ? { ...i, quantity: 3 } : i)) },
    })
    expect(row(edited, 'salmon fillets', 'lb').quantity).toBe(3)
    expect(edited.status?.text).toBe('This week’s shopping and prep were updated.')
    // Editing an unplanned recipe is silent.
    const lentil = state.recipes.find((r) => r.id === 'red-lentil-coconut-soup')!
    expect(run(state, { type: 'recipe/update', recipe: { ...lentil, title: 'Lentil soup' } }).status).toBeNull()
  })

  test('an empty week gives an empty list', () => {
    const list = deriveShopping({ ...cleanSeed(), plan: { revision: 0, nights: cleanSeed().plan.nights.map((n) => ({ day: n.day, status: 'open' as const, pinned: false })) } })
    expect(list.rows).toEqual([])
    expect(list.counts).toEqual({ total: 0, toBuy: 0, checked: 0 })
  })
})

describe('derivePantry', () => {
  test('lists staples with who uses them and the running-low flag', () => {
    const state = run(cleanSeed(), { type: 'pantry/toggleLow', name: 'olive oil' })
    const pantry = derivePantry(state)
    const oil = pantry.find((p) => p.key === 'olive oil')
    expect(oil?.low).toBe(true)
    expect(oil?.usedBy.map((m) => m.label)).toEqual(['chicken', 'salmon'])
    expect(oil?.usedByRecipes.length).toBeGreaterThan(2)
    expect(pantry.find((p) => p.key === 'ground cumin')?.low).toBe(false)
    expect(pantry.find((p) => p.key === 'curry powder')?.usedBy).toEqual([])
  })
})

describe('derivePrep', () => {
  test('lists prep-ahead tasks per planned meal in day order with done state', () => {
    const prep = derivePrep(createSeedForPrep())
    expect(prep.tasks.map((t) => t.key)).toEqual([
      'gochujang-chicken:dice-onion',
      'gochujang-chicken:marinade',
      'bean-sweet-potato-chili:dice-onions',
      'bean-sweet-potato-chili:cube-sweet-potatoes',
      'bean-sweet-potato-chili:measure-spices',
      'salmon-lemon-herb-orzo:herb-sauce',
      'salmon-lemon-herb-orzo:toast-orzo',
    ])
    expect(prep.tasks.filter((t) => t.done).map((t) => t.stepId)).toEqual(['dice-onions', 'cube-sweet-potatoes'])
    expect(prep.totals).toEqual({ tasks: 7, done: 2, minutes: 39, minutesLeft: 27 })
    expect(prep.tasks.every((t) => t.warning === null)).toBe(true)
  })

  test('toggling marks done and records the serving factor', () => {
    const key = 'gochujang-chicken:marinade'
    const state = run(cleanSeed(), { type: 'prep/toggle', key })
    expect(state.progress.prepDone[key]).toMatchObject({ doneFactor: 1, text: 'Mix marinade, bag the chicken' })
    expect(derivePrep(state).tasks.find((t) => t.key === key)?.status).toBe('done')
    expect(derivePrep(run(state, { type: 'prep/toggle', key })).tasks.find((t) => t.key === key)?.status).toBe('todo')
  })

  test('a serving increase marks a done task "needs a top-up" but it stays done; confirming refreshes it', () => {
    const key = 'gochujang-chicken:marinade'
    let state = run(cleanSeed(), { type: 'prep/toggle', key }, { type: 'plan/servings', day: 1, servings: 6 })
    const task = derivePrep(state).tasks.find((t) => t.key === key)
    expect(task).toMatchObject({ status: 'topup', done: true })
    expect(derivePrep(state).totals.done).toBe(1)
    state = run(state, { type: 'prep/toggle', key })
    expect(derivePrep(state).tasks.find((t) => t.key === key)).toMatchObject({ status: 'done' })
    expect(state.progress.prepDone[key].doneFactor).toBe(1.5)
  })

  test('a decrease does not need a top-up', () => {
    const key = 'gochujang-chicken:marinade'
    const state = run(cleanSeed(), { type: 'prep/toggle', key }, { type: 'plan/servings', day: 1, servings: 2 })
    expect(derivePrep(state).tasks.find((t) => t.key === key)?.status).toBe('done')
  })

  test('removing a meal keeps done progress under "Done, no longer planned"', () => {
    const state = run(createSeedForPrep(), { type: 'plan/open', day: 3 })
    const prep = derivePrep(state)
    expect(prep.tasks.some((t) => t.recipeId === 'bean-sweet-potato-chili')).toBe(false)
    expect(prep.doneNoLongerPlanned).toEqual([
      { key: 'bean-sweet-potato-chili:dice-onions', text: 'Dice 2 onions', minutes: 4, mealTitle: 'Slow-cooker bean & sweet potato chili' },
      { key: 'bean-sweet-potato-chili:cube-sweet-potatoes', text: 'Peel & cube 2 sweet potatoes', minutes: 8, mealTitle: 'Slow-cooker bean & sweet potato chili' },
    ])
    expect(prep.totals.done).toBe(0)
    // Undo brings them back as ordinary done tasks.
    const restored = derivePrep(run(state, { type: 'undo' }))
    expect(restored.doneNoLongerPlanned).toEqual([])
    expect(restored.totals.done).toBe(2)
    // Dismiss removes one for good.
    const dismissed = run(state, { type: 'dismiss', kind: 'prep', key: 'bean-sweet-potato-chili:dice-onions' })
    expect(derivePrep(dismissed).doneNoLongerPlanned).toHaveLength(1)
  })

  test('toggling a "done, no longer planned" task removes it', () => {
    const state = run(createSeedForPrep(), { type: 'plan/open', day: 3 }, { type: 'prep/toggle', key: 'bean-sweet-potato-chili:dice-onions' })
    expect(derivePrep(state).doneNoLongerPlanned).toHaveLength(1)
  })

  test('moving a meal later raises a keep-time warning (§3 example)', () => {
    // Marinade keeps 1 day; prep day is Sunday. Monday is fine, Thursday is not.
    const state = run(cleanSeed(), { type: 'plan/move', from: 1, to: 5 }) // Mon -> Fri
    const task = derivePrep(state).tasks.find((t) => t.key === 'gochujang-chicken:marinade')
    expect(task?.warning).toEqual({ message: 'Marinade keeps 1 day — prep it Thursday instead', suggestedPrepDay: 4, daysAhead: 5 })
    expect(deriveNightWarnings(state)[5]).toContain('Marinade keeps 1 day — prep it Thursday instead')
    expect(deriveNightWarnings(state)[3]).toEqual([])
    const toThursday = run(cleanSeed(), { type: 'plan/move', from: 1, to: 4 }) // swaps with salmon
    expect(derivePrep(toThursday).tasks.find((t) => t.key === 'gochujang-chicken:marinade')?.warning?.message).toBe(
      'Marinade keeps 1 day — prep it Wednesday instead',
    )
  })

  test('limits are inclusive: salmon sauce (4 days) on Thursday is fine, Friday warns', () => {
    const fine = derivePrep(cleanSeed()).tasks.find((t) => t.stepId === 'herb-sauce')
    expect(fine?.warning).toBeNull()
    const state = run(cleanSeed(), { type: 'plan/move', from: 4, to: 5 })
    expect(derivePrep(state).tasks.find((t) => t.stepId === 'herb-sauce')?.warning?.message).toBe(
      'Herb sauce keeps 4 days — prep it Monday instead',
    )
  })

  test('a different prep day changes the warnings', () => {
    const state = run(cleanSeed(), { type: 'rules/set', patch: { prepDay: 3 } }) // Wednesday prep
    // Monday chicken: (1-3+7)%7 = 5 days > 1 -> warns, suggests Sunday.
    expect(derivePrep(state).tasks.find((t) => t.stepId === 'marinade')?.warning?.suggestedPrepDay).toBe(0)
  })

  test('steps that never expire (keepsDays null) never warn', () => {
    const state = run(cleanSeed(), { type: 'plan/move', from: 3, to: 6 })
    expect(derivePrep(state).tasks.find((t) => t.stepId === 'measure-spices')?.warning).toBeNull()
  })

  test('empty plan has no tasks', () => {
    const state = run(cleanSeed(), { type: 'plan/open', day: 1 }, { type: 'plan/open', day: 3 }, { type: 'plan/open', day: 4 })
    expect(derivePrep(state).tasks).toEqual([])
  })
})

describe('planSummary', () => {
  test('counts nights and remaining dinners', () => {
    expect(planSummary(cleanSeed())).toEqual({ planned: 3, open: 4, skipped: 0, pinned: 0, target: 3, remaining: 0, full: true, label: '3 nights planned' })
    const state = run(cleanSeed(), { type: 'plan/skip', day: 1 }, { type: 'plan/pin', day: 3 })
    expect(planSummary(state)).toMatchObject({ planned: 2, skipped: 1, pinned: 1, remaining: 1, full: false, label: '2 nights planned' })
  })
})

function createSeedForPrep() {
  // The shipped seed has the two chili prep tasks already done.
  return { ...cleanSeed(), progress: { shoppingChecked: {}, prepDone: createSeedState().progress.prepDone } }
}

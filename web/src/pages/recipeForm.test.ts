import { expect, test } from 'vitest'
import { createSeedState } from '../planner/seed'
import type { Recipe } from '../planner/types'
import { formFromRecipe, toRecipeFields, validate } from './recipeForm'

function recipe(): Recipe {
  const base = createSeedState().recipes[0]
  return {
    ...base,
    ingredients: [
      { ...base.ingredients[0], quantity: 0.3333333333 },
      { ...base.ingredients[1], quantity: 1.25 },
      { ...base.ingredients[2], quantity: null },
    ],
    cookTonight: [
      { id: 'a', text: 'Simmer', timerSeconds: 100 },
      { id: 'b', text: 'Rest', timerSeconds: 90 },
      { id: 'c', text: 'Serve', timerSeconds: null },
    ],
  }
}

test('a no-op edit round-trips quantities and timers exactly', () => {
  const original = recipe()
  // The page saves { ...recipe, ...fields }, so fields the form does not edit (id, shortName) are kept.
  expect({ ...original, ...toRecipeFields(formFromRecipe(original)) }).toEqual(original)
})

test('editing a quantity or timer replaces only that value', () => {
  const original = recipe()
  const form = formFromRecipe(original)
  form.ingredients[0].quantity = '0.5'
  form.cookTonight[0].timerMinutes = '2'
  const saved = toRecipeFields(form)
  expect(saved.ingredients[0].quantity).toBe(0.5)
  expect(saved.ingredients[1].quantity).toBe(1.25)
  expect(saved.cookTonight[0].timerSeconds).toBe(120)
  expect(saved.cookTonight[1].timerSeconds).toBe(90)
})

test('retyping the displayed rounded value keeps the exact stored value', () => {
  const form = formFromRecipe(recipe())
  expect(form.ingredients[0].quantity).toBe('0.33')
  expect(toRecipeFields(form).ingredients[0].quantity).toBe(0.3333333333)
})


test.each([0.004, 0.0000001])('small quantity %s survives create and an unrelated edit', (quantity) => {
  const form = formFromRecipe(recipe())
  form.ingredients[0].quantity = String(quantity.toLocaleString('en-US', { useGrouping: false, maximumSignificantDigits: 21 }))
  expect(validate(form)).toEqual([])
  const created = { ...recipe(), ...toRecipeFields(form) }
  const edit = formFromRecipe(created)
  expect(Number(edit.ingredients[0].quantity)).toBe(quantity)
  edit.title = 'Updated title'
  expect(validate(edit)).toEqual([])
  expect(toRecipeFields(edit).ingredients[0].quantity).toBe(quantity)
})

test('a timer cannot save zero seconds after rounding', () => {
  const form = formFromRecipe(recipe())
  form.cookTonight[0].timerMinutes = '0.001'
  expect(validate(form)).toContainEqual(expect.objectContaining({ field: 'cook-a-timerMinutes' }))
  form.cookTonight[0].timerMinutes = '0.01'
  expect(validate(form)).toEqual([])
  const created = { ...recipe(), ...toRecipeFields(form) }
  expect(created.cookTonight[0].timerSeconds).toBe(1)
  const edit = formFromRecipe(created)
  edit.title = 'Updated timer recipe'
  expect(validate(edit)).toEqual([])
  expect(toRecipeFields(edit).cookTonight[0].timerSeconds).toBe(1)
})

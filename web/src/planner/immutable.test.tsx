// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, expect, test } from 'vitest'
import { PlannerProvider, usePlanner, type PlannerData } from './store'
import type { Night, Recipe } from './types'

afterEach(cleanup)

function mount() {
  return renderHook(() => usePlanner(), {
    wrapper: ({ children }: { children: ReactNode }) => <PlannerProvider storage={null}>{children}</PlannerProvider>,
  })
}

test('retained public nested state cannot corrupt the previous undo snapshot', () => {
  const { result } = mount()
  const before = result.current.plan.nights[1]
  const servings = before.servings
  act(() => result.current.setServings(1, 6))
  expect(() => { (before as Night).servings = 9 }).toThrow(TypeError)
  expect(() => { (result.current.state.undo!.plan.nights[1] as Night).servings = 9 }).toThrow(TypeError)
  act(() => result.current.undo())
  expect(result.current.plan.nights[1].servings).toBe(servings)
})

test('getters and derived lists are deeply immutable while actions still accept copied edits', () => {
  const { result } = mount()
  const recipe = result.current.getRecipe('salmon-lemon-herb-orzo')!
  expect(() => { (recipe as Recipe).ingredients[0].name = 'mutated' }).toThrow(TypeError)
  expect(Object.isFrozen(result.current.getDraft('draft-fennel-traybake')!.confirmed)).toBe(true)
  expect(Object.isFrozen(result.current.shopping.rows[0].meals)).toBe(true)
  expect(Object.isFrozen(result.current.prep.tasks[0].meal)).toBe(true)
  expect(Object.isFrozen(result.current.pantry[0].usedByRecipes)).toBe(true)
  act(() => result.current.updateRecipe({ ...recipe, title: 'Updated safely' }))
  expect(result.current.getRecipe(recipe.id)?.title).toBe('Updated safely')
  expect(recipe.title).not.toBe('Updated safely')
})

test.each(['constructor', '__proto__', 'toString', 'hasOwnProperty'])('free-text unit %s does not crash planner derivations', (unit) => {
  const { result } = mount()
  const recipe = result.current.getRecipe('salmon-lemon-herb-orzo')!
  act(() => result.current.updateRecipe({
    ...recipe,
    ingredients: [{ ...recipe.ingredients[0], unit }],
  }))
  expect(result.current.shopping.rows.some((row) => row.key.endsWith(`|${unit.toLowerCase()}`))).toBe(true)
  expect(() => act(() => result.current.togglePantryLow(recipe.ingredients[0].name))).not.toThrow()
})

// Compile-time contract: tsc checks this exported, deliberately uncalled function.
// Every assertion must keep failing to compile, including access through raw state/getters.
export function assertPublicDataIsDeeplyReadonly(data: PlannerData) {
  // @ts-expect-error nested nights must not be mutable
  data.plan.nights[0].servings = 9
  // @ts-expect-error raw state is not an escape hatch
  data.state.undo!.plan.nights[0].servings = 9
  // @ts-expect-error recipe list children must not be mutable
  data.recipes[0].ingredients[0].quantity = 100
  // @ts-expect-error recipe getters must not expose mutable arrays
  data.getRecipe('x')!.tags.push('changed')
  // @ts-expect-error draft getters must not expose mutable confirmation records
  data.getDraft('x')!.confirmed.storage = true
  // @ts-expect-error derived nested records must not be mutable
  data.shopping.rows[0].meals[0].title = 'changed'
  // @ts-expect-error derived arrays must not be mutable
  data.prep.tasks.push(data.prep.tasks[0])
  // @ts-expect-error pantry child records must not be mutable
  data.pantry[0].usedByRecipes[0].title = 'changed'
  // @ts-expect-error progress values must not be mutable
  data.progress.shoppingChecked.x.checkedQuantity = 99
}

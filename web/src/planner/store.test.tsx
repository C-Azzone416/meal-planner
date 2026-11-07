// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, test } from 'vitest'
import { parsePersisted, RESET_NOTICE, STORAGE_KEY, type StorageLike } from './persist'
import { createSeedState } from './seed'
import { mulberry32 } from './plan'
import { PlannerProvider, usePlanner } from './store'
import type { NewRecipe } from './types'

afterEach(cleanup)

function fakeStorage(initial?: string): StorageLike & { data: Map<string, string> } {
  const data = new Map<string, string>()
  if (initial !== undefined) data.set(STORAGE_KEY, initial)
  return {
    data,
    getItem: (key) => data.get(key) ?? null,
    setItem: (key, value) => void data.set(key, value),
    removeItem: (key) => void data.delete(key),
  }
}

function mount(storage: StorageLike | null) {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <PlannerProvider storage={storage} random={mulberry32(1)}>{children}</PlannerProvider>
  )
  return renderHook(() => usePlanner(), { wrapper })
}

const newRecipe: NewRecipe = {
  title: 'Test Tacos',
  icon: '🌮',
  activeMinutes: 20,
  totalMinutes: 30,
  attention: 'medium',
  tags: [],
  defaultServings: 4,
  ingredients: [{ id: 'i1', name: 'tortillas', quantity: 8, unit: '', aisle: 'Bakery', isPantry: false }],
  prepAhead: [],
  prepTonight: [],
  cookTonight: [{ id: 's1', text: 'Assemble.', timerSeconds: null }],
}

describe('PlannerProvider', () => {
  test('seeds on first run and exposes derived lists', () => {
    const { result } = mount(fakeStorage())
    expect(result.current.recipes).toHaveLength(8)
    expect(result.current.summary.planned).toBe(3)
    expect(result.current.shopping.rows.length).toBeGreaterThan(5)
    expect(result.current.prep.tasks.length).toBe(7)
    expect(result.current.resetNotice).toBeNull()
  })

  test('throws a clear error outside the provider', () => {
    expect(() => renderHook(() => usePlanner())).toThrow('usePlanner must be used inside <PlannerProvider>')
  })

  test('persists under the versioned key and restores on reload', () => {
    const storage = fakeStorage()
    const first = mount(storage)
    act(() => first.result.current.setSkipped(5))
    const saved = JSON.parse(storage.data.get(STORAGE_KEY) as string)
    expect(saved.version).toBe(1)
    expect(saved.state.plan.nights[5].status).toBe('skipped')
    expect(saved.state.undo).toBeUndefined() // transient state is not persisted
    first.unmount()

    const second = mount(storage)
    expect(second.result.current.plan.nights[5].status).toBe('skipped')
    expect(second.result.current.undoInfo).toBeNull()
  })

  test.each([
    ['unparseable JSON', '{nope'],
    ['an old version', JSON.stringify({ version: 0, state: {} })],
    ['a wrong shape', JSON.stringify({ version: 1, state: { recipes: 'x' } })],
    ['a plan pointing at a missing recipe', (() => {
      const state = createSeedState()
      state.plan.nights[1].recipeId = 'ghost'
      return JSON.stringify({ version: 1, state })
    })()],
  ])('reseeds and exposes a resetNotice for %s', (_name, raw) => {
    const storage = fakeStorage(raw)
    const { result } = mount(storage)
    expect(result.current.recipes).toHaveLength(8)
    expect(result.current.resetNotice).toBe(RESET_NOTICE)
    expect(parsePersisted(storage.data.get(STORAGE_KEY) as string)).not.toBeNull() // corrupt value overwritten
    act(() => result.current.dismissResetNotice())
    expect(result.current.resetNotice).toBeNull()
  })

  test('works without any storage', () => {
    const { result } = mount(null)
    act(() => result.current.setSkipped(5))
    expect(result.current.plan.nights[5].status).toBe('skipped')
  })

  test('addRecipe returns a stable slug id, avoiding collisions', () => {
    const { result } = mount(fakeStorage())
    let a = '', b = ''
    act(() => { a = result.current.addRecipe(newRecipe) })
    act(() => { b = result.current.addRecipe(newRecipe) })
    expect(a).toBe('test-tacos')
    expect(b).toBe('test-tacos-2')
    expect(result.current.getRecipe(a)?.title).toBe('Test Tacos')
  })

  test('updateRecipe on a planned recipe recalculates and sets a status', () => {
    const { result } = mount(fakeStorage())
    const salmon = result.current.getRecipe('salmon-lemon-herb-orzo')!
    act(() => result.current.updateRecipe({ ...salmon, title: 'Renamed salmon' }))
    expect(result.current.getRecipe('salmon-lemon-herb-orzo')?.title).toBe('Renamed salmon')
    expect(result.current.status?.text).toBe('This week’s shopping and prep were updated.')
  })

  test('drafts stay out of recipes until every guess is confirmed and approved', () => {
    const { result } = mount(fakeStorage())
    const draftId = 'draft-fennel-traybake'
    expect(result.current.recipes.some((r) => r.title.includes('traybake'))).toBe(false)
    let id: string | null = 'unset'
    act(() => { id = result.current.approveDraft(draftId) })
    expect(id).toBeNull()
    expect(result.current.drafts).toHaveLength(2)
    expect(result.current.status?.kind).toBe('warning')

    for (const key of ['activeMinutes', 'totalMinutes', 'attention', 'prepAhead', 'storage'] as const) {
      act(() => result.current.setDraftConfirmed(draftId, key, true))
    }
    act(() => { id = result.current.approveDraft(draftId) })
    expect(id).toBe('chicken-thigh-traybake-with-fennel')
    expect(result.current.drafts.map((d) => d.id)).toEqual(['draft-black-bean-chili'])
    expect(result.current.getRecipe(id as string)?.cookTonight).toHaveLength(1)
  })

  test('editing a draft bumps its revision and clears the affected confirmations', () => {
    const { result } = mount(fakeStorage())
    act(() => result.current.updateDraft('draft-black-bean-chili', { totalMinutes: 400 }))
    const draft = result.current.getDraft('draft-black-bean-chili')!
    expect(draft.revision).toBe(4)
    expect(draft.confirmed.totalMinutes).toBe(false)
  })

  test('fillWeek / surprise / undo flow through the injected random source', () => {
    const { result } = mount(fakeStorage())
    act(() => result.current.proposeSurprise(4))
    expect(result.current.proposal?.day).toBe(4)
    act(() => result.current.keepProposal())
    expect(result.current.undoInfo?.message).toMatch(/^Swapped Thursday to /)
    act(() => result.current.undo())
    expect(result.current.plan.nights[4].recipeId).toBe('salmon-lemon-herb-orzo')
    expect(result.current.undoInfo).toBeNull()

    act(() => result.current.fillWeek())
    expect(result.current.status?.text).toContain('already has 3 dinners')
  })

  test('shopping and prep toggles go through the hook', () => {
    const { result } = mount(fakeStorage())
    const row = result.current.shopping.rows.find((r) => r.name === 'broccoli')!
    act(() => result.current.toggleShoppingChecked(row.key))
    expect(result.current.shopping.rows.find((r) => r.name === 'broccoli')?.checked).toBe(true)
    const task = result.current.prep.tasks.find((t) => !t.done)!
    act(() => result.current.togglePrepDone(task.key))
    expect(result.current.prep.tasks.find((t) => t.key === task.key)?.done).toBe(true)
    act(() => result.current.togglePantryLow('Soy Sauce'))
    expect(result.current.pantry.find((p) => p.key === 'soy sauce')?.low).toBe(true)
  })

  test('resetPreviewData reseeds, clears notices and persists the seed', () => {
    const storage = fakeStorage('garbage')
    const { result } = mount(storage)
    act(() => result.current.setSkipped(5))
    act(() => result.current.resetPreviewData())
    expect(result.current.plan.nights[5].status).toBe('open')
    expect(result.current.resetNotice).toBeNull()
    expect(result.current.plan.nights[1].status).toBe('planned')
    expect(JSON.parse(storage.data.get(STORAGE_KEY) as string).state.plan.nights[5].status).toBe('open')
  })

  test('setHouseRules clamps values', () => {
    const { result } = mount(null)
    act(() => result.current.setHouseRules({ targetDinners: 40, prepDay: 3 }))
    expect(result.current.houseRules).toEqual({ targetDinners: 7, prepDay: 3 })
  })
})

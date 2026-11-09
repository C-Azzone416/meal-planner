// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, test } from 'vitest'
import { STORAGE_KEY, RESET_NOTICE } from '../planner/persist'
import { PlannerProvider } from '../planner/store'
import { HouseRulesPage } from './HouseRulesPage'

afterEach(cleanup)

function renderRules(initial?: string) {
  const data = new Map<string, string>()
  if (initial !== undefined) data.set(STORAGE_KEY, initial)
  const storage = {
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => void data.set(key, value),
    removeItem: (key: string) => void data.delete(key),
  }
  render(
    <MemoryRouter>
      <PlannerProvider storage={storage}><HouseRulesPage /></PlannerProvider>
    </MemoryRouter>,
  )
  return data
}

test('dinners per week is saved in the planner store', () => {
  const data = renderRules()
  fireEvent.click(screen.getByRole('button', { name: 'Increase dinners' }))
  expect(screen.getByText('4 dinners')).toBeTruthy()
  expect(JSON.parse(data.get(STORAGE_KEY) as string).state.houseRules.targetDinners).toBe(4)
})

test('unfinished save is disabled with a visible reason', () => {
  renderRules()
  const save = screen.getByRole('button', { name: 'Save house rules' }) as HTMLButtonElement
  expect(save.disabled).toBe(true)
  expect(screen.getByText(/coming soon/).id).toBe(save.getAttribute('aria-describedby'))
})

test('unreadable stored data is reseeded with a dismissable status', () => {
  renderRules('{broken')
  expect(screen.getByRole('status').textContent).toContain(RESET_NOTICE)
  fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
  expect(screen.queryByText(RESET_NOTICE)).toBeNull()
})

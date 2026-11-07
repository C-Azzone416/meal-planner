// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, expect, test, vi } from 'vitest'
import { PlannerProvider } from '../planner/store'
import '../styles/app.css'
import { CookPage } from './CookPage'
import { WeekPage } from './WeekPage'

const chicken = 'Sheet-pan gochujang chicken & broccoli'
const chili = 'Slow-cooker bean & sweet potato chili'
const salmon = 'Seared salmon, lemon-herb orzo'

function renderPlanner(path = '/week') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <PlannerProvider storage={null} random={() => 0.25}>
        <Routes>
          <Route path="/week" element={<WeekPage />} />
          <Route path="/cook" element={<CookPage />} />
        </Routes>
      </PlannerProvider>
    </MemoryRouter>,
  )
}

function click(name: string) {
  fireEvent.click(screen.getByRole('button', { name }))
}

function night(day: string) {
  return screen.getByRole('button', { name: day })
}

function openNight(day: string) {
  click(day)
  click('Make open')
}

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

test('shows the empty week and fills the three preferred nights', () => {
  renderPlanner()
  for (const day of ['Monday', 'Wednesday', 'Thursday']) openNight(day)

  expect(screen.getByRole('heading', { name: 'No dinners yet' })).toBeTruthy()
  click('Plan my week')

  expect(screen.getAllByRole('article')).toHaveLength(3)
  for (const day of ['Monday', 'Wednesday', 'Thursday']) {
    expect(night(day).textContent).toContain('planned')
  }
  expect(night('Tuesday').textContent).toContain('open')
})

test('fills only open nights and preserves a pinned dinner and a skipped night', () => {
  renderPlanner()
  const surprise = screen.getByRole<HTMLButtonElement>('button', { name: 'Surprise me' })
  expect(surprise.disabled).toBe(true)
  expect(screen.getByText('Pick a night first')).toBeTruthy()

  click('Monday')
  click('Pin dinner')
  expect(screen.getByText(/Monday is pinned/)).toBeTruthy()
  expect(surprise.disabled).toBe(true)

  click('Wednesday')
  click('Skip night')
  expect(screen.getByText(/Wednesday is skipped/)).toBeTruthy()
  expect(surprise.disabled).toBe(true)
  openNight('Thursday')
  click('Plan my week')

  expect(screen.getAllByRole('article')).toHaveLength(3)
  expect(night('Monday').textContent).toContain(chicken)
  expect(night('Monday').textContent).toContain('Serves 4')
  expect(screen.getByRole('button', { name: 'Pin Monday dinner', pressed: true })).toBeTruthy()
  expect(night('Wednesday').textContent).toContain('skipped')
  expect(night('Tuesday').textContent).toContain('planned')
  expect(night('Thursday').textContent).toContain('planned')
})

test('announces that the week is full', () => {
  renderPlanner()
  click('Plan my week')
  const status = screen.getAllByRole('status').find((element) => element.textContent?.includes('already has 3 dinners'))
  expect(status?.textContent).toBe('Your week already has 3 dinners. Use Surprise me on a night to swap one.')
  expect(screen.getAllByRole('article')).toHaveLength(3)
  expect(screen.queryByRole('button', { name: 'Undo' })).toBeNull()
})

test('offers another surprise and changes a dinner only when kept', () => {
  renderPlanner()
  click('Monday')
  click('Surprise me')
  const first = screen.getByText('Try:').textContent
  expect(night('Monday').textContent).toContain(chicken)

  click('Try another')
  const suggestion = screen.getByText('Try:')
  expect(suggestion.textContent).not.toBe(first)
  expect(night('Monday').textContent).toContain(chicken)

  const title = suggestion.textContent?.replace(/^Try: /, '')
  expect(title).toBeTruthy()
  click('Keep it')

  expect(night('Monday').textContent).toContain(title)
  expect(screen.queryByText('Try:')).toBeNull()
})

test('cancelling a suggestion leaves the open night open', () => {
  renderPlanner()
  click('Tuesday')
  click('Surprise me')
  expect(screen.getByText('Try:')).toBeTruthy()
  expect(night('Tuesday').textContent).toContain('open')

  click('Cancel')
  expect(screen.queryByText('Try:')).toBeNull()
  expect(night('Tuesday').textContent).toContain('open')
  expect(screen.queryByRole('button', { name: 'Undo' })).toBeNull()
})

test('swaps dinners, carries a pin, and restores the original nights with Undo', () => {
  renderPlanner()
  click('Monday')
  click('Pin dinner')
  fireEvent.change(screen.getByLabelText('Move to another night'), { target: { value: '3' } })
  click('Swap nights')

  expect(night('Monday').textContent).toContain(chili)
  expect(night('Wednesday').textContent).toContain(chicken)
  expect(screen.getByRole('button', { name: 'Pin Wednesday dinner', pressed: true })).toBeTruthy()
  expect(screen.getByText('Swapped Monday and Wednesday')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Undo' }).tabIndex).toBe(0)
  click('Undo')

  expect(night('Monday').textContent).toContain(chicken)
  expect(night('Wednesday').textContent).toContain(chili)
  expect(screen.getByRole('button', { name: 'Pin Monday dinner', pressed: true })).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Undo' })).toBeNull()
})

test('explains skipped move targets and shows prep timing after a valid move', () => {
  renderPlanner()
  click('Saturday')
  click('Skip night')
  click('Monday')
  const destination = screen.getByLabelText('Move to another night')
  fireEvent.change(destination, { target: { value: '6' } })
  expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Move dinner' }).disabled).toBe(true)
  expect(screen.getByText('Saturday is skipped — make it open first.')).toBeTruthy()

  fireEvent.change(destination, { target: { value: '5' } })
  click('Move dinner')
  expect(night('Friday').textContent).toContain(chicken)
  expect(screen.getByText('Marinade keeps 1 day — prep it Thursday instead')).toBeTruthy()
  expect(screen.getByRole('progressbar', { name: 'Prep tasks done' }).getAttribute('value')).toBe('2')
})

test('restarts the ten-second undo window on each serving change', () => {
  vi.useFakeTimers()
  renderPlanner()
  click('Monday')
  click('Increase servings')
  act(() => vi.advanceTimersByTime(9000))
  click('Increase servings')
  expect(night('Monday').textContent).toContain('Serves 6')

  act(() => vi.advanceTimersByTime(9999))
  expect(screen.getByRole('button', { name: 'Undo' })).toBeTruthy()
  act(() => vi.advanceTimersByTime(1))
  expect(screen.queryByRole('button', { name: 'Undo' })).toBeNull()
})

test('opens Cook for the requested night with scaled ingredients and display timers', () => {
  renderPlanner()
  click('Thursday')
  click('Increase servings')
  fireEvent.click(screen.getByRole('link', { name: 'Cook tonight →', description: 'Thursday' }))

  expect(screen.getByRole('heading', { level: 1, name: salmon })).toBe(document.activeElement)
  expect(screen.getByText('Thursday · Serves 5')).toBeTruthy()
  expect(screen.getByRole('heading', { name: 'Prep tonight' })).toBeTruthy()
  expect(screen.getByRole('heading', { name: 'Cook tonight' })).toBeTruthy()
  expect(screen.getByText('2½ lb')).toBeTruthy()
  expect(screen.getByText('Timer 9:00')).toBeTruthy()
})

test('shows the cook empty state for an explicitly requested open night', () => {
  renderPlanner('/cook?day=2')
  expect(screen.getByRole('heading', { name: 'Nothing to cook tonight' })).toBeTruthy()
  expect(screen.getByRole('link', { name: 'This week →' }).getAttribute('href')).toBe('/week')
})

test('defaults Cook to today or the first planned dinner', () => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date(2026, 10, 4, 12))
  const first = renderPlanner('/cook')
  expect(screen.getByRole('heading', { level: 1, name: chili })).toBeTruthy()
  first.unmount()

  vi.setSystemTime(new Date(2026, 10, 6, 12))
  renderPlanner('/cook')
  expect(screen.getByRole('heading', { level: 1, name: chicken })).toBeTruthy()
})

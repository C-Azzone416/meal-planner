// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, test } from 'vitest'
import { mulberry32 } from '../planner/plan'
import { PlannerProvider, usePlanner } from '../planner/store'
import { PrepPage } from './PrepPage'
import { ShoppingPage } from './ShoppingPage'

afterEach(cleanup)

/** Test-only controls so we can change the plan the way the Week page will. */
function Controls() {
  const planner = usePlanner()
  return (
    <div>
      <button onClick={() => planner.setServings(4, 6)}>t:salmon-6</button>
      <button onClick={() => planner.setOpen(4)}>t:clear-thu</button>
      <button onClick={() => planner.setOpen(3)}>t:clear-wed</button>
      <button onClick={() => planner.moveNight(1, 5)}>t:move-mon-fri</button>
      <button onClick={() => planner.undo()}>t:undo</button>
      <button onClick={() => { for (const d of [1, 3, 4] as const) planner.setOpen(d) }}>t:clear-all</button>
    </div>
  )
}

function renderPage(page: 'shopping' | 'prep') {
  return render(
    <MemoryRouter>
      <PlannerProvider storage={null} random={mulberry32(1)}>
        <Controls />
        {page === 'shopping' ? <ShoppingPage /> : <PrepPage />}
      </PlannerProvider>
    </MemoryRouter>,
  )
}

const click = (name: string | RegExp) => fireEvent.click(screen.getByRole('button', { name }))

describe('ShoppingPage', () => {
  test('groups the derived list by aisle with meals per row and excludes pantry staples', () => {
    renderPage('shopping')
    expect(screen.getByRole('heading', { name: 'shopping list' })).toBeTruthy()
    for (const aisle of ['Produce', 'Meat & fish', 'Dairy', 'Pantry aisle']) {
      expect(screen.getByRole('region', { name: aisle })).toBeTruthy()
    }
    const onions = screen.getByRole('checkbox', { name: /yellow onions/ })
    expect(within(onions.closest('.sp-row') as HTMLElement).getByText('chicken + chili')).toBeTruthy()
    expect(screen.queryByRole('checkbox', { name: /soy sauce/ })).toBeNull()
    // the seed has olive oil running low, so it shows in the Pantry aisle
    const oil = screen.getByRole('checkbox', { name: /olive oil/ })
    expect(within(oil.closest('.sp-row') as HTMLElement).getByText('running low')).toBeTruthy()
    expect(screen.getByText(/to buy · \d+ items/)).toBeTruthy()
  })

  test('can group by meal', () => {
    renderPage('shopping')
    click('By meal')
    expect(screen.getByRole('region', { name: 'Thursday · Seared salmon, lemon-herb orzo' })).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'Produce' })).toBeNull()
  })

  test('checking a row strikes it and updates the count', () => {
    renderPage('shopping')
    const before = screen.getByText(/to buy/).textContent
    fireEvent.click(screen.getByRole('checkbox', { name: /broccoli/ }))
    expect((screen.getByRole('checkbox', { name: /broccoli/ }) as HTMLInputElement).checked).toBe(true)
    expect(screen.getByText(/to buy/).textContent).not.toBe(before)
  })

  test('an increased amount unchecks and says how much more to buy', () => {
    renderPage('shopping')
    fireEvent.click(screen.getByRole('checkbox', { name: /salmon fillets/ }))
    click('t:salmon-6')
    const box = screen.getByRole('checkbox', { name: /salmon fillets/ }) as HTMLInputElement
    expect(box.checked).toBe(false)
    expect(within(box.closest('.sp-row') as HTMLElement).getByText('need 1 more lb (you checked 2 lb)')).toBeTruthy()
  })

  test('a checked row that is no longer needed moves to "No longer needed" and can be dismissed', () => {
    renderPage('shopping')
    fireEvent.click(screen.getByRole('checkbox', { name: /salmon fillets/ }))
    click('t:clear-thu')
    expect(screen.queryByRole('checkbox', { name: /salmon fillets/ })).toBeNull()
    const section = screen.getByRole('region', { name: 'No longer needed' })
    expect(within(section).getByText('salmon fillets')).toBeTruthy()
    expect(within(section).getByText('2 lb')).toBeTruthy()
    fireEvent.click(within(section).getByRole('button', { name: 'Dismiss salmon fillets' }))
    expect(screen.queryByRole('region', { name: 'No longer needed' })).toBeNull()
  })

  test('pantry shelf shows what uses each staple and toggles running low', () => {
    renderPage('shopping')
    click(/Pantry shelf/)
    expect(screen.getByRole('heading', { name: 'Pantry shelf' })).toBeTruthy()
    const soy = screen.getByRole('button', { name: 'soy sauce running low' })
    expect(soy.getAttribute('aria-pressed')).toBe('false')
    expect(within(soy.closest('.sp-row') as HTMLElement).getByText('used by: chicken')).toBeTruthy()
    expect(within(screen.getByRole('button', { name: 'curry powder running low' }).closest('.sp-row') as HTMLElement).getByText('not needed this week · in 1 saved recipe')).toBeTruthy()

    fireEvent.click(soy)
    expect(soy.getAttribute('aria-pressed')).toBe('true')
    click('This week’s list')
    expect(screen.getByRole('checkbox', { name: /soy sauce/ })).toBeTruthy()
  })

  test('shows the empty state when nothing is planned', () => {
    renderPage('shopping')
    click('t:clear-all')
    // The seed still has olive oil running low, so restock it to reach the true empty state.
    click('olive oil restocked')
    expect(screen.getByRole('heading', { name: 'Nothing to buy — plan a dinner first' })).toBeTruthy()
    expect((screen.getByRole('link', { name: 'Go to This week' }) as HTMLAnchorElement).getAttribute('href')).toBe('/week')
  })
})

describe('PrepPage', () => {
  test('lists prep-ahead tasks in order with done state and progress', () => {
    renderPage('prep')
    expect(screen.getByRole('heading', { name: 'sunday prep' })).toBeTruthy()
    const boxes = screen.getAllByRole('checkbox') as HTMLInputElement[]
    expect(boxes).toHaveLength(7)
    expect(boxes.filter((box) => box.checked)).toHaveLength(2)
    expect(document.querySelector('.progress-block p')?.textContent).toBe('39 min planned · 2 of 7 done · 27 min left')
    fireEvent.click(screen.getByRole('checkbox', { name: /Mix marinade/ }))
    expect(screen.getByText(/3 of 7 done/)).toBeTruthy()
  })

  test('shows storage notes and the meal each task belongs to', () => {
    renderPage('prep')
    const task = screen.getByRole('checkbox', { name: /Mix marinade/ }).closest('label') as HTMLElement
    expect(within(task).getByText('Mon · Sheet-pan gochujang chicken & broccoli')).toBeTruthy()
    expect(within(task).getByText('Fridge · 1 day')).toBeTruthy()
  })

  test('warns when a moved meal outlasts the prep keep-time', () => {
    renderPage('prep')
    expect(screen.queryByText(/keeps 1 day/)).toBeNull()
    click('t:move-mon-fri')
    const task = screen.getByRole('checkbox', { name: /Mix marinade/ }).closest('label') as HTMLElement
    expect(within(task).getByText('Marinade keeps 1 day — prep it Thursday instead')).toBeTruthy()
    expect(screen.getAllByText('Marinade keeps 1 day — prep it Thursday instead').length).toBeGreaterThan(1) // task + hero note
  })

  test('a removed meal keeps its done tasks under "Done, no longer planned", dismissable', () => {
    renderPage('prep')
    click('t:clear-wed')
    const section = screen.getByRole('region', { name: 'Done, no longer planned' })
    expect(within(section).getByText('Dice 2 onions')).toBeTruthy()
    expect(within(section).getByText('Peel & cube 2 sweet potatoes')).toBeTruthy()
    fireEvent.click(within(section).getByRole('button', { name: 'Dismiss Dice 2 onions' }))
    expect(within(screen.getByRole('region', { name: 'Done, no longer planned' })).queryByText('Dice 2 onions')).toBeNull()
    // undo returns the remaining progress to the list
    click('t:undo')
    expect(screen.queryByRole('region', { name: 'Done, no longer planned' })).toBeNull()
  })

  test('a serving increase marks a done task as needing a top-up but still done', () => {
    renderPage('prep')
    fireEvent.click(screen.getByRole('checkbox', { name: /Blend herb sauce/ }))
    click('t:salmon-6')
    const label = screen.getByRole('checkbox', { name: /Blend herb sauce/ }).closest('label') as HTMLElement
    expect(within(label).getByText(/Needs a top-up/)).toBeTruthy()
    expect(label.className).toContain('task-card--done')
    expect((within(label).getByRole('checkbox') as HTMLInputElement).checked).toBe(false)
    fireEvent.click(within(label).getByRole('checkbox'))
    expect(within(label).queryByText(/Needs a top-up/)).toBeNull()
  })

  test('shows the empty state when no dinners are planned', () => {
    renderPage('prep')
    click('t:clear-all')
    // The seed's done chili tasks remain as "done, no longer planned", so dismiss them to reach the empty state.
    for (const name of ['Dice 2 onions', 'Peel & cube 2 sweet potatoes']) fireEvent.click(screen.getByRole('button', { name: `Dismiss ${name}` }))
    expect(screen.getByRole('heading', { name: 'Nothing to prep — plan a dinner first' })).toBeTruthy()
  })
})

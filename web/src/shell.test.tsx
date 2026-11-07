// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, expect, test, vi } from 'vitest'
import { AppRoutes } from './App'
import { AuthProvider } from './auth/AuthProvider'
import type { AuthService, AuthUser } from './auth/service'
import { PlannerProvider } from './planner/store'

// One route is made to crash so we can see the per-route boundary keep the shell alive.
vi.mock('./pages/ShoppingPage', () => ({
  ShoppingPage: () => {
    throw new Error('boom')
  },
}))

const user: AuthUser = { id: 'u', email: 'allowed@example.test', emailVerified: true }

function renderApp(path: string) {
  const service: AuthService = {
    getSession: vi.fn().mockResolvedValue(user),
    signInWithPassword: vi.fn().mockResolvedValue(user),
    sendPasswordResetCode: vi.fn().mockResolvedValue(undefined),
    setPassword: vi.fn().mockResolvedValue(user),
    signOut: vi.fn().mockResolvedValue(undefined),
  }
  const router = createMemoryRouter(
    [{
      path: '*',
      element: (
        <AuthProvider serviceFactory={() => Promise.resolve(service)}>
          <PlannerProvider storage={null}><AppRoutes /></PlannerProvider>
        </AuthProvider>
      ),
    }],
    { initialEntries: [path] },
  )
  render(<RouterProvider router={router} />)
  return router
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

test('a crashing page shows the error card but keeps the navigation, and leaving the page clears it', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => undefined)
  renderApp('/shopping')
  expect(await screen.findByRole('heading', { name: 'Something went wrong on this page' })).toBeTruthy()
  expect(screen.getAllByRole('link', { name: 'Recipes' }).length).toBeGreaterThan(0)

  fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
  expect(screen.getByRole('alert')).toBeTruthy() // still broken, still contained

  fireEvent.click(screen.getAllByRole('link', { name: 'Recipes' })[0])
  expect(await screen.findByRole('heading', { name: 'our recipes' })).toBeTruthy()
  expect(screen.queryByRole('alert')).toBeNull()
})

test('moves focus to the page heading after navigating, but not on first load', async () => {
  renderApp('/week')
  await screen.findByRole('heading', { name: 'this week’s dinners' })
  expect(document.activeElement).toBe(document.body)

  fireEvent.click(screen.getAllByRole('link', { name: 'Recipes' })[0])
  const heading = await screen.findByRole('heading', { name: 'our recipes' })
  await waitFor(() => expect(document.activeElement).toBe(heading))
})

test('shows the offline banner inside the signed-in shell and hides it again when back online', async () => {
  const onLine = vi.spyOn(window.navigator, 'onLine', 'get').mockReturnValue(false)
  renderApp('/week')
  expect((await screen.findByText('You’re offline. Changes are saved in this browser.')).getAttribute('role')).toBe('status')
  act(() => {
    onLine.mockReturnValue(true)
    window.dispatchEvent(new Event('online'))
  })
  expect(screen.queryByText('You’re offline. Changes are saved in this browser.')).toBeNull()
})

test('an unknown recipe is a "Recipe not found" page with a way back, inside the shell', async () => {
  renderApp('/recipes/nope')
  expect(await screen.findByRole('heading', { level: 1, name: 'Recipe not found' })).toBeTruthy()
  expect(screen.getByRole('link', { name: 'Back to Recipes' }).getAttribute('href')).toBe('/recipes')
  expect(screen.getAllByRole('link', { name: 'Recipes' }).length).toBeGreaterThan(0)
})

test('empty recipes use the shared card with an Add recipe action; no-results clears filters and refocuses search', async () => {
  renderApp('/recipes')
  const search = await screen.findByLabelText('Search recipes')
  fireEvent.change(search, { target: { value: 'zzz' } })
  fireEvent.click(screen.getByRole('button', { name: 'Vegetarian' }))
  const card = await screen.findByRole('status', { name: '' })
  expect(card.textContent).toContain('No recipes match “zzz” with Vegetarian')
  fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))
  expect(document.activeElement).toBe(search)
  expect((search as HTMLInputElement).value).toBe('')
  expect(screen.queryByText(/No recipes match/)).toBeNull()
})

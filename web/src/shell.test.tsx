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
  renderApp('/house-rules')
  await screen.findByRole('heading', { name: 'house rules' })
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

test('cook view shows unfinished feedback as disabled with a visible reason, not hidden or fake', async () => {
  renderApp('/cook')
  const button = (await screen.findByRole('button', { name: 'How did it go?' })) as HTMLButtonElement
  expect(button.disabled).toBe(true)
  expect(screen.getByText('Feedback after cooking — coming soon').id).toBe(button.getAttribute('aria-describedby'))
})

test('week and cook show the shared empty cards when nothing is planned', async () => {
  const router = renderApp('/week')
  await screen.findByRole('heading', { name: 'this week’s dinners' })
  // An open night (Tuesday) has no dinner to cook.
  await act(async () => { await router.navigate('/cook?day=2') })
  expect(await screen.findByRole('heading', { name: 'Nothing to cook tonight' })).toBeTruthy()
  expect(screen.getByRole('link', { name: 'This week →' }).getAttribute('href')).toBe('/week')
})

test('the loading skeleton has an explicit width, so the centring auth-state grid cannot shrink it to 0px', async () => {
  // Auth never resolves, so the protected shell stays on its loading state.
  const router = createMemoryRouter(
    [{
      path: '*',
      element: (
        <AuthProvider serviceFactory={() => new Promise(() => undefined)}>
          <PlannerProvider storage={null}><AppRoutes /></PlannerProvider>
        </AuthProvider>
      ),
    }],
    { initialEntries: ['/week'] },
  )
  render(<RouterProvider router={router} />)
  const region = await screen.findByRole('status', { name: 'Checking your fridge' })
  expect(region.getAttribute('aria-busy')).toBe('true')
  expect((region as HTMLElement).style.width).toBe('min(100%, 560px)')
  expect(region.closest('.auth-state')).not.toBeNull()
})

test('adding a recipe to the week replaces the stale "Saved." with the new status', async () => {
  const router = createMemoryRouter(
    [{
      path: '*',
      element: (
        <AuthProvider serviceFactory={() => Promise.resolve({
          getSession: vi.fn().mockResolvedValue(user),
          signInWithPassword: vi.fn(),
          sendPasswordResetCode: vi.fn(),
          setPassword: vi.fn(),
          signOut: vi.fn(),
        })}>
          <PlannerProvider storage={null}><AppRoutes /></PlannerProvider>
        </AuthProvider>
      ),
    }],
    { initialEntries: [{ pathname: '/recipes/red-lentil-coconut-soup', state: { saved: true } }] },
  )
  render(<RouterProvider router={router} />)
  expect(await screen.findByText('Saved.')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: /^Add to week/ }))
  expect(await screen.findByText('Planned Tuesday: Red lentil coconut soup')).toBeTruthy()
  expect(screen.queryByText('Saved.')).toBeNull()
})

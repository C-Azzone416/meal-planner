// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, expect, test, vi } from 'vitest'
import { AppRoutes } from './App'
import { AuthProvider } from './auth/AuthProvider'
import type { AuthService, AuthUser } from './auth/service'
import { PlannerProvider } from './planner/store'

const user: AuthUser = { id: 'user-test', email: 'allowed@example.test', emailVerified: true }

function makeService(): AuthService {
  return {
    getSession: vi.fn().mockResolvedValue(user),
    signInWithPassword: vi.fn().mockResolvedValue(user),
    sendPasswordResetCode: vi.fn().mockResolvedValue(undefined),
    setPassword: vi.fn().mockResolvedValue(user),
    signOut: vi.fn().mockResolvedValue(undefined),
  }
}

/** The recipe form uses useBlocker, which needs a data router (as the real App does). */
function renderApp(path: string, service = makeService()) {
  const router = createMemoryRouter(
    [{
      path: '*',
      element: (
        <AuthProvider serviceFactory={() => Promise.resolve(service)}>
          <PlannerProvider storage={null}>
            <AppRoutes />
          </PlannerProvider>
        </AuthProvider>
      ),
    }],
    { initialEntries: [path] },
  )
  render(<RouterProvider router={router} />)
  return { router, service }
}

afterEach(cleanup)

const change = (label: string | RegExp, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } })

function fillValidRecipe(title = 'Garlic noodles') {
  change(/^Title/, title)
  change(/^Active minutes/, '15')
  change(/^Total minutes/, '25')
  change(/^Name for ingredient 1/, 'spaghetti')
  change(/^Quantity for ingredient 1/, '1')
  change(/^Unit for ingredient 1/, 'lb')
  change(/^Step for cook step 1/, 'Boil the noodles and toss with garlic butter.')
}

test('opens a recipe from the list, shows every section, and back returns to the list', async () => {
  const { router } = renderApp('/recipes')
  fireEvent.click(await screen.findByRole('link', { name: 'Slow-cooker bean & sweet potato chili' }))

  expect(await screen.findByRole('heading', { level: 1, name: 'Slow-cooker bean & sweet potato chili' })).toBeTruthy()
  expect(router.state.location.pathname).toBe('/recipes/bean-sweet-potato-chili')
  for (const name of ['Prep ahead', 'Prep tonight', 'Cook tonight']) {
    expect(screen.getByRole('heading', { level: 2, name })).toBeTruthy()
  }
  expect(screen.getByRole('heading', { level: 2, name: /Ingredients/ })).toBeTruthy()
  expect(screen.getAllByText(/^Storage:/).length).toBeGreaterThan(0)
  expect(screen.getByText('On this week:').textContent).toContain('Wednesday')

  await router.navigate(-1)
  expect(await screen.findByRole('heading', { name: 'our recipes' })).toBeTruthy()
})

test('shows the "on Mon" badge from the plan on the list', async () => {
  renderApp('/recipes')
  expect(await screen.findByText('on Mon')).toBeTruthy()
})

test('loads a recipe by direct URL', async () => {
  renderApp('/recipes/salmon-lemon-herb-orzo')
  expect(await screen.findByRole('heading', { level: 1, name: 'Seared salmon, lemon-herb orzo' })).toBeTruthy()
  const link = screen.getByRole('link', { name: 'Thursday' })
  expect(link.getAttribute('href')).toBe('/week')
})

test('shows an in-page not-found for an unknown recipe, on detail and edit', async () => {
  const { router } = renderApp('/recipes/nope')
  expect(await screen.findByRole('heading', { name: 'Recipe not found' })).toBeTruthy()
  fireEvent.click(screen.getByRole('link', { name: 'Back to Recipes' }))
  expect(await screen.findByRole('heading', { name: 'our recipes' })).toBeTruthy()

  await router.navigate('/recipes/nope/edit')
  expect(await screen.findByRole('heading', { name: 'Recipe not found' })).toBeTruthy()
})

test('Add to week plans the first open night and is disabled with a reason when none is open', async () => {
  renderApp('/recipes/red-lentil-coconut-soup')
  await screen.findByRole('heading', { level: 1 })
  const button = () => screen.getByRole('button', { name: /^Add to week/ }) as HTMLButtonElement
  // Seed plan has Mon/Wed/Thu planned; Sunday is the prep day, so it goes last.
  // Order of the four open nights: Tue, Fri, Sat, then Sun.
  const order = ['Tuesday', 'Friday', 'Saturday', 'Sunday']
  for (const day of order) {
    expect(button().textContent).toContain(day)
    fireEvent.click(button())
  }
  await waitFor(() => expect(button().disabled).toBe(true))
  expect(screen.getByText(/Every night this week is already planned/)).toBeTruthy()
  expect(button().getAttribute('aria-describedby')).toBeTruthy()
})

test('adds a recipe and lands on its detail page with a Saved status', async () => {
  const { router } = renderApp('/recipes')
  fireEvent.click(await screen.findByRole('link', { name: 'Add recipe' }))
  expect(await screen.findByRole('heading', { name: 'add a recipe' })).toBeTruthy()

  fillValidRecipe()
  fireEvent.click(screen.getByRole('button', { name: '+ Add prep-ahead step' }))
  change(/^Step for prep-ahead step 1/, 'Mince the garlic')
  change(/^Keeps \(days\) for prep-ahead step 1/, '2')
  change(/^Storage note for prep-ahead step 1/, 'Fridge · 2 days')
  fireEvent.click(screen.getByRole('button', { name: 'Save recipe' }))

  expect(await screen.findByRole('heading', { level: 1, name: 'Garlic noodles' })).toBeTruthy()
  expect(router.state.location.pathname).toBe('/recipes/garlic-noodles')
  expect(within(screen.getAllByRole('status').find((el) => el.textContent?.includes('Saved'))!).getByText('Saved.')).toBeTruthy()
  expect(screen.getByText('Storage: Fridge · 2 days')).toBeTruthy()
  expect(screen.getByText('Mince the garlic')).toBeTruthy()

  fireEvent.click(screen.getByRole('link', { name: '← All recipes' }))
  expect(await screen.findByRole('link', { name: 'Garlic noodles' })).toBeTruthy()
})

test('reorders and removes rows', async () => {
  renderApp('/recipes/new')
  await screen.findByRole('heading', { name: 'add a recipe' })
  fireEvent.click(screen.getByRole('button', { name: '+ Add ingredient' }))
  change(/^Name for ingredient 1/, 'first')
  change(/^Name for ingredient 2/, 'second')
  fireEvent.click(screen.getByRole('button', { name: 'Move Ingredient 2 up' }))
  expect((screen.getByLabelText(/^Name for ingredient 1/) as HTMLInputElement).value).toBe('second')
  expect(document.activeElement?.getAttribute('aria-label')).toBe('Move Ingredient 1 down')
  fireEvent.click(screen.getByRole('button', { name: 'Remove Ingredient 1' }))
  expect((screen.getByLabelText(/^Name for ingredient 1/) as HTMLInputElement).value).toBe('first')
  expect(screen.queryByLabelText(/^Name for ingredient 2/)).toBeNull()
})

test('shows inline errors, an alert summary, and focuses the first invalid field', async () => {
  renderApp('/recipes/new')
  await screen.findByRole('heading', { name: 'add a recipe' })
  fireEvent.click(screen.getByRole('button', { name: 'Save recipe' }))

  const alert = await screen.findByRole('alert')
  expect(alert.textContent).toContain('problems to fix')
  const title = screen.getByLabelText(/^Title/)
  expect(title.getAttribute('aria-invalid')).toBe('true')
  expect(document.activeElement).toBe(title)
  expect(screen.getByText('Error: Title is required.')).toBeTruthy()
  expect(screen.getByText(/Add at least one ingredient/, { selector: 'p' })).toBeTruthy()
  expect(screen.getByText(/Add at least one Cook tonight step/, { selector: 'p' })).toBeTruthy()

  // Out-of-range values and re-validation as the user fixes things.
  change(/^Title/, 'x'.repeat(121))
  change(/^Servings/, '13')
  change(/^Active minutes/, '601')
  expect(screen.getByText(/Title must be 120 characters or fewer/, { selector: 'p' })).toBeTruthy()
  expect(screen.getByText(/Servings must be a whole number from 1 to 12/, { selector: 'p' })).toBeTruthy()
  expect(screen.getByText(/Active minutes must be a whole number from 0 to 600/, { selector: 'p' })).toBeTruthy()

  fillValidRecipe('Fixed it')
  change(/^Servings/, '2')
  expect(screen.queryByRole('alert')).toBeNull()
})

test('focuses a later invalid field when the title is fine', async () => {
  renderApp('/recipes/new')
  await screen.findByRole('heading', { name: 'add a recipe' })
  fillValidRecipe()
  change(/^Quantity for ingredient 1/, 'lots')
  fireEvent.click(screen.getByRole('button', { name: 'Save recipe' }))
  await screen.findByRole('alert')
  expect(document.activeElement).toBe(screen.getByLabelText(/^Quantity for ingredient 1/))
})

test('cancel with no changes leaves without asking', async () => {
  const { router } = renderApp('/recipes/new')
  await screen.findByRole('heading', { name: 'add a recipe' })
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(await screen.findByRole('heading', { name: 'our recipes' })).toBeTruthy()
  expect(router.state.location.pathname).toBe('/recipes')
})

test('cancel with unsaved changes asks to discard, and can keep editing', async () => {
  const { router } = renderApp('/recipes/new')
  await screen.findByRole('heading', { name: 'add a recipe' })
  change(/^Title/, 'Half-written')
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

  const dialog = await screen.findByRole('alertdialog', { name: 'Discard changes?' })
  expect(document.activeElement).toBe(within(dialog).getByRole('button', { name: 'Keep editing' }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Keep editing' }))
  expect(screen.queryByRole('alertdialog')).toBeNull()
  expect((screen.getByLabelText(/^Title/) as HTMLInputElement).value).toBe('Half-written')
  expect(router.state.location.pathname).toBe('/recipes/new')

  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  fireEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Discard' }))
  expect(await screen.findByRole('heading', { name: 'our recipes' })).toBeTruthy()
  expect(screen.queryByText('Half-written')).toBeNull()
})

test('leaving through the nav with unsaved changes asks first', async () => {
  const { router } = renderApp('/recipes/new')
  await screen.findByRole('heading', { name: 'add a recipe' })
  change(/^Title/, 'Half-written')
  fireEvent.click(screen.getAllByRole('link', { name: 'Shopping' })[0])
  fireEvent.keyDown(await screen.findByRole('alertdialog'), { key: 'Escape' })
  expect(router.state.location.pathname).toBe('/recipes/new')

  fireEvent.click(screen.getAllByRole('link', { name: 'Shopping' })[0])
  fireEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Discard' }))
  await waitFor(() => expect(router.state.location.pathname).toBe('/shopping'))
})

test('signing out with unsaved changes redirects without the discard prompt', async () => {
  const { router, service } = renderApp('/recipes/new')
  await screen.findByRole('heading', { name: 'add a recipe' })
  change(/^Title/, 'Half-written')
  fireEvent.click(screen.getByRole('button', { name: 'Sign out' }))
  await waitFor(() => expect(service.signOut).toHaveBeenCalled())
  expect(await screen.findByRole('heading', { name: 'what’s for dinner?' })).toBeTruthy()
  expect(screen.queryByRole('alertdialog')).toBeNull()
  expect(router.state.location.pathname).toBe('/sign-in')
})

test('editing a planned recipe saves, updates the plan and shows the notice', async () => {
  const { router } = renderApp('/recipes/salmon-lemon-herb-orzo')
  fireEvent.click(await screen.findByRole('link', { name: 'Edit' }))
  expect(await screen.findByRole('heading', { name: 'edit recipe' })).toBeTruthy()
  expect(screen.getByText(/This recipe is on this week \(Thursday\)/)).toBeTruthy()
  expect((screen.getByLabelText(/^Title/) as HTMLInputElement).value).toBe('Seared salmon, lemon-herb orzo')

  change(/^Title/, 'Seared salmon with orzo')
  change(/^Servings/, '6')
  fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))

  expect(await screen.findByRole('heading', { level: 1, name: 'Seared salmon with orzo' })).toBeTruthy()
  // The id is stable when the title changes.
  expect(router.state.location.pathname).toBe('/recipes/salmon-lemon-herb-orzo')
  expect(screen.getByText('Saved.')).toBeTruthy()
  expect(screen.getByText('This week’s shopping and prep were updated.')).toBeTruthy()
  expect(screen.getByRole('link', { name: 'Thursday' })).toBeTruthy()
})

test('editing an unplanned recipe does not claim the week changed', async () => {
  renderApp('/recipes/red-lentil-coconut-soup/edit')
  await screen.findByRole('heading', { name: 'edit recipe' })
  expect(screen.queryByText(/This recipe is on this week/)).toBeNull()
  change(/^Servings/, '3')
  fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  await screen.findByText('Saved.')
  expect(screen.queryByText(/shopping and prep were updated/)).toBeNull()
})

test('drafts are distinct, and Approve stays disabled until every guess is confirmed', async () => {
  const { router } = renderApp('/drafts')
  const card = (await screen.findByRole('article', { name: /Chicken thigh traybake with fennel/ }))
  expect(within(card).getByText('Draft · not saved')).toBeTruthy()
  expect(within(card).queryByLabelText('paw magnet')).toBeNull()

  const approve = within(card).getByRole('button', { name: 'Approve and save recipe' }) as HTMLButtonElement
  expect(approve.disabled).toBe(true)
  expect(within(card).getByText(/Check every guess to approve \(5 left\)/)).toBeTruthy()

  for (const button of within(card).getAllByRole('button', { name: /^Confirm / })) fireEvent.click(button)
  await waitFor(() => expect(approve.disabled).toBe(false))
  expect(within(card).getAllByRole('button', { name: /Undo confirmation/ })).toHaveLength(5)

  fireEvent.click(approve)
  expect(await screen.findByRole('heading', { level: 1, name: 'Chicken thigh traybake with fennel' })).toBeTruthy()
  expect(router.state.location.pathname).toBe('/recipes/chicken-thigh-traybake-with-fennel')
  expect(screen.getByText(/Saved “Chicken thigh traybake with fennel” to your recipes/)).toBeTruthy()

  await router.navigate('/drafts')
  expect(screen.queryByRole('article', { name: /Chicken thigh traybake/ })).toBeNull()
  await router.navigate('/recipes')
  expect(await screen.findByRole('link', { name: 'Chicken thigh traybake with fennel' })).toBeTruthy()
})

test('un-confirming a guess disables Approve again', async () => {
  renderApp('/drafts')
  const card = await screen.findByRole('article', { name: /Slow-cooker black bean/ })
  // One guess (total time) starts confirmed in the seed data.
  for (const button of within(card).getAllByRole('button', { name: /^Confirm / })) fireEvent.click(button)
  const approve = within(card).getByRole('button', { name: 'Approve and save recipe' }) as HTMLButtonElement
  await waitFor(() => expect(approve.disabled).toBe(false))
  fireEvent.click(within(card).getByRole('button', { name: /Undo confirmation of attention/ }))
  await waitFor(() => expect(approve.disabled).toBe(true))
})

test('discarding a draft asks first, then removes it; the nav count follows the store', async () => {
  renderApp('/drafts')
  const card = await screen.findByRole('article', { name: /Chicken thigh traybake with fennel/ })
  expect(screen.getAllByLabelText('2 drafts').length).toBeGreaterThan(0)

  fireEvent.click(within(card).getByRole('button', { name: 'Discard' }))
  const dialog = await screen.findByRole('alertdialog', { name: 'Discard this draft?' })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Keep draft' }))
  expect(screen.getByRole('article', { name: /Chicken thigh traybake/ })).toBeTruthy()

  fireEvent.click(within(card).getByRole('button', { name: 'Discard' }))
  fireEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Discard draft' }))
  await waitFor(() => expect(screen.queryByRole('article', { name: /Chicken thigh traybake/ })).toBeNull())
  expect(screen.getByText('Discarded “Chicken thigh traybake with fennel”.')).toBeTruthy()
  expect(screen.getAllByLabelText('1 draft').length).toBeGreaterThan(0)

  const remaining = screen.getByRole('article', { name: /Slow-cooker black bean/ })
  fireEvent.click(within(remaining).getByRole('button', { name: 'Discard' }))
  fireEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Discard draft' }))
  expect(await screen.findByRole('heading', { name: 'No drafts waiting' })).toBeTruthy()
  expect(screen.queryByLabelText(/drafts?$/)).toBeNull()
})

test('Recipes list: no-results state clears filters and refocuses search', async () => {
  renderApp('/recipes')
  const search = (await screen.findByLabelText('Search recipes')) as HTMLInputElement
  fireEvent.change(search, { target: { value: 'zzz' } })
  expect(await screen.findByRole('heading', { name: /No recipes match “zzz”/ })).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))
  expect(search.value).toBe('')
  expect(document.activeElement).toBe(search)
})

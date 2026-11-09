// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { AppRoutes } from './App'
import { AuthProvider } from './auth/AuthProvider'
import type { AuthService } from './auth/service'
import { accountStorageKey, parsePersisted } from './planner/persist'
import { createSeedState } from './planner/seed'
import type { NewRecipe } from './planner/types'
import type { RecipeEnvelope } from './recipes/api'

const id = '11111111-1111-4111-8111-111111111111'
const secondId = '22222222-2222-4222-8222-222222222222'
function record(title = 'Database dinner', revision = 1, recipeId = id): RecipeEnvelope {
  const { id: unused, ...content } = createSeedState().recipes[0]
  void unused
  return { id: recipeId, revision, status: 'approved', origin: 'manual', createdAt: '2026-11-08T00:00:00Z', updatedAt: '2026-11-08T00:00:00Z', content: { ...content, title } }
}
function json(value: unknown, status = 200) { return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } }) }
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
let database: RecipeEnvelope[] = []
const fetcher = vi.fn<typeof fetch>()
beforeEach(() => {
  database = []
  fetcher.mockReset().mockImplementation(async (_url, init) => {
    if (!init?.method) return json({ recipes: database, nextCursor: null })
    const body = JSON.parse(init.body as string) as { content: NewRecipe; expectedRevision?: number }
    const saved = { ...record(), content: body.content, revision: (body.expectedRevision ?? 0) + 1 }
    database = [saved]
    return json(saved)
  })
  vi.stubGlobal('fetch', fetcher)
})
afterEach(() => { cleanup(); localStorage.clear(); vi.unstubAllGlobals() })
function mount(path = '/recipes') {
  const user = { id: 'account-a', email: 'test@example.test', emailVerified: true }
  const service: AuthService = {
    getSession: vi.fn().mockResolvedValue(user), getAccessToken: vi.fn().mockResolvedValue('opaque-test-token'),
    signInWithPassword: vi.fn().mockResolvedValue(user), sendPasswordResetCode: vi.fn(), setPassword: vi.fn(), signOut: vi.fn(),
  }
  const factory = async () => service
  const router = createMemoryRouter([{ path: '*', element: <AuthProvider serviceFactory={factory}><AppRoutes /></AuthProvider> }], { initialEntries: [path] })
  const view = render(<RouterProvider router={router} />)
  return { ...view, router, service }
}
function fill() {
  const change = (label: RegExp, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } })
  change(/^Title/, 'My manual dinner'); change(/^Active minutes/, '10'); change(/^Total minutes/, '20')
  change(/^Name for ingredient 1/, 'rice'); change(/^Step for cook step 1/, 'Cook rice.')
}
const writes = () => fetcher.mock.calls.filter(([, init]) => init?.method)

test('does not seed or upload browser recipes; loads hosted data and distinguishes storage', async () => {
  database = [record()]
  mount()
  expect(await screen.findByRole('link', { name: 'Database dinner' })).toBeTruthy()
  expect(screen.getByText('Saved to account database')).toBeTruthy()
  expect(screen.getAllByText('Browser only · not uploaded').length).toBeGreaterThan(0)
  expect(writes()).toHaveLength(0)
  expect(fetcher.mock.calls[0][1]?.headers).toMatchObject({ Authorization: 'Bearer opaque-test-token' })
  const stored = localStorage.getItem(accountStorageKey('account-a'))!
  expect(stored).not.toContain('Database dinner')
})

test('waits for database commit, blocks repeated clicks, then reloads the committed recipe', async () => {
  const pending = deferred<Response>()
  const { router, unmount } = mount('/recipes/new')
  await screen.findByRole('heading', { name: 'add a recipe' }); fill()
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1))
  fetcher.mockImplementationOnce(() => pending.promise)
  const save = screen.getByRole('button', { name: 'Save recipe' })
  fireEvent.click(save); fireEvent.click(save)
  await waitFor(() => expect(writes()).toHaveLength(1))
  expect(router.state.location.pathname).toBe('/recipes/new')
  expect(screen.queryByText('Saved.')).toBeNull()
  expect((screen.getByRole('button', { name: 'Saving…' }) as HTMLButtonElement).disabled).toBe(true)
  database = [{ ...record(), content: JSON.parse(writes()[0][1]?.body as string).content }]
  await act(async () => pending.resolve(json(database[0])))
  expect(await screen.findByRole('heading', { name: 'My manual dinner' })).toBeTruthy()
  expect(screen.getByText('Saved.')).toBeTruthy()
  expect(localStorage.getItem(accountStorageKey('account-a'))).not.toContain('My manual dinner')
  unmount()
  mount(`/recipes/${id}`)
  expect(await screen.findByRole('heading', { name: 'My manual dinner' })).toBeTruthy()
})

test('unknown create outcome retains fields and retries the identical payload and idempotency key', async () => {
  mount('/recipes/new'); await screen.findByRole('heading', { name: 'add a recipe' }); fill()
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1))
  fetcher.mockRejectedValueOnce(new TypeError('Network failed'))
  fireEvent.click(screen.getByRole('button', { name: 'Save recipe' }))
  expect(await screen.findByRole('alert')).toBeTruthy()
  expect((screen.getByLabelText(/^Title/) as HTMLInputElement).value).toBe('My manual dinner')
  fireEvent.click(screen.getByRole('button', { name: 'Retry save' }))
  expect(await screen.findByRole('heading', { name: 'My manual dinner' })).toBeTruthy()
  expect(writes()).toHaveLength(2)
  expect(writes()[0][1]?.body).toBe(writes()[1][1]?.body)
  expect(writes()[0][1]?.headers).toEqual(writes()[1][1]?.headers)
  expect((writes()[0][1]?.headers as Record<string, string>)['Idempotency-Key']).toMatch(/^[a-f0-9-]{36}$/)
})

test('stale edit keeps the form and original revision, never navigates or silently overwrites', async () => {
  database = [record('Before', 4)]
  const { router } = mount(`/recipes/${id}/edit`)
  await screen.findByRole('heading', { name: 'edit recipe' })
  fireEvent.change(screen.getByLabelText(/^Title/), { target: { value: 'My edited dinner' } })
  fetcher.mockResolvedValueOnce(json({ error: { code: 'conflict' } }, 409))
  fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  expect((await screen.findByRole('alert')).textContent).toContain('changed elsewhere')
  expect((screen.getByLabelText(/^Title/) as HTMLInputElement).value).toBe('My edited dinner')
  expect(JSON.parse(writes()[0][1]?.body as string).expectedRevision).toBe(4)
  expect(router.state.location.pathname).toBe(`/recipes/${id}/edit`)
  expect(screen.queryByText('Saved.')).toBeNull()
  expect((screen.getByRole('button', { name: 'Save changes' }) as HTMLButtonElement).disabled).toBe(true)
})

test('existing browser recipe requires an explicit copy and preserves the original', async () => {
  mount('/recipes/gochujang-chicken/edit')
  await screen.findByRole('heading', { name: 'edit recipe' })
  expect(screen.getByText(/Saving creates a new database copy/)).toBeTruthy()
  expect(writes()).toHaveLength(0)
  fireEvent.change(screen.getByLabelText(/^Title/), { target: { value: 'Account copy' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save a copy to account' }))
  expect(await screen.findByRole('heading', { name: 'Account copy' })).toBeTruthy()
  expect(writes()[0][1]?.method).toBe('POST')
  const local = parsePersisted(localStorage.getItem(accountStorageKey('account-a'))!)!
  expect(local.recipes.find((recipe) => recipe.id === 'gochujang-chicken')?.title).not.toBe('Account copy')
})

test('failed remote reads show retry instead of false not-found; retry recovers direct links', async () => {
  fetcher.mockResolvedValueOnce(json({}, 503))
  mount(`/recipes/${id}`)
  expect(await screen.findByRole('button', { name: 'Retry loading recipes' })).toBeTruthy()
  expect(screen.queryByText('Recipe not found')).toBeNull()
  database = [record()]
  fireEvent.click(screen.getByRole('button', { name: 'Retry loading recipes' }))
  expect(await screen.findByRole('heading', { name: 'Database dinner' })).toBeTruthy()
})

test('sign-out during an in-flight write cannot navigate back or expose its response', async () => {
  const pending = deferred<Response>()
  const { router } = mount('/recipes/new')
  await screen.findByRole('heading', { name: 'add a recipe' }); fill()
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1))
  fetcher.mockImplementationOnce(() => pending.promise)
  fireEvent.click(screen.getByRole('button', { name: 'Save recipe' }))
  await waitFor(() => expect(writes()).toHaveLength(1))
  fireEvent.click(screen.getByRole('button', { name: 'Sign out' }))
  await waitFor(() => expect(router.state.location.pathname).toBe('/sign-in'))
  await act(async () => pending.resolve(json(record('Private late result'))))
  expect(router.state.location.pathname).toBe('/sign-in')
  expect(screen.queryByText('Private late result')).toBeNull()
})

test('hosted recipe planning survives a reload without copying hosted content into local storage', async () => {
  database = [record('Plan dinner', 1, secondId)]
  const first = mount(`/recipes/${secondId}`)
  fireEvent.click(await screen.findByRole('button', { name: /Add to week · Tuesday/ }))
  const raw = localStorage.getItem(accountStorageKey('account-a'))!
  expect(raw).not.toContain('Plan dinner')
  expect(parsePersisted(raw)?.plan.nights[2].recipeId).toBe(secondId)
  first.unmount()
  mount(`/recipes/${secondId}`)
  expect(await screen.findByText('Already on Tuesday')).toBeTruthy()
})

test('rejects overlong tags locally and allows correction after a definitive 400 response', async () => {
  mount('/recipes/new'); await screen.findByRole('heading', { name: 'add a recipe' }); fill()
  fireEvent.change(screen.getByLabelText('Tags'), { target: { value: 'x'.repeat(81) } })
  fireEvent.click(screen.getByRole('button', { name: 'Save recipe' }))
  expect(await screen.findByText('Error: Each tag must be 80 characters or fewer.')).toBeTruthy()
  expect(writes()).toHaveLength(0)
  fireEvent.change(screen.getByLabelText('Tags'), { target: { value: 'Dinner' } })
  fetcher.mockResolvedValueOnce(json({}, 400))
  fireEvent.click(screen.getByRole('button', { name: 'Save recipe' }))
  expect((await screen.findByRole('alert')).textContent).toContain('rejected without saving')
  expect((screen.getByLabelText(/^Title/) as HTMLInputElement).closest('fieldset')?.disabled).toBe(false)
  fireEvent.change(screen.getByLabelText(/^Title/), { target: { value: 'Corrected dinner' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save recipe' }))
  expect(await screen.findByRole('heading', { name: 'Corrected dinner' })).toBeTruthy()
  expect(writes()).toHaveLength(2)
  expect(writes()[0][1]?.body).not.toBe(writes()[1][1]?.body)
  expect((writes()[0][1]?.headers as Record<string, string>)['Idempotency-Key']).not.toBe((writes()[1][1]?.headers as Record<string, string>)['Idempotency-Key'])
})

test('the direct add form warns before submit when recipe requests are already failing, and still fails safe on submit', async () => {
  fetcher.mockResolvedValueOnce(json({}, 503))
  mount('/recipes/new')
  await screen.findByRole('heading', { name: 'add a recipe' })
  expect((await screen.findByText(/Saving is likely unavailable right now/)).getAttribute('role')).toBe('status')
  expect(screen.getByRole('button', { name: 'Retry loading recipes' })).toBeTruthy()
  expect(writes()).toHaveLength(0) // nothing is sent just by opening the form

  // Submitting is still attempted, and a failed write reports an unconfirmed save (no browser-local fallback).
  fill()
  fetcher.mockResolvedValueOnce(json({}, 503))
  fireEvent.click(screen.getByRole('button', { name: 'Save recipe' }))
  await waitFor(() => expect(screen.getAllByRole('alert').some((el) => /not been confirmed/.test(el.textContent ?? ''))).toBe(true))
  expect(writes()).toHaveLength(1)
  expect(localStorage.getItem(accountStorageKey('account-a')) ?? '').not.toContain('My manual dinner')
})

test('the add form shows no outage warning when recipe requests work', async () => {
  mount('/recipes/new')
  await screen.findByRole('heading', { name: 'add a recipe' })
  await waitFor(() => expect(fetcher).toHaveBeenCalled())
  expect(screen.queryByText(/Saving is likely unavailable/)).toBeNull()
})

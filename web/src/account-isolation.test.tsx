// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, test, vi } from 'vitest'

// Keep the existing browser-preview regression suite isolated. hosted-recipes.test.tsx
// exercises the authenticated database boundary and durability/failure behavior.
vi.mock('./recipes/RemoteRecipes', () => ({
  AccountRecipes: ({ children }: { children: import('react').ReactNode }) => children,
  useRemoteRecipes: () => null,
  RecipeLoadNotice: () => null,
}))

import { AppRoutes } from './App'
import { AuthProvider } from './auth/AuthProvider'
import type { AuthService, AuthUser } from './auth/service'
import { accountStorageKey, serialize } from './planner/persist'
import { createSeedState } from './planner/seed'

afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.clear() })
const user: AuthUser = { id: 'account-a', email: 'a@example.test', emailVerified: true }
function setup() {
  const state = createSeedState(); state.recipes[0].title = 'Private account A dinner'
  localStorage.setItem(accountStorageKey(user.id), serialize(state))
  const service: AuthService = {
    getSession: vi.fn().mockResolvedValue(user),
    signInWithPassword: vi.fn().mockResolvedValue(user),
    setPassword: vi.fn().mockResolvedValue(user),
    sendPasswordResetCode: vi.fn().mockResolvedValue(undefined),
    signOut: vi.fn().mockResolvedValue(undefined),
  }
  const serviceFactory = () => Promise.resolve(service)
  render(<MemoryRouter initialEntries={['/recipes']}><AuthProvider serviceFactory={serviceFactory}><AppRoutes /></AuthProvider></MemoryRouter>)
  return service
}

test('different authorized identity found on focus clears old account and requires sign-in', async () => {
  const service = setup()
  expect(await screen.findByText('Private account A dinner')).toBeTruthy()
  await act(async () => {})
  vi.mocked(service.getSession).mockResolvedValue({ ...user, id: 'account-b', email: 'b@example.test' })
  act(() => window.dispatchEvent(new Event('focus')))
  expect(await screen.findByRole('heading', { name: 'what’s for dinner?' })).toBeTruthy()
  expect(screen.queryByText('Private account A dinner')).toBeNull()
  expect(localStorage.getItem(accountStorageKey('account-b'))).toBeNull()
  expect(localStorage.getItem(accountStorageKey('account-a'))).toContain('Private account A dinner')
})

test('pending and failed remote sign-out immediately hide account data without erasing it', async () => {
  const service = setup()
  expect(await screen.findByText('Private account A dinner')).toBeTruthy()
  let reject!: (reason: Error) => void
  vi.mocked(service.signOut).mockImplementation(() => new Promise<void>((_resolve, fail) => { reject = fail }))
  fireEvent.click(screen.getByRole('button', { name: 'Sign out' }))
  expect(await screen.findByRole('heading', { name: 'what’s for dinner?' })).toBeTruthy()
  expect(screen.queryByText('Private account A dinner')).toBeNull()
  await act(async () => reject(new Error('offline')))
  await waitFor(() => expect(screen.getByText(/Remote sign-out could not be confirmed/)).toBeTruthy())
  expect(localStorage.getItem(accountStorageKey('account-a'))).toContain('Private account A dinner')
})

test('quota failure visibly warns on every protected page and retry restores accurate saved status', async () => {
  const setItem = vi.spyOn(Storage.prototype, 'setItem')
  const service = setup()
  await screen.findByText('Private account A dinner')
  setItem.mockImplementation(() => { throw new Error('quota exceeded') })
  fireEvent.click(screen.getByRole('link', { name: 'House rules' }))
  await screen.findByRole('heading', { name: 'house rules' })
  // A data change triggers a fresh storage write.
  fireEvent.click(screen.getByRole('button', { name: 'Increase dinners' }))
  expect(await screen.findByRole('button', { name: 'Retry saving' })).toBeTruthy()
  expect(screen.getByRole('alert').textContent).toContain('Changes are not saved')
  expect(screen.queryByText('Preview data · saved in this browser only')).toBeNull()
  setItem.mockRestore()
  fireEvent.click(screen.getByRole('button', { name: 'Retry saving' }))
  expect(await screen.findByText('Preview data · saved in this browser only')).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Retry saving' })).toBeNull()
  expect(service.signOut).not.toHaveBeenCalled()
})

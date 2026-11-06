// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, test, vi } from 'vitest'
import { AppRoutes } from './App'
import { AuthProvider } from './auth/AuthProvider'
import type { AuthService, AuthUser } from './auth/service'

const user: AuthUser = {
  id: 'user-test',
  email: 'allowed@example.test',
  emailVerified: true,
}

function makeService(session: AuthUser | null): AuthService {
  return {
    getSession: vi.fn().mockResolvedValue(session),
    sendCode: vi.fn().mockResolvedValue(undefined),
    verifyCode: vi.fn().mockResolvedValue(user),
    signOut: vi.fn().mockResolvedValue(undefined),
  }
}

function renderRoute(path: string, service: AuthService | null) {
  const serviceFactory = () => Promise.resolve(service)
  return render(
    <MemoryRouter initialEntries={[path]}>
      <AuthProvider serviceFactory={serviceFactory}>
        <AppRoutes />
      </AuthProvider>
    </MemoryRouter>,
  )
}

afterEach(cleanup)

test('redirects a signed-out direct URL to sign in', async () => {
  renderRoute('/recipes', makeService(null))
  expect(await screen.findByRole('heading', { name: 'what’s for dinner?' })).toBeTruthy()
})

test('renders protected sample views and working navigation', async () => {
  renderRoute('/week', makeService(user))
  expect(await screen.findByRole('heading', { name: 'this week’s dinners' })).toBeTruthy()
  expect(screen.getByText('Sample preview · changes on this screen reset when you reload.')).toBeTruthy()

  fireEvent.click(screen.getAllByRole('link', { name: 'Recipes' })[0])
  expect(await screen.findByRole('heading', { name: 'our recipes' })).toBeTruthy()
})

test('opens the cook view from the planned Thursday meal', async () => {
  renderRoute('/week', makeService(user))
  fireEvent.click(await screen.findByRole('link', { name: 'Cook tonight →' }))
  expect(await screen.findByRole('heading', { name: 'Seared salmon, lemon-herb orzo' })).toBeTruthy()
})

test('updates a house-rule preview choice without claiming persistence', async () => {
  renderRoute('/house-rules', makeService(user))
  const choice = await screen.findByRole('button', { name: 'One person does it all' })
  fireEvent.click(choice)
  expect(choice.getAttribute('aria-pressed')).toBe('true')
  expect(screen.getByText('Sample preview · changes on this screen reset when you reload.')).toBeTruthy()
})

test.each([
  ['/week', 'this week’s dinners'],
  ['/recipes', 'our recipes'],
  ['/prep', 'sunday prep'],
  ['/shopping', 'shopping list'],
  ['/drafts', 'drafts to review'],
  ['/cook', 'Seared salmon, lemon-herb orzo'],
  ['/house-rules', 'house rules'],
])('renders the protected direct route %s', async (path, heading) => {
  renderRoute(path, makeService(user))
  expect(await screen.findByRole('heading', { name: heading })).toBeTruthy()
})

test('completes the email-code flow and opens the requested view', async () => {
  const service = makeService(null)
  renderRoute('/sign-in', service)

  fireEvent.change(await screen.findByLabelText('Email'), {
    target: { value: 'allowed@example.test' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Email me a sign-in code' }))

  const code = await screen.findByLabelText('Sign-in code')
  expect(service.sendCode).toHaveBeenCalledWith('allowed@example.test')
  fireEvent.change(code, { target: { value: '123456' } })
  fireEvent.click(screen.getByRole('button', { name: 'Open my fridge' }))

  expect(await screen.findByRole('heading', { name: 'this week’s dinners' })).toBeTruthy()
  expect(service.verifyCode).toHaveBeenCalledWith('allowed@example.test', '123456')
})

test('fails closed when authentication is not configured', async () => {
  renderRoute('/sign-in', null)
  expect(await screen.findByText('Sign-in is not configured for this environment yet.')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Email me a sign-in code' }).hasAttribute('disabled')).toBe(true)
})

test('rejects an existing unverified session', async () => {
  const service = makeService({ ...user, emailVerified: false })
  renderRoute('/week', service)

  expect(await screen.findByRole('heading', { name: 'what’s for dinner?' })).toBeTruthy()
  expect(service.signOut).toHaveBeenCalled()
})

test('signs out and returns to the sign-in screen', async () => {
  const service = makeService(user)
  renderRoute('/week', service)
  fireEvent.click(await screen.findByRole('button', { name: 'Sign out' }))

  await waitFor(() => expect(service.signOut).toHaveBeenCalled())
  expect(await screen.findByRole('heading', { name: 'what’s for dinner?' })).toBeTruthy()
})

// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, test, vi } from 'vitest'
import { AppRoutes } from './App'
import { AuthProvider } from './auth/AuthProvider'
import { PlannerProvider } from './planner/store'
import {
  AuthCodeVerificationError,
  AuthCredentialsError,
  type AuthService,
  type AuthUser,
} from './auth/service'

const user: AuthUser = {
  id: 'user-test',
  email: 'allowed@example.test',
  emailVerified: true,
}

function makeService(session: AuthUser | null): AuthService {
  return {
    getSession: vi.fn().mockResolvedValue(session),
    signInWithPassword: vi.fn().mockResolvedValue(user),
    sendPasswordResetCode: vi.fn().mockResolvedValue(undefined),
    setPassword: vi.fn().mockResolvedValue(user),
    signOut: vi.fn().mockResolvedValue(undefined),
  }
}

function renderRoute(path: string, service: AuthService | null) {
  const serviceFactory = () => Promise.resolve(service)
  return render(
    <MemoryRouter initialEntries={[path]}>
      <AuthProvider serviceFactory={serviceFactory}>
        <PlannerProvider storage={null}>
          <AppRoutes />
        </PlannerProvider>
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
  expect(screen.getByText('Preview data · saved in this browser only')).toBeTruthy()

  fireEvent.click(screen.getAllByRole('link', { name: 'Recipes' })[0])
  expect(await screen.findByRole('heading', { name: 'our recipes' })).toBeTruthy()
})

test('opens the cook view from the planned Thursday meal', async () => {
  renderRoute('/week', makeService(user))
  fireEvent.click(await screen.findByRole('button', { name: 'Thursday' }))
  fireEvent.click(screen.getByRole('link', { name: 'Cook tonight →', description: 'Thursday' }))
  expect(await screen.findByRole('heading', { name: 'Seared salmon, lemon-herb orzo' })).toBeTruthy()
})

test('updates a house-rule preview choice without claiming persistence', async () => {
  renderRoute('/house-rules', makeService(user))
  const choice = await screen.findByRole('button', { name: 'One person does it all' })
  fireEvent.click(choice)
  expect(choice.getAttribute('aria-pressed')).toBe('true')
  expect(screen.getByText('Preview data · saved in this browser only')).toBeTruthy()
})

test.each([
  ['/week', 'this week’s dinners'],
  ['/recipes', 'our recipes'],
  ['/prep', 'sunday prep'],
  ['/shopping', 'shopping list'],
  ['/drafts', 'drafts to review'],
  ['/cook?day=4', 'Seared salmon, lemon-herb orzo'],
  ['/house-rules', 'house rules'],
])('renders the protected direct route %s', async (path, heading) => {
  renderRoute(path, makeService(user))
  expect(await screen.findByRole('heading', { name: heading })).toBeTruthy()
})

test('signs in with email and password', async () => {
  const service = makeService(null)
  renderRoute('/sign-in', service)

  fireEvent.change(await screen.findByLabelText('Email'), {
    target: { value: 'allowed@example.test' },
  })
  fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'private-password' } })
  fireEvent.click(screen.getByRole('button', { name: 'Open my fridge' }))

  expect(await screen.findByRole('heading', { name: 'this week’s dinners' })).toBeTruthy()
  expect(service.signInWithPassword).toHaveBeenCalledWith('allowed@example.test', 'private-password')
})

test('sets the first password for an existing invited account', async () => {
  const service = makeService(null)
  renderRoute('/sign-in', service)

  fireEvent.click(await screen.findByRole('button', { name: 'Set or reset password' }))
  fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'allowed@example.test' } })
  fireEvent.click(screen.getByRole('button', { name: 'Email my password code' }))

  expect(await screen.findByRole('heading', { name: 'choose your password' })).toBeTruthy()
  expect(screen.getByText('Enter the code from your email, then choose a new password.')).toBeTruthy()
  expect(service.sendPasswordResetCode).toHaveBeenCalledWith('allowed@example.test')

  fireEvent.change(screen.getByLabelText('Password code'), { target: { value: '123456' } })
  fireEvent.change(screen.getByLabelText('New password'), { target: { value: 'private-password' } })
  fireEvent.change(screen.getByLabelText('Confirm password'), { target: { value: 'private-password' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save password & open my fridge' }))

  expect(await screen.findByRole('heading', { name: 'this week’s dinners' })).toBeTruthy()
  expect(service.setPassword).toHaveBeenCalledWith('allowed@example.test', '123456', 'private-password')
})

test('uses privacy-safe copy when requesting a password code', async () => {
  const service = makeService(null)
  renderRoute('/sign-in', service)

  fireEvent.click(await screen.findByRole('button', { name: 'Set or reset password' }))
  expect(screen.getByText('We’ll email a password code if this invited account exists.')).toBeTruthy()
})

test('reports an invalid password code without exposing provider details', async () => {
  const service = makeService(null)
  vi.mocked(service.setPassword).mockRejectedValue(new AuthCodeVerificationError())
  renderRoute('/sign-in', service)

  fireEvent.click(await screen.findByRole('button', { name: 'Set or reset password' }))
  fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'allowed@example.test' } })
  fireEvent.click(screen.getByRole('button', { name: 'Email my password code' }))
  fireEvent.change(await screen.findByLabelText('Password code'), { target: { value: '123456' } })
  fireEvent.change(screen.getByLabelText('New password'), { target: { value: 'private-password' } })
  fireEvent.change(screen.getByLabelText('Confirm password'), { target: { value: 'private-password' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save password & open my fridge' }))

  expect((await screen.findByRole('alert')).textContent).toBe('That code is invalid or expired. Request a new code.')
})

test('reports invalid password credentials without exposing provider details', async () => {
  const service = makeService(null)
  vi.mocked(service.signInWithPassword).mockRejectedValue(new AuthCredentialsError())
  renderRoute('/sign-in', service)
  fireEvent.change(await screen.findByLabelText('Email'), { target: { value: 'allowed@example.test' } })
  fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'not-the-password' } })
  fireEvent.click(screen.getByRole('button', { name: 'Open my fridge' }))
  expect((await screen.findByRole('alert')).textContent).toBe('That email and password did not match.')
})

test('clears a sign-in error when switching to password recovery', async () => {
  const service = makeService(null)
  vi.mocked(service.signInWithPassword).mockRejectedValue(new AuthCredentialsError())
  renderRoute('/sign-in', service)
  fireEvent.change(await screen.findByLabelText('Email'), { target: { value: 'allowed@example.test' } })
  fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'not-the-password' } })
  fireEvent.click(screen.getByRole('button', { name: 'Open my fridge' }))
  await screen.findByRole('alert')

  fireEvent.click(screen.getByRole('button', { name: 'Set or reset password' }))
  expect(screen.queryByRole('alert')).toBeNull()
})

test('fails closed when authentication is not configured', async () => {
  renderRoute('/sign-in', null)
  expect(await screen.findByText('Sign-in is not configured for this environment yet.')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Open my fridge' }).hasAttribute('disabled')).toBe(true)
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

test('resets preview data only after confirmation', async () => {
  const confirm = vi.spyOn(window, 'confirm')
  renderRoute('/house-rules', makeService(user))
  const button = await screen.findByRole('button', { name: 'Reset preview data' })

  confirm.mockReturnValueOnce(false)
  fireEvent.click(button)
  expect(screen.queryByText('Preview data was reset.')).toBeNull()

  confirm.mockReturnValueOnce(true)
  fireEvent.click(button)
  expect((await screen.findByText('Preview data was reset.')).getAttribute('role')).toBe('status')
  confirm.mockRestore()
})

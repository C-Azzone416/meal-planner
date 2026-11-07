// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, test, vi } from 'vitest'
import { AppRoutes } from '../App'
import { PlannerProvider } from '../planner/store'
import { AuthProvider } from './AuthProvider'
import { AuthCredentialsError, type AuthService, type AuthUser } from './service'

const user: AuthUser = { id: 'u1', email: 'allowed@example.test', emailVerified: true }

function makeService(session: AuthUser | null): AuthService {
  return {
    getSession: vi.fn().mockResolvedValue(session),
    signInWithPassword: vi.fn().mockResolvedValue(user),
    sendPasswordResetCode: vi.fn().mockResolvedValue(undefined),
    setPassword: vi.fn().mockResolvedValue(user),
    signOut: vi.fn().mockResolvedValue(undefined),
  }
}

function renderRoute(path: string, service: AuthService) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <AuthProvider serviceFactory={() => Promise.resolve(service)}>
        <PlannerProvider storage={null}>
          <AppRoutes />
        </PlannerProvider>
      </AuthProvider>
    </MemoryRouter>,
  )
}

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

async function requestCode(email: string) {
  fireEvent.click(await screen.findByRole('button', { name: 'Set or reset password' }))
  fireEvent.change(screen.getByLabelText('Email'), { target: { value: email } })
  fireEvent.click(screen.getByRole('button', { name: 'Email my password code' }))
}

describe('password reset code request', () => {
  test.each(['allowed@example.test', 'stranger@nowhere.test'])('says the same thing for %s', async (email) => {
    renderRoute('/sign-in', makeService(null))
    await requestCode(email)
    expect((await screen.findByRole('status', { name: '' })).textContent).toBe('If that address can sign in, we sent a code.')
    expect(screen.queryByText(email)).toBeNull() // never echoes the address back
  })

  test('the resend button is disabled with a visible 60 second countdown', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'Date'] })
    renderRoute('/sign-in', makeService(null))
    await requestCode('allowed@example.test')
    const resend = await screen.findByRole('button', { name: 'Email me a new password code' }) as HTMLButtonElement
    expect(resend.disabled).toBe(true)
    expect(screen.getByText('You can request another code in 60 seconds.')).toBeTruthy()

    act(() => { vi.advanceTimersByTime(1000) })
    expect(screen.getByText('You can request another code in 59 seconds.')).toBeTruthy()
    act(() => { vi.advanceTimersByTime(58_000) })
    expect(screen.getByText('You can request another code in 1 second.')).toBeTruthy()
    expect(resend.disabled).toBe(true)

    act(() => { vi.advanceTimersByTime(1000) })
    expect(resend.disabled).toBe(false)
    expect(screen.queryByText(/You can request another code/)).toBeNull()
  })

  test('resending starts a fresh countdown', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'Date'] })
    const service = makeService(null)
    renderRoute('/sign-in', service)
    await requestCode('allowed@example.test')
    await screen.findByText('If that address can sign in, we sent a code.')
    act(() => { vi.advanceTimersByTime(60_000) })
    const resend = screen.getByRole('button', { name: 'Email me a new password code' })
    await act(async () => { fireEvent.click(resend) })
    expect(service.sendPasswordResetCode).toHaveBeenCalledTimes(2)
    expect((screen.getByRole('button', { name: 'Email me a new password code' }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText('You can request another code in 60 seconds.')).toBeTruthy()
  })

  test('going back and requesting again cannot skip the cooldown', async () => {
    const service = makeService(null)
    renderRoute('/sign-in', service)
    await requestCode('allowed@example.test')
    await screen.findByText('If that address can sign in, we sent a code.')
    fireEvent.click(screen.getByRole('button', { name: 'Back to sign in' }))
    fireEvent.click(screen.getByRole('button', { name: 'Set or reset password' }))
    // findBy: the route's pending flag may still be flushing, which briefly relabels the button 'Sending…'.
    expect(((await screen.findByRole('button', { name: 'Email my password code' })) as HTMLButtonElement).disabled).toBe(true)
    expect(service.sendPasswordResetCode).toHaveBeenCalledTimes(1)
  })
})

describe('sign-out and session expiry notices', () => {
  test('signing out returns to sign-in with "You\'re signed out."', async () => {
    renderRoute('/week', makeService(user))
    fireEvent.click(await screen.findByRole('button', { name: 'Sign out' }))
    expect((await screen.findByText('You\'re signed out.')).getAttribute('role')).toBe('status')
  })

  test('an ended session sends the person to sign-in with the expiry message, then back to the page they were on', async () => {
    const service = makeService(user)
    renderRoute('/recipes', service)
    expect(await screen.findByRole('heading', { name: 'our recipes' })).toBeTruthy()

    vi.mocked(service.getSession).mockResolvedValue(null)
    act(() => { window.dispatchEvent(new Event('focus')) })
    expect(await screen.findByText('Your session ended. Sign in again.')).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'what’s for dinner?' })).toBeTruthy()

    fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'allowed@example.test' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'private-password' } })
    fireEvent.click(screen.getByRole('button', { name: 'Open my fridge' }))
    expect(await screen.findByRole('heading', { name: 'our recipes' })).toBeTruthy()
    expect(screen.queryByText('Your session ended. Sign in again.')).toBeNull()
  })

  test('a failed session check (offline, server error) does not sign the person out', async () => {
    const service = makeService(user)
    renderRoute('/week', service)
    await screen.findByRole('heading', { name: 'this week’s dinners' })
    vi.mocked(service.getSession).mockRejectedValue(new Error('network down'))
    act(() => { window.dispatchEvent(new Event('focus')) })
    await waitFor(() => expect(service.getSession).toHaveBeenCalledTimes(2))
    expect(screen.getByRole('heading', { name: 'this week’s dinners' })).toBeTruthy()
    expect(screen.queryByText('Your session ended. Sign in again.')).toBeNull()
  })

  test('the session is also re-checked when the tab becomes visible', async () => {
    const service = makeService(user)
    renderRoute('/week', service)
    await screen.findByRole('heading', { name: 'this week’s dinners' })
    vi.mocked(service.getSession).mockResolvedValue(null)
    act(() => { document.dispatchEvent(new Event('visibilitychange')) })
    expect(await screen.findByText('Your session ended. Sign in again.')).toBeTruthy()
  })

  test('wrong credentials still use the generic copy and keep no stale notice', async () => {
    const service = makeService(user)
    renderRoute('/week', service)
    fireEvent.click(await screen.findByRole('button', { name: 'Sign out' }))
    await screen.findByText('You\'re signed out.')
    vi.mocked(service.signInWithPassword).mockRejectedValue(new AuthCredentialsError())
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'allowed@example.test' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'nope' } })
    fireEvent.click(screen.getByRole('button', { name: 'Open my fridge' }))
    expect((await screen.findByRole('alert')).textContent).toBe('That email and password did not match.')
  })
})

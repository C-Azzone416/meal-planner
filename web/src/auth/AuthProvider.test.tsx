// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { AuthProvider, useAuth } from './AuthProvider'
import { AuthSessionEstablishmentError, type AuthService } from './service'
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})
function Probe() {
  const auth = useAuth()
  return (
    <>
      <p>{auth.status}</p>
      <button onClick={auth.expireSession}>Expire from recipe request</button>
      <button onClick={() => void auth.signOut()}>Sign out directly</button>
      <button onClick={() => void auth.getToken().catch(() => {})}>
        Check feedback token
      </button>
    </>
  )
}
test.each([
  ['transient', new Error('network'), 'signed-in', 0],
  ['expired', new AuthSessionEstablishmentError(), 'signed-out', 1],
] as const)(
  '%s token failures preserve or clear the session correctly',
  async (_name, error, status, count) => {
    const ended = vi.fn()
    window.addEventListener('feedback-session-ended', ended)
    const service: AuthService = {
      getSession: vi.fn().mockResolvedValue({
        id: 'one',
        email: 'test@example.test',
        emailVerified: true,
      }),
      getToken: vi.fn().mockRejectedValue(error),
      signInWithPassword: vi.fn(),
      sendPasswordResetCode: vi.fn(),
      setPassword: vi.fn(),
      signOut: vi.fn(),
    }
    render(
      <AuthProvider serviceFactory={async () => service}>
        <Probe />
      </AuthProvider>,
    )
    await screen.findByText('signed-in')
    fireEvent.click(screen.getByText('Check feedback token'))
    await waitFor(() => expect(service.getToken).toHaveBeenCalledOnce())
    await screen.findByText(status)
    expect(ended).toHaveBeenCalledTimes(count)
    window.removeEventListener('feedback-session-ended', ended)
  },
)

test.each(['Expire from recipe request', 'Sign out directly'])(
  '%s clears feedback alongside recipe account data',
  async (action) => {
    const ended = vi.fn()
    window.addEventListener('feedback-session-ended', ended)
    const service: AuthService = {
      getSession: vi
        .fn()
        .mockResolvedValue({
          id: 'one',
          email: 'test@example.test',
          emailVerified: true,
        }),
      getAccessToken: vi.fn().mockResolvedValue('token'),
      signInWithPassword: vi.fn(),
      sendPasswordResetCode: vi.fn(),
      setPassword: vi.fn(),
      signOut: vi.fn().mockResolvedValue(undefined),
    }
    render(
      <AuthProvider serviceFactory={async () => service}>
        <Probe />
      </AuthProvider>,
    )
    await screen.findByText('signed-in')
    fireEvent.click(screen.getByText(action))
    await screen.findByText('signed-out')
    expect(ended).toHaveBeenCalledOnce()
    window.removeEventListener('feedback-session-ended', ended)
  },
)

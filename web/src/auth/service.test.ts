import { expect, test, vi } from 'vitest'
import { isInvalidOrExpiredCodeError, loadAuthService } from './service'

function fetchResponse(body: unknown, status = 200): typeof fetch {
  return vi.fn(async () => new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })) as unknown as typeof fetch
}

test('keeps authentication disabled when the server policy is disabled', async () => {
  await expect(loadAuthService(fetchResponse({ enabled: false }))).resolves.toBeNull()
})

test('fails closed when enabled configuration omits its auth URL', async () => {
  await expect(loadAuthService(fetchResponse({ enabled: true }))).rejects.toThrow(
    'Sign-in configuration is incomplete.',
  )
})

test('rejects a non-HTTPS auth endpoint', async () => {
  await expect(loadAuthService(fetchResponse({
    enabled: true,
    baseUrl: 'http://auth.example.invalid',
  }), 'https://meal.example.test')).rejects.toThrow('Sign-in configuration is invalid.')
})

test('accepts only the same-origin auth proxy', async () => {
  await expect(loadAuthService(fetchResponse({
    enabled: true,
    baseUrl: '/api/auth-proxy',
  }), 'https://meal.example.test')).resolves.not.toBeNull()
})

test('rejects a cross-origin HTTPS auth endpoint', async () => {
  await expect(loadAuthService(fetchResponse({
    enabled: true,
    baseUrl: 'https://auth.example.invalid/example/auth',
  }), 'https://meal.example.test')).rejects.toThrow('Sign-in configuration is invalid.')
})

test('fails closed when the public configuration endpoint fails', async () => {
  await expect(loadAuthService(fetchResponse({ message: 'unavailable' }, 503))).rejects.toThrow(
    'Sign-in configuration is unavailable.',
  )
})

test.each([
  [{ code: 'INVALID_OTP' }, true],
  [{ body: { code: 'OTP_EXPIRED' } }, true],
  [{ code: 'INTERNAL_SERVER_ERROR' }, false],
  [{ status: 502, message: 'unavailable' }, false],
  [null, false],
])('classifies only provider OTP failures as invalid or expired', (error, expected) => {
  expect(isInvalidOrExpiredCodeError(error)).toBe(expected)
})

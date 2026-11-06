import { afterEach, expect, test, vi } from 'vitest'
import { isInvalidOrExpiredCodeError, loadAuthService } from './service'

function fetchResponse(body: unknown, status = 200): typeof fetch {
  return vi.fn(async () => new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })) as unknown as typeof fetch
}

afterEach(() => {
  vi.unstubAllGlobals()
})

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
  [{ code: 'validation_failed', error: { code: 'INVALID_OTP' } }, true],
  [{ code: 'INTERNAL_SERVER_ERROR' }, false],
  [{ status: 502, message: 'unavailable' }, false],
  [null, false],
])('classifies only provider OTP failures as invalid or expired', (error, expected) => {
  expect(isInvalidOrExpiredCodeError(error)).toBe(expected)
})

test('authorizes with the JWT injected into a fresh real-adapter session', async () => {
  const providerRequests: string[] = []
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    providerRequests.push(url)
    if (url.endsWith('/sign-in/email-otp')) {
      return new Response(JSON.stringify({
        token: 'opaque-session-token',
        user: { id: 'user-test', email: 'allowed@example.test', emailVerified: true },
      }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    }
    if (url.endsWith('/get-session')) {
      return new Response(JSON.stringify({
        session: { id: 'session-test', token: 'opaque-session-token', userId: 'user-test' },
        user: { id: 'user-test', email: 'allowed@example.test', emailVerified: true },
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json', 'Set-Auth-Jwt': 'signed-api-jwt' },
      })
    }
    throw new Error(`unexpected provider request: ${url}`)
  }))

  let authorization = ''
  const appFetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input) === '/api/auth-config') {
      return new Response(JSON.stringify({ enabled: true, baseUrl: '/api/auth-proxy' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    if (String(input) === '/api/auth/authorize') {
      authorization = new Headers(init?.headers).get('Authorization') ?? ''
      return new Response(null, { status: 204 })
    }
    throw new Error(`unexpected app request: ${String(input)}`)
  }) as unknown as typeof fetch

  const service = await loadAuthService(appFetch, 'https://meal.example.test')
  expect(service).not.toBeNull()
  await expect(service!.verifyCode('allowed@example.test', '123456')).resolves.toMatchObject({
    id: 'user-test',
    email: 'allowed@example.test',
  })
  expect(authorization).toBe('Bearer signed-api-jwt')
  expect(providerRequests.some((url) => url.endsWith('/get-session'))).toBe(true)
  expect(providerRequests.some((url) => url.endsWith('/token'))).toBe(false)
})

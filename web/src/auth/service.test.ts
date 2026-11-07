import { afterEach, expect, test, vi } from 'vitest'
import {
  AuthCodeVerificationError,
  AuthCredentialsError,
  isInvalidOrExpiredCodeError,
  loadAuthService,
} from './service'

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

test('password sign-in authorizes with the JWT injected into a fresh real-adapter session', async () => {
  const providerRequests: string[] = []
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    providerRequests.push(url)
    if (url.endsWith('/sign-in/email')) {
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
  await expect(service!.signInWithPassword('allowed@example.test', 'private-password')).resolves.toMatchObject({
    id: 'user-test',
    email: 'allowed@example.test',
  })
  expect(authorization).toBe('Bearer signed-api-jwt')
  expect(providerRequests.some((url) => url.endsWith('/get-session'))).toBe(true)
  expect(providerRequests.some((url) => url.endsWith('/revoke-other-sessions'))).toBe(false)
  expect(providerRequests.some((url) => url.endsWith('/token'))).toBe(false)
})
test('maps the proxy synthetic blocked sign-in response through the real adapter', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.endsWith('/sign-in/email')) {
      return new Response(JSON.stringify({
        code: 'INVALID_EMAIL_OR_PASSWORD',
        message: 'Invalid email or password',
      }), { status: 401, headers: { 'Content-Type': 'application/json' } })
    }
    throw new Error(`unexpected provider request: ${url}`)
  }))

  const service = await loadAuthService(fetchResponse({
    enabled: true,
    baseUrl: '/api/auth-proxy',
  }), 'https://meal.example.test')

  await expect(service!.signInWithPassword('blocked@example.test', 'not-the-password'))
    .rejects.toBeInstanceOf(AuthCredentialsError)
})

test('maps the proxy synthetic blocked reset response through the real adapter', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.endsWith('/email-otp/reset-password')) {
      return new Response(JSON.stringify({ code: 'INVALID_OTP', message: 'Invalid OTP' }), {
        status: 400,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    throw new Error(`unexpected provider request: ${url}`)
  }))

  const service = await loadAuthService(fetchResponse({
    enabled: true,
    baseUrl: '/api/auth-proxy',
  }), 'https://meal.example.test')

  await expect(service!.setPassword('blocked@example.test', '000000', 'private-password'))
    .rejects.toBeInstanceOf(AuthCodeVerificationError)
})

test('sets an existing user password, signs in, and revokes older sessions', async () => {
  const providerRequests: string[] = []
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    providerRequests.push(url)
    if (url.endsWith('/email-otp/reset-password')) {
      return new Response(JSON.stringify({ success: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    if (url.endsWith('/sign-in/email')) {
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
    if (url.endsWith('/revoke-other-sessions')) {
      return new Response(JSON.stringify({ status: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    throw new Error(`unexpected provider request: ${url}`)
  }))

  const appFetch = vi.fn(async (input: RequestInfo | URL) => {
    if (String(input) === '/api/auth-config') {
      return new Response(JSON.stringify({ enabled: true, baseUrl: '/api/auth-proxy' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    if (String(input) === '/api/auth/authorize') return new Response(null, { status: 204 })
    throw new Error(`unexpected app request: ${String(input)}`)
  }) as unknown as typeof fetch

  const service = await loadAuthService(appFetch, 'https://meal.example.test')
  await expect(service!.setPassword('allowed@example.test', '654321', 'private-password')).resolves.toMatchObject({
    id: 'user-test',
  })
  for (const path of ['/email-otp/reset-password', '/sign-in/email', '/get-session', '/revoke-other-sessions']) {
    expect(providerRequests.some((url) => url.endsWith(path))).toBe(true)
  }
  expect(providerRequests.some((url) => url.endsWith('/token'))).toBe(false)
})

test('classifies only unanswered requests as network failures', async () => {
  const { isNetworkFailure } = await import('./service')
  expect(isNetworkFailure(new TypeError('Failed to fetch'))).toBe(true)
  expect(isNetworkFailure({ status: 0 })).toBe(true)
  expect(isNetworkFailure({ status: 503 })).toBe(true)
  expect(isNetworkFailure({ code: 'NETWORK_ERROR' })).toBe(true)
  expect(isNetworkFailure(new Error('NetworkError when attempting to fetch resource.'))).toBe(true)
  expect(isNetworkFailure({ status: 400, code: 'USER_NOT_FOUND' })).toBe(false)
  expect(isNetworkFailure({ status: 429 })).toBe(false)
  expect(isNetworkFailure(new Error('Unknown email'))).toBe(false)
  expect(isNetworkFailure(undefined)).toBe(false)
})

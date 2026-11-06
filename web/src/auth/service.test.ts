import { expect, test, vi } from 'vitest'
import { loadAuthService } from './service'

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
  }))).rejects.toThrow('Sign-in configuration is invalid.')
})

test('fails closed when the public configuration endpoint fails', async () => {
  await expect(loadAuthService(fetchResponse({ message: 'unavailable' }, 503))).rejects.toThrow(
    'Sign-in configuration is unavailable.',
  )
})

// @vitest-environment jsdom
import { afterEach, expect, test, vi } from 'vitest'
import { IDBFactory } from 'fake-indexeddb'
import { OfflineQueue } from '@masra91/framebriar/browser/queue'
import { prepareLocalFeedback } from '@masra91/framebriar/browser/local-export'
import { createDurableFeedback } from '@masra91/framebriar/browser/durable-feedback'
import {
  clearKnownScope,
  enabled,
  reconcileScope,
  request,
  transport,
} from './client'
vi.mock('./config', () => ({
  feedbackConfig: {
    enabled: true,
    environment: 'preview',
    origin: 'https://meal.test',
    appId: 'meal-preview',
    buildSha: 'a'.repeat(40),
    screenshots: true,
  },
}))
afterEach(() => {
  vi.unstubAllGlobals()
  localStorage.clear()
})
const report = {
  v: 1 as const,
  id: '58c847b1-cb12-4270-bbe0-bbf64f7d993a',
  gameId: 'meal-preview',
  buildSha: 'a'.repeat(40),
  scene: 'week',
  sessionId: 'f5a6ab88-88d0-4998-a071-ac09ddcb6b85',
  seed: null,
  platform: 'mobile',
  input: 'touch',
  capturedAt: new Date().toISOString(),
  notes: 'Synthetic note',
  breadcrumbs: [],
  media: null,
}
const response = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status })
test('requires exact allowed origin', () => {
  expect(enabled('https://meal.test')).toBe(true)
  expect(enabled('https://random.vercel.app')).toBe(false)
})
test.each([401, 403, 429, 500])(
  'safe errors for %s, including non JSON auth',
  async (status) => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(new Response('private error', { status }))
    await expect(
      request('status', async () => 'fresh', undefined, undefined, fetcher),
    ).rejects.toThrow(
      status === 429
        ? 'quota'
        : status < 500
          ? 'access-required'
          : 'unavailable',
    )
  },
)
test('uses fresh session token, own route and receipt identity', async () => {
  const fetcher = vi
      .fn()
      .mockResolvedValue(
        response({ id: report.id, gameId: report.gameId, status: 'received' }),
      ),
    token = vi.fn().mockResolvedValue('fresh-session')
  await expect(
    transport(token, () => true, fetcher)({ report, media: null }),
  ).resolves.toMatchObject({ status: 'processed' })
  expect(token).toHaveBeenCalledOnce()
  expect(fetcher.mock.calls[0][0]).toBe('/api/feedback/reports')
  const options = fetcher.mock.calls[0][1]
  expect(options.headers.Authorization).toBe('Bearer fresh-session')
  expect(options.redirect).toBe('error')
  expect(options.body.get('report').type).toBe('application/json')
})
test.each([
  { status: 'saved-local' },
  { status: 'received', id: 'wrong' },
  { status: 'received', gameId: 'other' },
])('never marks malformed receipt received %o', async (change) => {
  await expect(
    transport(
      async () => 'token',
      () => true,
      vi
        .fn()
        .mockResolvedValue(
          response(Object.assign({ ...report, status: 'received' }, change)),
        ),
    )({ report, media: null }),
  ).rejects.toThrow('invalid-ack')
})
test('session change while fetching fresh token prevents request', async () => {
  let valid = true
  const fetcher = vi.fn()
  await expect(
    transport(
      async () => {
        valid = false
        return 'token'
      },
      () => valid,
      fetcher,
    )({ report, media: null }),
  ).rejects.toThrow('access-required')
  expect(fetcher).not.toHaveBeenCalled()
})
test('rejects media and JSON exceeding lower hosted limits', async () => {
  const fetcher = vi.fn(),
    send = transport(
      async () => 'token',
      () => true,
      fetcher,
    )
  await expect(
    send({
      report,
      media: new Blob([new Uint8Array(3 * 1024 * 1024 + 1)], {
        type: 'image/png',
      }),
    }),
  ).rejects.toThrow('invalid-input')
  await expect(
    send({ report: { ...report, notes: 'x'.repeat(16384) }, media: null }),
  ).rejects.toThrow('invalid-input')
  expect(fetcher).not.toHaveBeenCalled()
})
test('SDK persists one immutable ID, restores after reload, and isolated scopes clear before switching', async () => {
  vi.stubGlobal('indexedDB', new IDBFactory())
  const first = new OfflineQueue('account-one'),
    second = new OfflineQueue('account-two')
  const queue = createDurableFeedback({
    gameId: report.gameId,
    buildSha: report.buildSha,
    endpoint: 'https://meal.test/api/feedback/reports',
    queue: first,
    getToken: async () => undefined,
    onChange: () => {},
    onReceipt: () => {},
    send: async () => {
      throw Error('offline')
    },
  })
  const draft = await prepareLocalFeedback({
    blob: null,
    notes: report.notes,
    captureContext: report,
  })
  await queue.enqueue({ ...draft, includeDiagnostics: false })
  await queue.enqueue({ ...draft, includeDiagnostics: false })
  expect(await new OfflineQueue('account-one').all()).toHaveLength(1)
  expect(await second.all()).toHaveLength(0)
  await reconcileScope('account-one')
  await reconcileScope('account-two')
  expect(await first.all()).toHaveLength(0)
  queue.stop()
})
test('storage unavailable refuses scope transition without claiming save', async () => {
  vi.stubGlobal('indexedDB', undefined)
  localStorage.setItem('meal-feedback-scope-preview', 'old')
  await expect(reconcileScope('new')).rejects.toBeTruthy()
  expect(localStorage.getItem('meal-feedback-scope-preview')).toBe('old')
})

test('a stale account clears only its captured scope, never another tab pointer', async () => {
  vi.stubGlobal('indexedDB', new IDBFactory())
  const other = new OfflineQueue('account-b')
  const outbox = createDurableFeedback({
    gameId: report.gameId,
    buildSha: report.buildSha,
    endpoint: 'https://meal.test/api/feedback/reports',
    queue: other,
    getToken: async () => undefined,
    onChange: () => {},
    onReceipt: () => {},
    send: async () => {
      throw Error('offline')
    },
  })
  await outbox.enqueue({ report, media: null, includeDiagnostics: false })
  localStorage.setItem('meal-feedback-scope-preview', 'account-b')
  await clearKnownScope('account-a')
  expect(await other.all()).toHaveLength(1)
  expect(localStorage.getItem('meal-feedback-scope-preview')).toBe('account-b')
  outbox.stop()
})

test('no initialized feedback scope does not require local storage to sign out', async () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
    throw new DOMException('blocked', 'SecurityError')
  })
  await expect(clearKnownScope('')).resolves.toBeUndefined()
  vi.restoreAllMocks()
})

test('SDK storage cap refuses the eleventh report without evicting drafts', async () => {
  vi.stubGlobal('indexedDB', new IDBFactory())
  const queue = new OfflineQueue('bounded-account')
  const outbox = createDurableFeedback({
    gameId: report.gameId,
    buildSha: report.buildSha,
    endpoint: 'https://meal.test/api/feedback/reports',
    queue,
    getToken: async () => undefined,
    onChange: () => {},
    onReceipt: () => {},
    send: async () => {
      throw Error('offline')
    },
  })
  for (let i = 0; i < 10; i++) {
    await outbox.enqueue({
      report: { ...report, id: crypto.randomUUID() },
      media: null,
      includeDiagnostics: false,
    })
  }
  await expect(
    outbox.enqueue({
      report: { ...report, id: crypto.randomUUID() },
      media: null,
      includeDiagnostics: false,
    }),
  ).rejects.toMatchObject({ code: 'storage-full' })
  expect(await queue.all()).toHaveLength(10)
  outbox.stop()
})

test('an aborted save never commits to the SDK queue', async () => {
  vi.stubGlobal('indexedDB', new IDBFactory())
  const queue = new OfflineQueue('cancelled-account')
  const outbox = createDurableFeedback({
    gameId: report.gameId,
    buildSha: report.buildSha,
    endpoint: 'https://meal.test/api/feedback/reports',
    queue,
    getToken: async () => undefined,
    onChange: () => {},
    onReceipt: () => {},
    send: async () => {
      throw Error('offline')
    },
  })
  const controller = new AbortController()
  controller.abort()
  await expect(
    outbox.enqueue({
      report,
      media: null,
      includeDiagnostics: false,
      signal: controller.signal,
    }),
  ).rejects.toMatchObject({ name: 'AbortError' })
  expect(await queue.all()).toHaveLength(0)
  outbox.stop()
})

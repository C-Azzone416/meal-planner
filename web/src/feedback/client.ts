import {
  createDurableFeedback,
  type Delivery,
  type OutboxStatus,
  type Receipt,
} from '@masra91/framebriar/browser/durable-feedback'
import { OfflineQueue } from '@masra91/framebriar/browser/queue'
import { feedbackConfig } from './config'

export type Connection = {
  status: string
  outboxKey?: string
  expiresAt?: string
  environment?: string
  appId?: string
  buildSha?: string
  screenshots?: boolean
}
export function enabled(origin = location.origin) {
  return (
    feedbackConfig.enabled &&
    feedbackConfig.origin === origin &&
    (/^https:\/\//.test(origin) ||
      (import.meta.env.DEV &&
        /^http:\/\/(localhost|127\.0\.0\.1)(:\d+)?$/.test(origin))) &&
    !!feedbackConfig.environment &&
    /^[a-f0-9]{40}$/.test(feedbackConfig.buildSha)
  )
}
export class FeedbackError extends Error {
  code: string
  constructor(code: string) {
    super(code)
    this.code = code
  }
}
export async function request(
  path: string,
  token: () => Promise<string>,
  body?: BodyInit,
  signal?: AbortSignal,
  fetcher: typeof fetch = fetch,
) {
  const bearer = await token()
  if (signal?.aborted) throw new FeedbackError('cancelled')
  const response = await fetcher(`/api/feedback/${path}`, {
    method: body === undefined ? 'GET' : 'POST',
    credentials: 'same-origin',
    redirect: 'error',
    headers: {
      Authorization: `Bearer ${bearer}`,
      ...(typeof body === 'string'
        ? { 'Content-Type': 'application/json' }
        : {}),
    },
    body,
    signal: signal
      ? AbortSignal.any([signal, AbortSignal.timeout(20000)])
      : AbortSignal.timeout(20000),
  })
  if (!response.ok)
    throw new FeedbackError(
      response.status === 401 || response.status === 403
        ? 'access-required'
        : response.status === 429
          ? 'quota'
          : 'unavailable',
    )
  return response.json()
}
export function transport(
  token: () => Promise<string>,
  valid: () => boolean,
  fetcher: typeof fetch = fetch,
) {
  return async ({ report, media, signal }: Delivery): Promise<Receipt> => {
    if (!valid()) throw new FeedbackError('access-required')
    const form = new FormData()
    const json = new Blob([JSON.stringify(report)], {
      type: 'application/json',
    })
    if (
      json.size > 16384 ||
      (media && (media.size > 3 * 1024 * 1024 || media.type !== 'image/png'))
    )
      throw new FeedbackError('invalid-input')
    form.append('report', json, 'report.json')
    if (media) form.append('media', media, 'screenshot.png')
    const ack = await request(
      'reports',
      async () => {
        const bearer = await token()
        if (!valid()) throw new FeedbackError('access-required')
        return bearer
      },
      form,
      signal,
      fetcher,
    )
    if (
      !valid() ||
      ack.status !== 'received' ||
      ack.id !== report.id ||
      ack.gameId !== report.gameId
    )
      throw new FeedbackError('invalid-ack')
    // SDK 0.4.1 recognizes ready/processed; the Go receipt is the sole mapping.
    return { ...ack, status: 'processed' }
  }
}
export function makeOutbox(
  scope: string,
  token: () => Promise<string>,
  valid: () => boolean,
  onChange: (value: OutboxStatus) => void,
  onReceipt: (receipt: Receipt) => void,
) {
  // The SDK validates an HTTPS identifier even with an injected transport.
  // DEV-only localhost fixtures still send solely through the relative Go route.
  const sdkOrigin = import.meta.env.DEV
    ? feedbackConfig.origin.replace(
        /^http:\/\/(localhost|127\.0\.0\.1)(?=:|$)/,
        'https://$1',
      )
    : feedbackConfig.origin
  return createDurableFeedback({
    gameId: feedbackConfig.appId,
    buildSha: feedbackConfig.buildSha,
    endpoint: `${sdkOrigin}/api/feedback/reports`,
    queue: new OfflineQueue(scope),
    getToken: token,
    send: transport(token, valid),
    onChange,
    onReceipt,
  })
}
export async function inspectScope(scope: string): Promise<OutboxStatus> {
  const rows = await new OfflineQueue(scope).all()
  return {
    state: 'available',
    items: rows.length,
    bytes: rows.reduce((total, row) => total + (row.blob?.size ?? 0), 0),
  }
}

export async function reconcileScope(scope: string) {
  const key = `meal-feedback-scope-${feedbackConfig.environment}`
  const previous = localStorage.getItem(key)
  if (previous && previous !== scope) await new OfflineQueue(previous).clear()
  localStorage.setItem(key, scope)
}
export async function clearKnownScope(expectedScope?: string) {
  // No initialized scope means this tab has no durable content to erase.
  // Disabled feedback must never make normal sign-out depend on localStorage.
  if (expectedScope === '') return
  const key = `meal-feedback-scope-${feedbackConfig.environment}`
  const previous = localStorage.getItem(key)
  const scope = expectedScope ?? previous
  if (scope) await new OfflineQueue(scope).clear()
  if (localStorage.getItem(key) === scope) localStorage.removeItem(key)
}

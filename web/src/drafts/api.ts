import type { NewRecipe } from '../planner/types'

export interface Basis { kind: 'agent_estimate' | 'sourced'; source_ref?: string; note?: string }
export interface CanonicalDraftContent {
  title: string
  yield?: string
  ingredients: Array<{ text: string; quantity?: string; unit?: string; name?: string; note?: string }>
  instructions: Array<{ text: string; phase?: 'prep_ahead' | 'prep_tonight' | 'cook_tonight'; phase_basis?: Basis }>
  notes?: string
  sources: Array<{ id: string; kind: 'url' | 'publication' | 'user_supplied' | 'agent_generated'; label?: string; url?: string; locator?: string }>
  estimates?: Record<string, { value?: number | string; basis?: Basis }>
  phase_estimates?: Record<string, Record<string, { value?: number | string; basis?: Basis }>>
  guidance: Array<{ topic: string; text: string; basis: Basis }>
}
export interface IncomingIntent { id: string; revision: number; state: string; content_hash: string; created_at: string; expires_at: string; submission: { content_schema_version: 1; content: CanonicalDraftContent } }
export interface HostedDraft {
  schema_version: 1; content_schema_version: 1; id: string; revision: number; status: string; origin: 'agent'
  created_at: string; updated_at: string; content: CanonicalDraftContent
  review: { revision: number; state: string; draft_revision: number }
  approved_recipe_id: string | null
}
export interface HostedDraftResponse { api_version: string; draft: HostedDraft; validation: { notice: string; errors: unknown[]; warnings: Array<{ code: string; path: string; message: string }> } }

export class DraftReviewAPIError extends Error {
  readonly status: number
  constructor(status: number, message: string) { super(message); this.status = status }
}

export function draftReviewClient(getToken: () => Promise<string | null>, expire: () => void, fetcher: typeof fetch = fetch, isActive: () => boolean = () => true) {
  async function request(path: string, init?: RequestInit) {
    if (!isActive()) throw new Error('The account changed.')
    const token = await getToken()
    if (!isActive()) throw new Error('The account changed.')
    if (!token) { expire(); throw new DraftReviewAPIError(401, 'Your session ended. Sign in again.') }
    const response = await fetcher(path, { ...init, credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'application/json', ...init?.headers, Authorization: `Bearer ${token}` } })
    if (!isActive()) throw new Error('The account changed.')
    if (response.status === 401) expire()
    if (!response.ok) throw new DraftReviewAPIError(response.status,
      response.status === 409 ? 'This draft changed elsewhere. Reload before continuing.'
        : response.status === 401 ? 'Your session ended. Sign in again.'
          : response.status === 403 ? 'This request was not accepted from this page.'
            : response.status === 404 ? 'This draft is no longer available.'
              : 'The draft service is unavailable. No change was confirmed.')
    return response.json() as Promise<unknown>
  }
  return {
    async intents() { return (await request('/api/draft-review/intents') as { intents: IncomingIntent[] }).intents },
    async decide(intent: IncomingIntent, decision: 'approve' | 'deny') { return request('/api/draft-review/intents', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ intent_id: intent.id, expected_revision: intent.revision, content_hash: intent.content_hash, decision }) }) },
    async drafts() { return (await request('/api/draft-review/drafts') as { drafts: Array<{ id: string; title: string; revision: number; review_state: string; review_revision: number }> }).drafts },
    async draft(id: string) { return await request(`/api/draft-review/drafts?id=${encodeURIComponent(id)}`) as HostedDraftResponse },
    async review(item: HostedDraftResponse) { return await request('/api/draft-review/drafts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ draft_id: item.draft.id, expected_draft_revision: item.draft.revision, expected_review_revision: item.draft.review.revision }) }) as HostedDraftResponse },
    async save(item: HostedDraftResponse, projection: NewRecipe) { return request('/api/draft-review/drafts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ draft_id: item.draft.id, expected_draft_revision: item.draft.revision, expected_review_revision: item.draft.review.revision, projection }) }) },
  }
}
export type DraftReviewClient = ReturnType<typeof draftReviewClient>

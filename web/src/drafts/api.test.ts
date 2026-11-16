import { describe, expect, it, vi } from 'vitest'
import { draftReviewClient, type HostedDraftResponse } from './api'

const draft: HostedDraftResponse = {
  api_version: 'draft-intent-v1',
  draft: { schema_version: 1, content_schema_version: 1, id: '11111111-1111-4111-8111-111111111111', revision: 3, status: 'draft', origin: 'agent', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z', content: { title: 'x', ingredients: [], instructions: [], sources: [], guidance: [], estimates: { active_minutes: { value: 0, basis: { kind: 'agent_estimate' } } } }, review: { revision: 4, state: 'ready', draft_revision: 3 }, approved_recipe_id: null },
  validation: { notice: 'Structural only', errors: [], warnings: [] },
}

describe('draft review client', () => {
  it('approves pairing only through the owner bearer route with the typed code and fingerprint', async () => {
    const fetcher = vi.fn<typeof fetch>()
    fetcher.mockResolvedValue(new Response(JSON.stringify({ api_version: 'local-client-v1', state: 'approved' }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    const client = draftReviewClient(async () => 'owner-jwt', () => undefined, fetcher)
    await client.approvePairing('23456789', '0123abcd')
    expect(fetcher).toHaveBeenCalledWith('/api/draft-pairings/approve', expect.objectContaining({ method: 'POST', credentials: 'same-origin' }))
    const init = fetcher.mock.calls[0][1]
    expect(init?.headers).toMatchObject({ Authorization: 'Bearer owner-jwt', 'Content-Type': 'application/json' })
    expect(JSON.parse(String(init?.body))).toEqual({ user_code: '23456789', challenge_fingerprint: '0123abcd' })
  })

  it('binds mutations to exact draft and review revisions without converting zero', async () => {
    const fetcher = vi.fn<typeof fetch>()
    fetcher.mockResolvedValue(new Response(JSON.stringify({ draft: draft.draft, recipe: {} }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    const client = draftReviewClient(async () => 'jwt', () => undefined, fetcher)
    const projection = { title: 'Manual', shortName: '', icon: '🍲', activeMinutes: 0, totalMinutes: 0, attention: 'low' as const, tags: [], defaultServings: 1, ingredients: [], prepAhead: [], prepTonight: [], cookTonight: [] }
    await client.save(draft, projection)
    expect(fetcher.mock.calls[0][0]).toBe('/api/draft-review/drafts')
    const init = fetcher.mock.calls[0][1]
    const body = JSON.parse(String(init?.body))
    expect(init?.credentials).toBe('same-origin')
    expect(body).toEqual({ draft_id: draft.draft.id, expected_draft_revision: 3, expected_review_revision: 4, projection })
    expect(draft.draft.content.estimates?.active_minutes.value).toBe(0)
  })

  it('stops before fetch when the account changes during token lookup', async () => {
    let release!: (value: string | null) => void
    const token = new Promise<string | null>((resolve) => { release = resolve })
    let active = true
    const fetcher = vi.fn()
    const client = draftReviewClient(() => token, () => undefined, fetcher as typeof fetch, () => active)
    const request = client.drafts()
    active = false
    release('old-account-jwt')
    await expect(request).rejects.toThrow('account changed')
    expect(fetcher).not.toHaveBeenCalled()
  })

  it('expires the session on 401 without returning stale data', async () => {
    const expire = vi.fn()
    const client = draftReviewClient(async () => 'jwt', expire, vi.fn(async () => new Response('{}', { status: 401 })) as typeof fetch)
    await expect(client.drafts()).rejects.toMatchObject({ status: 401 })
    expect(expire).toHaveBeenCalledOnce()
  })

  it('treats a 400 as a definitive rejection suitable for correction', async () => {
    const client = draftReviewClient(async () => 'jwt', () => undefined, vi.fn(async () => new Response('{}', { status: 400 })) as typeof fetch)
    await expect(client.drafts()).rejects.toMatchObject({ status: 400, message: expect.stringContaining('rejected without changing') })
  })
})

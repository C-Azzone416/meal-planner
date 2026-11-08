import { expect, test, vi } from 'vitest'
import { recipeClient } from './api'
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status })

test('reads every page using the returned cursor and bearer token', async () => {
  const fetcher = vi.fn<typeof fetch>().mockResolvedValueOnce(json({ recipes: [{ id: 'first' }], nextCursor: 'cursor-id' })).mockResolvedValueOnce(json({ recipes: [{ id: 'second' }], nextCursor: null }))
  const client = recipeClient(async () => 'token', vi.fn(), fetcher)
  expect(await client.list()).toEqual([{ id: 'first' }, { id: 'second' }])
  expect(fetcher.mock.calls[1][0]).toBe('/api/recipes?limit=100&after=cursor-id')
  expect(fetcher.mock.calls[1][1]?.cache).toBe('no-store')
})

test('fails closed with no matching account token and expires the session', async () => {
  const expire = vi.fn(), fetcher = vi.fn<typeof fetch>()
  await expect(recipeClient(async () => null, expire, fetcher).list()).rejects.toMatchObject({ status: 401 })
  expect(expire).toHaveBeenCalledOnce()
  expect(fetcher).not.toHaveBeenCalled()
})

test('expires a live session on 401, but not on service failure', async () => {
  const expire = vi.fn()
  const fetcher = vi.fn<typeof fetch>().mockResolvedValueOnce(json({}, 503)).mockResolvedValueOnce(json({}, 401))
  const client = recipeClient(async () => 'token', expire, fetcher)
  await expect(client.list()).rejects.toMatchObject({ status: 503 })
  expect(expire).not.toHaveBeenCalled()
  await expect(client.list()).rejects.toMatchObject({ status: 401 })
  expect(expire).toHaveBeenCalledOnce()
})

test('an old account response cannot expire the new account session', async () => {
  let resolve!: (response: Response) => void
  let active = true
  const expire = vi.fn()
  const fetcher = vi.fn<typeof fetch>().mockImplementation(() => new Promise((done) => { resolve = done }))
  const request = recipeClient(async () => 'old-token', expire, fetcher, () => active).list()
  await vi.waitFor(() => expect(fetcher).toHaveBeenCalledOnce())
  active = false
  resolve(json({}, 401))
  await expect(request).rejects.toThrow('account changed')
  expect(expire).not.toHaveBeenCalled()
})

test('an account change while acquiring a token prevents sending the request', async () => {
  let resolve!: (token: string) => void
  let active = true
  const fetcher = vi.fn<typeof fetch>()
  const request = recipeClient(() => new Promise((done) => { resolve = done }), vi.fn(), fetcher, () => active).list()
  active = false
  resolve('old-token')
  await expect(request).rejects.toThrow('account changed')
  expect(fetcher).not.toHaveBeenCalled()
})

test('follows short pages: many one-item pages with non-null cursors are all read, in order', async () => {
  const fetcher = vi.fn<typeof fetch>()
    .mockResolvedValueOnce(json({ recipes: [{ id: 'a' }], nextCursor: 'a' }))
    .mockResolvedValueOnce(json({ recipes: [{ id: 'b' }, { id: 'c' }], nextCursor: 'c' }))
    .mockResolvedValueOnce(json({ recipes: [{ id: 'd' }], nextCursor: 'd' }))
    .mockResolvedValueOnce(json({ recipes: [{ id: 'e' }], nextCursor: null }))
  const client = recipeClient(async () => 'token', vi.fn(), fetcher)
  expect((await client.list()).map((recipe) => recipe.id)).toEqual(['a', 'b', 'c', 'd', 'e'])
  expect(fetcher.mock.calls.map(([url]) => url)).toEqual([
    '/api/recipes?limit=100',
    '/api/recipes?limit=100&after=a',
    '/api/recipes?limit=100&after=c',
    '/api/recipes?limit=100&after=d',
  ])
})

test('a repeated cursor is rejected instead of looping forever', async () => {
  const fetcher = vi.fn<typeof fetch>()
    .mockResolvedValueOnce(json({ recipes: [{ id: 'a' }], nextCursor: 'a' }))
    .mockResolvedValueOnce(json({ recipes: [{ id: 'b' }], nextCursor: 'a' }))
  await expect(recipeClient(async () => 'token', vi.fn(), fetcher).list()).rejects.toThrow('Invalid recipe pagination')
})

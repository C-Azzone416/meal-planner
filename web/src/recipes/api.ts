import type { DeepReadonly, NewRecipe } from '../planner/types'

export interface RecipeEnvelope {
  id: string
  revision: number
  status: 'approved'
  origin: 'manual'
  createdAt: string
  updatedAt: string
  content: NewRecipe
}

export class RecipeAPIError extends Error {
  readonly status: number
  constructor(status: number, message: string) { super(message); this.status = status }
}

export function recipeClient(getToken: () => Promise<string | null>, expire: () => void, fetcher: typeof fetch = fetch, isActive: () => boolean = () => true) {
  async function request(path = '', init?: RequestInit): Promise<unknown> {
    if (!isActive()) throw new Error('The account changed.')
    const token = await getToken()
    if (!isActive()) throw new Error('The account changed.')
    if (!token) {
      expire()
      throw new RecipeAPIError(401, 'Your session ended. Sign in again.')
    }
    const response = await fetcher(`/api/recipes${path}`, {
      ...init,
      credentials: 'same-origin',
      cache: 'no-store',
      headers: { Accept: 'application/json', ...init?.headers, Authorization: `Bearer ${token}` },
    })
    if (!isActive()) throw new Error('The account changed.')
    if (response.status === 401) expire()
    if (!response.ok) throw new RecipeAPIError(response.status,
      response.status === 409 ? 'This recipe changed elsewhere. Your edits are still here. Open the latest recipe before trying again.'
        : response.status === 401 ? 'Your session ended. Sign in again.'
        : response.status === 403 ? 'This account cannot access recipes.'
        : [400, 415].includes(response.status) ? 'The recipe was rejected without saving. Check the fields, correct your recipe and save again.'
        : response.status === 413 ? 'The recipe is too large and was not saved. Shorten the text or remove some rows, then save again.'
        : 'The recipe service is unavailable. Your changes have not been confirmed. Retry when connected.')
    return response.json()
  }
  return {
    async list(): Promise<RecipeEnvelope[]> {
      const recipes: RecipeEnvelope[] = []
      let cursor: string | null = null
      const cursors = new Set<string>()
      do {
        const page = await request(`?limit=100${cursor ? `&after=${encodeURIComponent(cursor)}` : ''}`) as { recipes: RecipeEnvelope[]; nextCursor: string | null }
        if (!Array.isArray(page.recipes) || (page.nextCursor !== null && typeof page.nextCursor !== 'string')) throw new Error('Invalid recipe response. Retry loading.')
        recipes.push(...page.recipes)
        cursor = page.nextCursor
        if (cursor && cursors.has(cursor)) throw new Error('Invalid recipe pagination. Retry loading.')
        if (cursor) cursors.add(cursor)
      } while (cursor)
      return recipes
    },
    async create(content: DeepReadonly<NewRecipe>, key: string): Promise<RecipeEnvelope> {
      return await request('', { method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key }, body: JSON.stringify({ content }) }) as RecipeEnvelope
    },
    async update(id: string, content: DeepReadonly<NewRecipe>, expectedRevision: number): Promise<RecipeEnvelope> {
      return await request(`?id=${encodeURIComponent(id)}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ content, expectedRevision }) }) as RecipeEnvelope
    },
  }
}
export type RecipeClient = ReturnType<typeof recipeClient>

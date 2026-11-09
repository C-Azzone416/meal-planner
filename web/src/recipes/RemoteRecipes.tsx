import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { recipeClient, type RecipeClient, type RecipeEnvelope } from './api'
import { useAuth } from '../auth/AuthProvider'
import type { DeepReadonly, NewRecipe } from '../planner/types'

interface RemoteRecipes {
  records: RecipeEnvelope[]
  loading: boolean
  error: string | null
  retry(): void
  create(content: DeepReadonly<NewRecipe>, key: string): Promise<string>
  update(id: string, content: DeepReadonly<NewRecipe>, revision: number): Promise<string>
}
const Context = createContext<RemoteRecipes | null>(null)
export function useRemoteRecipes() { return useContext(Context) }

export function AccountRecipes({ accountId, children }: { accountId: string; children: ReactNode }) {
  const { getAccessToken, expireSession } = useAuth()
  const active = useRef(true)
  useEffect(() => { active.current = true; return () => { active.current = false } }, [])
  // The client invokes this guard only during asynchronous requests, never during render.
  // eslint-disable-next-line react-hooks/refs
  const client = useMemo(() => recipeClient(() => getAccessToken(accountId), expireSession, fetch, () => active.current), [accountId, getAccessToken, expireSession])
  return <RemoteRecipesProvider key={accountId} client={client}>{children}</RemoteRecipesProvider>
}

export function RemoteRecipesProvider({ client, children }: { client: RecipeClient; children: ReactNode }) {
  const [records, setRecords] = useState<RecipeEnvelope[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [attempt, setAttempt] = useState(0)
  const active = useRef(false)
  const generation = useRef(0)
  useEffect(() => {
    active.current = true
    return () => { active.current = false }
  }, [])
  useEffect(() => {
    const current = ++generation.current
    // Retain the last known snapshot on read failures, never present it as a fresh successful read.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setLoading(true)
    setError(null)
    client.list().then((items) => {
      if (active.current && current === generation.current) setRecords(items)
    }).catch(() => {
      if (active.current && current === generation.current) setError('Database recipes could not be loaded. Shopping and prep may be incomplete. Browser-only recipes are still available.')
    }).finally(() => {
      if (active.current && current === generation.current) setLoading(false)
    })
    return () => { generation.current = current + 1 }
  }, [client, attempt])
  const accept = useCallback((record: RecipeEnvelope) => {
    if (!active.current) throw new Error('The account changed. Open your recipes again.')
    // A read started before this mutation must not replace the newer committed result.
    generation.current++
    setLoading(false)
    setAttempt((n) => n + 1)
    setRecords((current) => [...current.filter((item) => item.id !== record.id), record])
    return record.id
  }, [])
  const value = useMemo<RemoteRecipes>(() => ({
    records, loading, error, retry: () => setAttempt((n) => n + 1),
    create: async (content, key) => accept(await client.create(content, key)),
    update: async (id, content, revision) => accept(await client.update(id, content, revision)),
  }), [records, loading, error, client, accept])
  return <Context.Provider value={value}>{children}</Context.Provider>
}

export function RecipeLoadNotice() {
  const remote = useRemoteRecipes()
  if (!remote) return null
  return <>
    {remote.loading && <p role="status">Loading database recipes… Shopping and prep may be incomplete until loading finishes.</p>}
    {remote.error && <div role="alert"><p>{remote.error}</p><button type="button" onClick={remote.retry} disabled={remote.loading}>Retry loading recipes</button></div>}
  </>
}

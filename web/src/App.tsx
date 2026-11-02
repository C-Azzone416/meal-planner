import { useEffect, useState } from 'react'

export interface Health {
  status: string
  service: string
  version: string
}

type State =
  | { kind: 'loading' }
  | { kind: 'ok'; health: Health }
  | { kind: 'error'; message: string }

export default function App() {
  const [state, setState] = useState<State>({ kind: 'loading' })

  useEffect(() => {
    const controller = new AbortController()
    fetch('/api/health', { signal: controller.signal })
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        return res.json() as Promise<Health>
      })
      .then((health) => setState({ kind: 'ok', health }))
      .catch((err: unknown) => {
        if (controller.signal.aborted) return
        setState({
          kind: 'error',
          message: err instanceof Error ? err.message : 'unknown error',
        })
      })
    return () => controller.abort()
  }, [])

  return (
    <main>
      <h1>Meal Planner</h1>
      {state.kind === 'loading' && <p>Checking API…</p>}
      {state.kind === 'ok' && (
        <p>
          API status: <strong>{state.health.status}</strong> (version{' '}
          {state.health.version})
        </p>
      )}
      {state.kind === 'error' && (
        <p role="alert">API unavailable: {state.message}</p>
      )}
    </main>
  )
}

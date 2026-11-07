import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react'
import { type AuthService, type AuthUser, loadAuthService } from './service'

type AuthStatus =
  | 'loading'
  | 'unavailable'
  | 'signed-out'
  | 'signed-in'
  | 'error'

interface AuthContextValue {
  status: AuthStatus
  user: AuthUser | null
  message: string | null
  /** One-off, non-error message for the sign-in screen ("You're signed out.", "Your session ended. Sign in again."). */
  notice: string | null
  clearNotice(): void
  /** Call when any request reports the session is gone (e.g. a future API 401). */
  expireSession(): void
  signInWithPassword(email: string, password: string): Promise<void>
  sendPasswordResetCode(email: string): Promise<void>
  setPassword(email: string, code: string, password: string): Promise<void>
  signOut(): Promise<void>
  retry(): void
}

const AuthContext = createContext<AuthContextValue | null>(null)

export const SIGNED_OUT_NOTICE = 'You\'re signed out.'
export const SESSION_ENDED_NOTICE = 'Your session ended. Sign in again.'
/** How often a signed-in session is re-checked (also on focus / tab becoming visible). */
export const SESSION_RECHECK_MS = 5 * 60 * 1000

export interface AuthProviderProps {
  children: ReactNode
  serviceFactory?: () => Promise<AuthService | null>
}

export function AuthProvider({
  children,
  serviceFactory = loadAuthService,
}: AuthProviderProps) {
  const [service, setService] = useState<AuthService | null>(null)
  const [status, setStatus] = useState<AuthStatus>('loading')
  const [user, setUser] = useState<AuthUser | null>(null)
  const [message, setMessage] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let active = true
    serviceFactory()
      .then(async (nextService) => {
        if (!active) return
        if (!nextService) {
          setService(null)
          setStatus('unavailable')
          return
        }

        const nextUser = await nextService.getSession()
        if (!active) return
        if (nextUser && !nextUser.emailVerified) {
          await nextService.signOut()
          if (!active) return
          setService(nextService)
          setUser(null)
          setStatus('signed-out')
          return
        }
        setService(nextService)
        setUser(nextUser)
        setStatus(nextUser ? 'signed-in' : 'signed-out')
      })
      .catch(() => {
        if (!active) return
        setService(null)
        setUser(null)
        setStatus('error')
        setMessage('Sign-in is temporarily unavailable. Please try again.')
      })

    return () => {
      active = false
    }
  }, [attempt, serviceFactory])

  const signInWithPassword = useCallback(
    async (email: string, password: string) => {
      if (!service) throw new Error('Sign-in is not configured yet.')
      setMessage(null)
      const nextUser = await service.signInWithPassword(email, password)
      setUser(nextUser)
      setNotice(null)
      setStatus('signed-in')
    },
    [service],
  )

  const sendPasswordResetCode = useCallback(
    async (email: string) => {
      if (!service) throw new Error('Sign-in is not configured yet.')
      setMessage(null)
      await service.sendPasswordResetCode(email)
    },
    [service],
  )

  const setPassword = useCallback(
    async (email: string, code: string, password: string) => {
      if (!service) throw new Error('Sign-in is not configured yet.')
      setMessage(null)
      const nextUser = await service.setPassword(email, code, password)
      setUser(nextUser)
      setNotice(null)
      setStatus('signed-in')
    },
    [service],
  )

  const signOut = useCallback(async () => {
    if (service) await service.signOut()
    setUser(null)
    setNotice(service ? SIGNED_OUT_NOTICE : null)
    setStatus(service ? 'signed-out' : 'unavailable')
  }, [service])

  const expireSession = useCallback(() => {
    setUser(null)
    setNotice(SESSION_ENDED_NOTICE)
    setStatus((current) => (current === 'signed-in' ? 'signed-out' : current))
  }, [])

  const clearNotice = useCallback(() => setNotice(null), [])

  // While signed in, notice a session that ended elsewhere (other tab, expiry, revoked) and send the
  // person to sign-in; the route guard remembers the page so they return to it afterwards.
  useEffect(() => {
    if (status !== 'signed-in' || !service) return undefined
    let active = true
    const check = () => {
      service
        .getSession()
        .then((session) => {
          if (active && (!session || !session.emailVerified)) expireSession()
        })
        .catch(() => {
          // A failed check (offline, server hiccup) is not proof the session ended: stay signed in.
        })
    }
    const onVisible = () => {
      if (document.visibilityState === 'visible') check()
    }
    const interval = window.setInterval(check, SESSION_RECHECK_MS)
    window.addEventListener('focus', check)
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      active = false
      window.clearInterval(interval)
      window.removeEventListener('focus', check)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [expireSession, service, status])

  const retry = useCallback(() => {
    setStatus('loading')
    setMessage(null)
    setAttempt((value) => value + 1)
  }, [])

  const value = useMemo<AuthContextValue>(
    () => ({
      status,
      user,
      message,
      notice,
      clearNotice,
      expireSession,
      signInWithPassword,
      sendPasswordResetCode,
      setPassword,
      signOut,
      retry,
    }),
    [
      message,
      notice,
      clearNotice,
      expireSession,
      retry,
      sendPasswordResetCode,
      setPassword,
      signInWithPassword,
      signOut,
      status,
      user,
    ],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext)
  if (!value) throw new Error('useAuth must be used inside AuthProvider')
  return value
}

/** Notice for the sign-in screen; safe outside an AuthProvider (returns no notice). */
export function useAuthNotice(): { notice: string | null; clearNotice: () => void } {
  const value = useContext(AuthContext)
  return { notice: value?.notice ?? null, clearNotice: value?.clearNotice ?? (() => undefined) }
}

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
  signInWithPassword(email: string, password: string): Promise<void>
  sendPasswordResetCode(email: string): Promise<void>
  setPassword(email: string, code: string, password: string): Promise<void>
  signOut(): Promise<void>
  retry(): void
}

const AuthContext = createContext<AuthContextValue | null>(null)

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
      setStatus('signed-in')
    },
    [service],
  )

  const signOut = useCallback(async () => {
    if (service) await service.signOut()
    setUser(null)
    setStatus(service ? 'signed-out' : 'unavailable')
  }, [service])

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
      signInWithPassword,
      sendPasswordResetCode,
      setPassword,
      signOut,
      retry,
    }),
    [
      message,
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

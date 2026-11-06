import { useState } from 'react'
import {
  BrowserRouter,
  Navigate,
  Outlet,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from 'react-router-dom'
import { AuthProvider, useAuth } from './auth/AuthProvider'
import { AuthCodeVerificationError } from './auth/service'
import { AppShell } from './components/AppShell'
import {
  CookPage,
  DraftsPage,
  HouseRulesPage,
  PrepPage,
  RecipesPage,
  ShoppingPage,
  SignInPage,
  WeekPage,
} from './pages'

function ProtectedShell() {
  const auth = useAuth()
  const location = useLocation()

  if (auth.status === 'loading') {
    return <main className="auth-state" role="status">Checking your fridge…</main>
  }
  if (auth.status !== 'signed-in') {
    return <Navigate to="/sign-in" replace state={{ from: location.pathname }} />
  }

  return <AppShell onSignOut={() => void auth.signOut()}><Outlet /></AppShell>
}

function SignInRoute() {
  const auth = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string>()
  const from = (location.state as { from?: string } | null)?.from

  if (auth.status === 'signed-in') {
    return <Navigate to={from && from !== '/sign-in' ? from : '/week'} replace />
  }

  const requestCode = async (email: string) => {
    setPending(true)
    setError(undefined)
    try {
      await auth.sendCode(email.trim())
    } catch {
      setError('We could not send a code. Check the address and try again.')
      throw new Error('request failed')
    } finally {
      setPending(false)
    }
  }

  const verifyCode = async (email: string, code: string) => {
    setPending(true)
    setError(undefined)
    try {
      await auth.verifyCode(email.trim(), code.trim())
      navigate(from && from !== '/sign-in' ? from : '/week', { replace: true })
    } catch (cause) {
      setError(cause instanceof AuthCodeVerificationError
        ? 'That code is invalid or expired. Request a new code and try again.'
        : 'Your code was accepted, but we could not open a secure session. Request a new code and try again.')
    } finally {
      setPending(false)
    }
  }

  return (
    <SignInPage
      available={auth.status === 'signed-out'}
      unavailable={auth.status === 'unavailable'}
      pending={pending || auth.status === 'loading'}
      error={error ?? auth.message ?? undefined}
      onRequestCode={requestCode}
      onVerifyCode={verifyCode}
    />
  )
}

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/sign-in" element={<SignInRoute />} />
      <Route element={<ProtectedShell />}>
        <Route path="/week" element={<WeekPage />} />
        <Route path="/recipes" element={<RecipesPage />} />
        <Route path="/prep" element={<PrepPage />} />
        <Route path="/shopping" element={<ShoppingPage />} />
        <Route path="/drafts" element={<DraftsPage />} />
        <Route path="/cook" element={<CookPage />} />
        <Route path="/house-rules" element={<HouseRulesPage />} />
      </Route>
      <Route path="/" element={<Navigate to="/week" replace />} />
      <Route path="*" element={<Navigate to="/week" replace />} />
    </Routes>
  )
}

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <AppRoutes />
      </AuthProvider>
    </BrowserRouter>
  )
}

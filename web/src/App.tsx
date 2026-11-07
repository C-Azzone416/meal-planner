import { useState } from 'react'
import {
  createBrowserRouter,
  Navigate,
  Outlet,
  Route,
  RouterProvider,
  Routes,
  useLocation,
  useNavigate,
} from 'react-router-dom'
import { AuthProvider, useAuth } from './auth/AuthProvider'
import {
  AuthCodeVerificationError,
  AuthCredentialsError,
  AuthPasswordPolicyError,
} from './auth/service'
import { AppShell } from './components/AppShell'
import { PageSkeleton } from './components/StateCard'
import { PlannerProvider } from './planner/store'
import {
  CookPage,
  DraftsPage,
  HouseRulesPage,
  PrepPage,
  RecipeDetailPage,
  RecipeFormPage,
  RecipesPage,
  ShoppingPage,
  SignInPage,
  WeekPage,
} from './pages'

/** Where to go after signing in: an in-app path (query and hash kept), never the sign-in page or another origin. */
function returnPath(from: string | undefined): string | undefined {
  if (!from || !from.startsWith('/') || from.startsWith('//')) return undefined
  if (from === '/sign-in' || from.startsWith('/sign-in?') || from.startsWith('/sign-in#')) return undefined
  return from
}

function ProtectedShell() {
  const auth = useAuth()
  const location = useLocation()

  if (auth.status === 'loading') {
    return <main className="auth-state"><PageSkeleton label="Checking your fridge" cards={2} /></main>
  }
  if (auth.status !== 'signed-in') {
    return <Navigate to="/sign-in" replace state={{ from: `${location.pathname}${location.search}${location.hash}` }} />
  }

  return <AppShell onSignOut={() => void auth.signOut()}><Outlet /></AppShell>
}

function SignInRoute() {
  const auth = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string>()
  const from = returnPath((location.state as { from?: string } | null)?.from)

  if (auth.status === 'signed-in') {
    return <Navigate to={from ?? '/week'} replace />
  }

  const signInWithPassword = async (email: string, password: string) => {
    setPending(true)
    setError(undefined)
    try {
      await auth.signInWithPassword(email.trim(), password)
      navigate(from ?? '/week', { replace: true })
    } catch (cause) {
      setError(cause instanceof AuthCredentialsError
        ? 'That email and password did not match.'
        : 'We could not open a secure session. Please try again.')
      throw new Error('request failed', { cause })
    } finally {
      setPending(false)
    }
  }

  const requestPasswordResetCode = async (email: string) => {
    setPending(true)
    setError(undefined)
    try {
      await auth.sendPasswordResetCode(email.trim())
    } catch {
      setError('We could not send a password code. Check the address and try again.')
      throw new Error('request failed')
    } finally {
      setPending(false)
    }
  }

  const setPassword = async (email: string, code: string, password: string) => {
    setPending(true)
    setError(undefined)
    try {
      await auth.setPassword(email.trim(), code.trim(), password)
      navigate(from ?? '/week', { replace: true })
    } catch (cause) {
      setError(cause instanceof AuthCodeVerificationError
        ? 'That code is invalid or expired. Request a new code.'
        : cause instanceof AuthPasswordPolicyError
          ? 'Use a password between 8 and 128 characters.'
          : 'Your password may have changed, but we could not open a secure session. Try signing in.')
      throw new Error('password setup failed', { cause })
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
      onClearError={() => setError(undefined)}
      onSignIn={signInWithPassword}
      onRequestPasswordResetCode={requestPasswordResetCode}
      onSetPassword={setPassword}
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
        <Route path="/recipes/new" element={<RecipeFormPage mode="new" />} />
        <Route path="/recipes/by-id/:recipeId" element={<RecipeDetailPage />} />
        <Route path="/recipes/:recipeId" element={<RecipeDetailPage />} />
        <Route path="/recipes/:recipeId/edit" element={<RecipeFormPage mode="edit" />} />
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

function AppProviders() {
  return (
    <AuthProvider>
      <PlannerProvider>
        <AppRoutes />
      </PlannerProvider>
    </AuthProvider>
  )
}

// One router for the life of the page. Creating it inside a component (even in a state initializer) runs twice
// under StrictMode and would register two popstate listeners.
let browserRouter: ReturnType<typeof createBrowserRouter> | undefined
function getBrowserRouter() {
  // A data router is required for useBlocker (unsaved-changes prompt on the recipe form).
  // One splat route keeps <AppRoutes> as the single place routes are declared.
  browserRouter ??= createBrowserRouter([{ path: '*', element: <AppProviders /> }])
  return browserRouter
}

export default function App() {
  return <RouterProvider router={getBrowserRouter()} />
}

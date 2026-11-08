import { useMemo, useState, type ReactNode } from 'react'
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
  AuthNetworkError,
  AuthPasswordPolicyError,
} from './auth/service'
import { AppShell } from './components/AppShell'
import { RouteFocus } from './components/RouteFocus'
import { PageSkeleton } from './components/StateCard'
import { AccountRecipes, useRemoteRecipes } from './recipes/RemoteRecipes'
import { PlannerProvider } from './planner/store'
import type { StorageLike } from './planner/persist'
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

function ProtectedShell({ storage }: { storage?: StorageLike | null }) {
  const auth = useAuth()
  const location = useLocation()

  if (auth.status === 'loading') {
    return <main className="auth-state"><PageSkeleton label="Checking your fridge" cards={2} width="min(100%, 560px)" /></main>
  }
  if (auth.status !== 'signed-in' || !auth.user) {
    return <Navigate to="/sign-in" replace state={{ from: `${location.pathname}${location.search}${location.hash}` }} />
  }

  return <AccountRecipes key={auth.user.id} accountId={auth.user.id}><HostedPlanner accountId={auth.user.id} storage={storage}><AppShell onSignOut={() => void auth.signOut()}><Outlet /></AppShell></HostedPlanner></AccountRecipes>
}

function HostedPlanner({ accountId, storage, children }: { accountId: string; storage?: StorageLike | null; children: ReactNode }) {
  const remote = useRemoteRecipes()
  const recipes = useMemo(() => remote?.records.map((record) => ({ ...record.content, id: record.id })) ?? [], [remote?.records])
  return <PlannerProvider accountId={accountId} storage={storage} externalRecipes={recipes}>{children}</PlannerProvider>
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
    } catch (cause) {
      // Only "could not reach the server" is reported as such. Every other failure looks exactly like success
      // ("If that address can sign in, we sent a code."), so the response never reveals whether an account exists.
      if (cause instanceof AuthNetworkError) {
        setError('We couldn’t reach the server. Try again.')
        throw new Error('request failed', { cause })
      }
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

export function AppRoutes({ plannerStorage }: { plannerStorage?: StorageLike | null } = {}) {
  return (
    <>
      <Routes>
        <Route path="/sign-in" element={<SignInRoute />} />
        <Route element={<ProtectedShell storage={plannerStorage} />}>
          <Route path="/week" element={<WeekPage />} />
          <Route path="/recipes" element={<RecipesPage />} />
          <Route path="/recipes/new" element={<RecipeFormPage mode="new" />} />
          <Route path="/recipes/by-id/:recipeId" element={<RecipeDetailPage />} />
          <Route path="/recipes/by-id/:recipeId/edit" element={<RecipeFormPage mode="edit" />} />
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
    {/* Outside the shell, so it also runs when the session ends and the app redirects to sign-in. Placed after the routes so it focuses the new heading. */}
    <RouteFocus />
    </>
  )
}

function AppProviders() {
  return (
    <AuthProvider>
      <AppRoutes />
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

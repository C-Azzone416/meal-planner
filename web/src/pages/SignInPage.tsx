import { useState, type FormEvent } from 'react'
import { PawMagnet } from '../components/Magnet'

type SignInMode = 'sign-in' | 'reset-email' | 'set-password'

type SignInPageProps = {
  available: boolean
  unavailable?: boolean
  pending?: boolean
  error?: string
  onClearError: () => void
  onSignIn: (email: string, password: string) => Promise<void>
  onRequestPasswordResetCode: (email: string) => Promise<void>
  onSetPassword: (email: string, code: string, password: string) => Promise<void>
}

export function SignInPage({
  available,
  unavailable = false,
  pending = false,
  error,
  onClearError,
  onSignIn,
  onRequestPasswordResetCode,
  onSetPassword,
}: SignInPageProps) {
  const [mode, setMode] = useState<SignInMode>('sign-in')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [code, setCode] = useState('')
  const [localError, setLocalError] = useState<string>()

  const clearSecrets = () => {
    setCode('')
    setPassword('')
    setConfirmPassword('')
    setLocalError(undefined)
    onClearError()
  }

  const switchMode = (nextMode: SignInMode) => {
    clearSecrets()
    setMode(nextMode)
  }

  const signIn = async (event: FormEvent) => {
    event.preventDefault()
    setLocalError(undefined)
    try {
      await onSignIn(email, password)
    } catch {
      // The route owns the privacy-safe error copy.
    } finally {
      setPassword('')
    }
  }

  const requestResetCode = async (event?: FormEvent) => {
    event?.preventDefault()
    setLocalError(undefined)
    try {
      await onRequestPasswordResetCode(email)
      setCode('')
      setMode('set-password')
    } catch {
      // The route owns the privacy-safe error copy.
    }
  }

  const savePassword = async (event: FormEvent) => {
    event.preventDefault()
    setLocalError(undefined)
    if (password !== confirmPassword) {
      setLocalError('Those passwords do not match.')
      return
    }
    try {
      await onSetPassword(email, code, password)
      clearSecrets()
    } catch {
      setPassword('')
      setConfirmPassword('')
    }
  }

  return (
    <main className="sign-in-page">
      <div className="sign-in-fridge">
        <header>
          <span className="wordmark">dinner, sorted.</span>
          <h1 className="paper-title" aria-label="what’s for dinner?">
            <span>what’s</span><span>for</span><span>dinner?</span>
          </h1>
          <p className="script-copy">🐈‍⬛ sign in to see your fridge</p>
          <span className="door-handle" aria-hidden="true" />
        </header>
        <section className="sign-in-card">
          <PawMagnet />

          {mode === 'sign-in' && (
            <form onSubmit={(event) => void signIn(event)}>
              <label>
                Email
                <input type="email" autoComplete="email" value={email} onChange={(event) => setEmail(event.target.value)} placeholder="you@example.com" required />
              </label>
              <label>
                Password
                <input type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required />
              </label>
              <button className="button button--green full-button" disabled={pending || !available}>{pending ? 'Opening…' : 'Open my fridge'}</button>
              <button type="button" className="text-button" disabled={pending} onClick={() => switchMode('reset-email')}>Set or reset password</button>
              <p>Access is invitation-only. Invited accounts use an email and password.</p>
            </form>
          )}

          {mode === 'reset-email' && (
            <form onSubmit={(event) => void requestResetCode(event)}>
              <h2>set or reset your password</h2>
              <p>We’ll email a password code if this invited account exists.</p>
              <label>
                Email
                <input type="email" autoComplete="email" value={email} onChange={(event) => setEmail(event.target.value)} placeholder="you@example.com" required />
              </label>
              <button className="button button--green full-button" disabled={pending || !available}>{pending ? 'Sending…' : 'Email my password code'}</button>
              <button type="button" className="text-button" disabled={pending} onClick={() => switchMode('sign-in')}>Back to sign in</button>
            </form>
          )}

          {mode === 'set-password' && (
            <form onSubmit={(event) => void savePassword(event)}>
              <h2>choose your password</h2>
              <p>Enter the code from your email, then choose a new password.</p>
              <label>
                Password code
                <input inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(event) => setCode(event.target.value)} placeholder="6-digit code" minLength={6} maxLength={6} required />
              </label>
              <label>
                New password
                <input type="password" autoComplete="new-password" value={password} onChange={(event) => setPassword(event.target.value)} minLength={8} maxLength={128} required />
              </label>
              <label>
                Confirm password
                <input type="password" autoComplete="new-password" value={confirmPassword} onChange={(event) => setConfirmPassword(event.target.value)} minLength={8} maxLength={128} required />
              </label>
              <p>Use 8–128 characters. Your password and codes are never stored by this app.</p>
              <button className="button button--green full-button" disabled={pending || !available}>
                {pending ? 'Saving…' : 'Save password & open my fridge'}
              </button>
              <button type="button" className="text-button" disabled={pending || !available} onClick={() => void requestResetCode()}>Email me a new password code</button>
              <button type="button" className="text-button" disabled={pending} onClick={() => switchMode('sign-in')}>Back to sign in</button>
            </form>
          )}

          {unavailable && <p className="form-message" role="status">Sign-in is not configured for this environment yet.</p>}
          {(localError || error) && <p className="form-error" role="alert">{localError ?? error}</p>}
        </section>
        <footer>Your recipes and plans are private to your account.</footer>
      </div>
    </main>
  )
}

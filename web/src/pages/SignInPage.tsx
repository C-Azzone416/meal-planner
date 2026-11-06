import { useState, type FormEvent } from 'react'
import { PawMagnet } from '../components/Magnet'

type SignInPageProps = {
  available: boolean
  unavailable?: boolean
  pending?: boolean
  error?: string
  onRequestCode: (email: string) => Promise<void>
  onVerifyCode: (email: string, code: string) => Promise<void>
}

export function SignInPage({
  available,
  unavailable = false,
  pending = false,
  error,
  onRequestCode,
  onVerifyCode,
}: SignInPageProps) {
  const [email, setEmail] = useState('')
  const [code, setCode] = useState('')
  const [codeSent, setCodeSent] = useState(false)

  const requestCode = async (event: FormEvent) => {
    event.preventDefault()
    try {
      await onRequestCode(email)
      setCodeSent(true)
    } catch {
      // The route owns the privacy-safe error copy.
    }
  }

  const verify = async (event: FormEvent) => {
    event.preventDefault()
    await onVerifyCode(email, code)
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
          <form onSubmit={codeSent ? verify : requestCode}>
            {codeSent ? (
              <>
                <label>
                  Sign-in code
                  <input
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    value={code}
                    onChange={(event) => setCode(event.target.value)}
                    placeholder="6-digit code"
                    minLength={6}
                    maxLength={6}
                    required
                  />
                </label>
                <button className="button button--green full-button" disabled={pending || !available}>
                  {pending ? 'Checking…' : 'Open my fridge'}
                </button>
                <button type="button" className="text-button" onClick={() => { setCodeSent(false); setCode('') }}>
                  Use a different email
                </button>
              </>
            ) : (
              <>
                <label>
                  Email
                  <input
                    type="email"
                    autoComplete="email"
                    value={email}
                    onChange={(event) => setEmail(event.target.value)}
                    placeholder="you@example.com"
                    required
                  />
                </label>
                <button className="button button--green full-button" disabled={pending || !available}>
                  {pending ? 'Sending…' : 'Email me a sign-in code'}
                </button>
              </>
            )}
            <p>No passwords. New here? The same steps create your account.</p>
            {unavailable && <p className="form-message" role="status">Sign-in is not configured for this environment yet.</p>}
            {error && <p className="form-error" role="alert">{error}</p>}
          </form>
        </section>
        <footer>Your recipes and plans are private to your account.</footer>
      </div>
    </main>
  )
}

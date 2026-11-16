import { type FormEvent, useState } from 'react'
import { useAuth } from '../auth/AuthProvider'
import { draftReviewClient } from '../drafts/api'
import { FridgePanel, PaperTitle, PreviewNotice } from '../components/FridgePanel'

const codePattern = /^[23456789ABCDEFGHJKLMNPQRSTUVWXYZ]{8}$/
const fingerprintPattern = /^[0-9a-f]{8}$/

export function ConnectPage() {
  const auth = useAuth()
  const accountId = auth.user?.id ?? ''
  const [code, setCode] = useState('')
  const [fingerprint, setFingerprint] = useState('')
  const [pending, setPending] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const [approved, setApproved] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    const userCode = code.trim().toUpperCase()
    const challengeFingerprint = fingerprint.trim().toLowerCase()
    if (!codePattern.test(userCode) || !fingerprintPattern.test(challengeFingerprint)) {
      setMessage('Enter the eight-character code and eight-character fingerprint shown by your local client.')
      return
    }
    setPending(true)
    setMessage(null)
    try {
      const client = draftReviewClient(() => auth.getAccessToken(accountId), auth.expireSession)
      await client.approvePairing(userCode, challengeFingerprint)
      setApproved(true)
      setMessage('This local client is connected for draft proposals only. It cannot approve drafts or recipes.')
    } catch (cause) {
      setMessage(cause instanceof Error ? cause.message : 'Pairing was not confirmed.')
    } finally {
      setPending(false)
    }
  }

  return <>
    <FridgePanel className="hero-panel"><PaperTitle words={['connect', 'drafts']} /><p className="script-copy">pair one local client · proposals still need separate browser approval</p></FridgePanel>
    <PreviewNotice />
    <FridgePanel className="content-panel">
      <h2>Connect a local client</h2>
      <p>Type the code and fingerprint shown in your own terminal. Do not use values sent by someone else.</p>
      <form onSubmit={(event) => void submit(event)}>
        <label>User code<input value={code} autoComplete="off" inputMode="text" maxLength={8} disabled={pending || approved} onChange={(event) => setCode(event.target.value)} /></label>
        <label>Challenge fingerprint<input value={fingerprint} autoComplete="off" inputMode="text" maxLength={8} disabled={pending || approved} onChange={(event) => setFingerprint(event.target.value)} /></label>
        <button className="button button--green" type="submit" disabled={pending || approved}>{pending ? 'Connecting…' : 'Connect proposal-only client'}</button>
      </form>
      {message && <p role="status">{message}</p>}
    </FridgePanel>
  </>
}

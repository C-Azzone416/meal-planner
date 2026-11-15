import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { useLocation } from 'react-router-dom'
import {
  prepareLocalFeedback,
  type PreparedFeedback,
} from '@masra91/framebriar/browser/local-export'
import type {
  DurableFeedback,
  OutboxStatus,
} from '@masra91/framebriar/browser/durable-feedback'
import { captureViewport } from 'feedback-capture'
import { useAuth } from '../auth/AuthProvider'
import { feedbackConfig } from './config'
import {
  clearKnownScope,
  enabled,
  inspectScope,
  makeOutbox,
  reconcileScope,
  request,
  type Connection,
} from './client'
import './feedback.css'
import { ScreenshotEditor } from './ScreenshotEditor'
import { useAnnotationDraft } from './use-annotation-draft'
import { finalScreenshot } from './annotation-final'

type Controls = {
  open(opener: HTMLElement): void
  signOut(): Promise<void>
  connection: Connection
  refresh(): Promise<void>
  clear(): Promise<void>
  count: number
}
const Context = createContext<Controls | null>(null)
const screens: Record<string, string> = {
  '/week': 'week',
  '/recipes': 'recipes',
  '/prep': 'prep',
  '/shopping': 'shopping',
  '/drafts': 'drafts',
  '/cook': 'cook',
  '/house-rules': 'house-rules',
}

export function FeedbackProvider({ children }: { children: ReactNode }) {
  const auth = useAuth(),
    location = useLocation()
  const [connection, setConnection] = useState<Connection>({
    status: 'unavailable',
  })
  const [outboxState, setOutboxState] = useState<OutboxStatus>({
    state: 'checking',
    items: 0,
    bytes: 0,
  })
  const [capturing, setCapturing] = useState(false)
  const [locked, setLocked] = useState(false)
  const [open, setOpen] = useState(false),
    [notes, setNotes] = useState(''),
    [media, setMedia] = useState<Blob | null>(null)
  const [includeImage, setIncludeImage] = useState(true)
  const opening = useRef(false)
  const [busy, setBusy] = useState(false),
    [message, setMessage] = useState('')
  const box = useRef<DurableFeedback | null>(null),
    epoch = useRef(0),
    prepared = useRef<PreparedFeedback | null>(null),
    pending = useRef(false),
    captureEpoch = useRef(0)
  const dialog = useRef<HTMLDivElement>(null),
    returnFocus = useRef<HTMLElement | null>(null)
  const saveSettled = useRef<Promise<void>>(Promise.resolve())
  const saveAbort = useRef<AbortController | null>(null)
  const received = useRef(new Set<string>())
  const currentScope = useRef('')
  const currentConnection = useRef(connection)
  useEffect(() => { currentConnection.current = connection }, [connection])
  const annotation = useAnnotationDraft(connection.outboxKey ?? null)
  const captureScene = useRef('other')
  const refreshSequence = useRef(0)
  const captureTime = useRef<string | null>(null)
  const tokenRef = useRef(auth.getToken)
  useEffect(() => {
    tokenRef.current = auth.getToken
  }, [auth.getToken])
  const refresh = useCallback(async () => {
    if (!enabled()) return
    const ticket = epoch.current
    const sequence = ++refreshSequence.current
    try {
      const next: Connection = await request('status', () => tokenRef.current())
      if (ticket !== epoch.current || sequence !== refreshSequence.current)
        return
      if (
        next.environment !== feedbackConfig.environment ||
        next.appId !== feedbackConfig.appId ||
        next.buildSha !== feedbackConfig.buildSha
      )
        throw new Error('Configuration mismatch')
      setConnection(next)
      if (!next.outboxKey || !/^[a-zA-Z0-9_-]{16,128}$/.test(next.outboxKey))
        throw new Error('Missing storage scope')
      const scope = `meal-${feedbackConfig.environment}-${next.outboxKey}`
      if (box.current && currentScope.current !== scope) {
        box.current.stop()
        await box.current.clear()
        box.current = null
      }
      if (!box.current) {
        currentScope.current = scope
        await reconcileScope(scope)
        if (ticket !== epoch.current || sequence !== refreshSequence.current)
          return
        currentScope.current = scope
        box.current = makeOutbox(
          scope,
          () => tokenRef.current(),
          () => ticket === epoch.current,
          (value) => {
            if (ticket === epoch.current) setOutboxState(value)
          },
          (receipt) => {
            if (ticket === epoch.current) {
              received.current.add(receipt.id)
              setMessage('Received by Framebriar.')
            }
          },
        )
      }
      const stored = await inspectScope(scope)
      if (ticket !== epoch.current || sequence !== refreshSequence.current)
        return
      setOutboxState(stored)
      if (next.status === 'connected') await box.current.start()
      else box.current.stop()
    } catch {
      if (ticket === epoch.current) {
        box.current?.stop()
        setConnection({ status: 'unavailable' })
      }
    }
  }, [])
  useEffect(() => {
    void refresh()
    const timer = window.setInterval(() => void refresh(), 60000)
    const end = () => {
      epoch.current++
      captureEpoch.current++
      opening.current = false
      saveAbort.current?.abort()
      box.current?.stop()
      const scope = currentScope.current
      void saveSettled.current
        .then(() => {
          if (scope) return clearKnownScope(scope)
        })
        .catch(() => {})
      setOpen(false)
      setNotes('')
      setMedia(null)
    }
    window.addEventListener('feedback-session-ended', end)
    const captureGeneration = captureEpoch
    const generation = epoch
    return () => {
      clearInterval(timer)
      generation.current++
      captureGeneration.current++
      saveAbort.current?.abort()
      box.current?.stop()
      window.removeEventListener('feedback-session-ended', end)
    }
  }, [refresh])
  useEffect(() => {
    if (!open) return
    dialog.current?.querySelector<HTMLTextAreaElement>('textarea')?.focus()
    return () => {
      returnFocus.current?.focus()
    }
  }, [open])
  const close = () => {
    if (pending.current) return
    captureEpoch.current++
      opening.current = false
    setCapturing(false)
    setOpen(false)
    if (!prepared.current) annotation.discard()
    if (!prepared.current) setMedia(null)
  }
  const clear = async () => {
    const scope = currentScope.current
    if (enabled() && !scope)
      throw new Error(
        'Cannot verify local feedback ownership. Refresh connection status before clearing.',
      )
    epoch.current++
    captureEpoch.current++
      opening.current = false
    saveAbort.current?.abort()
    box.current?.stop()
    setCapturing(false)
    await saveSettled.current
    if (box.current) await box.current.clear()
    await clearKnownScope(scope)
    box.current = null
    prepared.current = null
    setLocked(false)
    setNotes('')
    setMedia(null)
    setOutboxState({ state: 'available', items: 0, bytes: 0 })
    setMessage('Unsent reports deleted from this device.')
  }
  const signOut = async () => {
    if (
      (notes || outboxState.items || pending.current) &&
      !window.confirm(
        'Signing out deletes this account’s unsent feedback on this device. Continue?',
      )
    )
      return
    try {
      await clear()
    } catch {
      if (
        !window.confirm(
          'Unsent feedback may remain on this device because storage could not be verified. Sign out anyway? It will not be sent while signed out.',
        )
      ) {
        setMessage('Sign-out canceled. Local feedback could not be cleared.')
        return
      }
      // The user accepted retention, not deletion. Revoke all pending work
      // before ending authentication; no local ownership is guessed here.
      epoch.current++
      captureEpoch.current++
      opening.current = false
      saveAbort.current?.abort()
      box.current?.stop()
      await saveSettled.current
    }
    try {
      await auth.signOut()
    } catch {
      setMessage('Could not sign out. Please try again.')
    }
  }
  const send = async () => {
    if (
      pending.current ||
      capturing ||
      !notes.trim() ||
      !box.current ||
      connection.status !== 'connected'
    )
      return
    let settleSave: () => void = () => {}
    saveSettled.current = new Promise<void>((resolve) => {
      settleSave = resolve
    })
    pending.current = true
    saveAbort.current = new AbortController()
    setBusy(true)
    const ticket = epoch.current
    try {
      if (!prepared.current) {
        const image = await finalScreenshot({ include: includeImage, original: media, drawing: annotation.drawing })
        if (image.kind === 'failed' || image.kind === 'too-large') {
          setMessage('Could not prepare the reviewed screenshot within the 4 MB limit. Undo marks or uncheck Include screenshot to send notes only.')
          return
        }
        if (ticket !== epoch.current) return
        prepared.current = await prepareLocalFeedback({
          blob: image.kind === 'ready' ? image.blob : null,
          notes,
          captureContext: {
            gameId: feedbackConfig.appId,
            buildSha: feedbackConfig.buildSha,
            scene: media && includeImage
              ? captureScene.current
              : (screens[location.pathname] ?? 'other'),
            capturedAt: (media && includeImage ? captureTime.current : null) ?? new Date().toISOString(),
            sessionId: crypto.randomUUID(),
            seed: null,
            platform: innerWidth < 640 ? 'mobile' : 'desktop',
            input: matchMedia('(pointer: coarse)').matches ? 'touch' : 'mouse',
            breadcrumbs: [],
          },
        })
      }
      if (ticket !== epoch.current) return
      setLocked(true)
      const draft = prepared.current
      await box.current.enqueue({
        report: draft.report,
        media: draft.media,
        includeDiagnostics: false,
        signal: saveAbort.current.signal,
      })
      if (ticket !== epoch.current) return
      setMessage(
        received.current.has(draft.report.id)
          ? 'Received by Framebriar.'
          : 'Saved on this device. Waiting for a verified receipt.',
      )
      setNotes('')
      setMedia(null)
      prepared.current = null
      setLocked(false)
      setOpen(false)
    } catch {
      if (ticket === epoch.current)
        setMessage(
          'Could not confirm the save. Your reviewed report is kept unchanged for a safe retry. Copy your notes before leaving; delete unsent reports in House rules to start over.',
        )
    } finally {
      settleSave()
      pending.current = false
      setBusy(false)
    }
  }
  useEffect(() => {
    captureEpoch.current++
    opening.current = false
    // eslint-disable-next-line react-hooks/set-state-in-effect -- navigation must revoke asynchronous capture and its visible sheet together
    setCapturing(false)
    setOpen(false)
    if (!pending.current && !prepared.current) setMedia(null)
  }, [location.pathname])
  const openSheet = async (opener: HTMLElement) => {
    if (opening.current || open || connection.status !== 'connected') return
    returnFocus.current = opener
    if (prepared.current) { setOpen(true); return }
    const ticket = ++captureEpoch.current
    const account = epoch.current
    const scope = currentScope.current
    const path = location.pathname
    opening.current = true
    setCapturing(true)
    setMessage('')
    annotation.discard()
    const startedAt = new Date().toISOString()
    const blob = feedbackConfig.screenshots && connection.screenshots ? await captureViewport() : null
    if (ticket !== captureEpoch.current || account !== epoch.current) return
    opening.current = false
    setCapturing(false)
    if (scope !== currentScope.current || currentConnection.current.status !== 'connected') return
    setMedia(blob)
    setIncludeImage(!!blob)
    captureTime.current = blob ? startedAt : null
    captureScene.current = screens[path] ?? (path.startsWith('/recipes/') ? 'recipes' : 'other')
    if (!blob && feedbackConfig.screenshots) setMessage('Screenshot unavailable on this screen. You can send notes only.')
    opening.current = false
    setCapturing(false)
    setOpen(true)
  }
  const controls: Controls = {
    open: (opener) => {
      void openSheet(opener)
    },
    signOut,
    connection,
    refresh,
    clear,
    count: outboxState.items,
  }
  return (
    <Context.Provider value={controls}>
      {children}
      {message && !open && (
        <p className="feedback-status" role="status">
          {message}
        </p>
      )}
      {open && (
        <div className="feedback-backdrop" data-feedback-ui>
          <div
            className="feedback-sheet"
            ref={dialog}
            role="dialog"
            aria-modal="true"
            aria-labelledby="feedback-title"
            onKeyDown={(event) => {
              if (event.key === 'Escape') {
                event.preventDefault()
                close()
              }
              if (event.key === 'Tab') {
                const targets = Array.from(
                  dialog.current!.querySelectorAll<HTMLElement>(
                    'button:not(:disabled),textarea,input,a[href]',
                  ),
                )
                const first = targets[0],
                  last = targets.at(-1)
                if (event.shiftKey && document.activeElement === first) {
                  event.preventDefault()
                  last?.focus()
                } else if (!event.shiftKey && document.activeElement === last) {
                  event.preventDefault()
                  first?.focus()
                }
              }
            }}
          >
            <h2 id="feedback-title">App feedback</h2>
            <p>
              Send a bug or product suggestion, separate from meal preferences.
            </p>
            <label>
              Notes
              <textarea
                aria-label="Notes"
                readOnly={locked}
                maxLength={2000}
                value={notes}
                disabled={busy}
                onChange={(event) => {
                  setNotes(event.target.value)
                  prepared.current = null
                }}
              />
            </label>
            <p>
              Sent to Framebriar: your notes, reviewed screen code, device/input
              class, app/environment, build version and capture time. No
              automatic account identity or URLs. Optional screenshots show the visible app, including recipes and page text, with private regions excluded.
            </p>
            {feedbackConfig.screenshots && connection.screenshots && (
              <>
                <label>
                  <input
                    type="checkbox"
                    checked={includeImage && !!media}
                    disabled={busy || locked || capturing || !media}
                    onChange={(event) => { setIncludeImage(event.target.checked); annotation.discard(); prepared.current = null }}
                  />{' '}
                  Include screenshot
                </label>
                <p>
                  Screenshots may show private recipes, meal plans or household
                  preferences. Review before sending.
                </p>
              </>
            )}
            {media && includeImage && <ScreenshotEditor blob={media} draft={annotation} disabled={busy || locked} />}
            <p>
              Delivered reports remain in Framebriar. Clearing this device’s
              outbox does not erase them.
            </p>
            {connection.status !== 'connected' && (
              <p>
                Feedback is {connection.status}. Connect in House rules to send.
                Your notes remain while this session is open.
              </p>
            )}
            <button
              type="button"
              disabled={
                busy ||
                capturing ||
                !notes.trim() ||
                connection.status !== 'connected'
              }
              onClick={() => void send()}
            >
              Send
            </button>
            <button type="button" disabled={busy} onClick={close}>
              Cancel
            </button>
            <p role="status">{message}</p>
            <p>
              {capturing
                ? 'Preparing screenshot…'
                : busy
                  ? 'Saving…'
                  : `${outboxState.items} unsent report(s) on this device.`}
            </p>
          </div>
        </div>
      )}
    </Context.Provider>
  )
}
export function FeedbackAction() {
  const context = useContext(Context)
  return context && enabled() && context.connection.status === 'connected' ? (
    <button
      className="feedback-action"
      type="button"
      onClick={(event) => context.open(event.currentTarget)}
    >
      Feedback
    </button>
  ) : null
}
export function FeedbackSignOut({
  onSignOut,
}: { onSignOut?: () => void } = {}) {
  const context = useContext(Context)
  return (
    <button
      className="sign-out-link"
      type="button"
      onClick={() => {
        if (context) void context.signOut()
        else onSignOut?.()
      }}
    >
      Sign out
    </button>
  )
}
export function FeedbackSettings() {
  const context = useContext(Context)
  return context && enabled() ? (
    <ConnectedFeedbackSettings context={context} />
  ) : null
}

function ConnectedFeedbackSettings({ context }: { context: Controls }) {
  const auth = useAuth()
  const [invitation, setInvitation] = useState(''),
    [busy, setBusy] = useState(false),
    [notice, setNotice] = useState('')
  const act = async (action: 'connect' | 'disconnect') => {
    if (busy) return
    if (
      action === 'disconnect' &&
      !window.confirm(
        'Disconnect Framebriar and delete this account’s unsent reports on this device? Delivered reports remain in Framebriar.',
      )
    )
      return
    setBusy(true)
    try {
      await request(
        action,
        auth.getToken,
        JSON.stringify(action === 'connect' ? { invitation } : {}),
      )
      setInvitation('')
      if (action === 'disconnect') await context.clear()
      await context.refresh()
      setNotice(action === 'connect' ? 'Connection checked.' : 'Disconnected.')
    } catch {
      setNotice(
        'Could not confirm the change. Your invitation is kept here for retry. Check status before retrying.',
      )
      await context.refresh()
    } finally {
      setBusy(false)
    }
  }
  return (
    <section className="feedback-settings paper-card" data-feedback-private>
      <h2>App feedback connection</h2>
      <p>Separate from your dietary and meal-planning rules.</p>
      <p role="status">
        {context.connection.status}
        {context.connection.expiresAt
          ? ` · expires ${context.connection.expiresAt}`
          : ''}
      </p>
      {context.connection.status !== 'connected' && (
        <>
          <label>
            Private Framebriar invitation
            <input
              type="password"
              autoComplete="off"
              value={invitation}
              onChange={(e) => setInvitation(e.target.value)}
              disabled={busy}
            />
          </label>
          <button
            disabled={busy || !invitation.trim()}
            onClick={() => void act('connect')}
          >
            Connect
          </button>
        </>
      )}
      {context.connection.status === 'connected' && <button type="button" onClick={(event) => context.open(event.currentTarget)}>Send feedback</button>}
      <button disabled={busy} onClick={() => void context.refresh()}>
        Refresh status
      </button>
      <button disabled={busy} onClick={() => void act('disconnect')}>
        Disconnect
      </button>
      <button
        disabled={busy}
        onClick={() => {
          if (
            window.confirm(
              'Delete unsent reports on this device? Delivered reports remain in Framebriar.',
            )
          )
            void context
              .clear()
              .then(context.refresh)
              .catch(() =>
                setNotice('Storage could not be cleared. Please retry.'),
              )
        }}
      >
        Delete unsent reports ({context.count})
      </button>
      <p>{notice}</p>
      <p>
        Unsent reports remain on this device until delivered or cleared.
        Sign-out removes them. A failed save stays in the review sheet until you
        leave this session.
      </p>
    </section>
  )
}

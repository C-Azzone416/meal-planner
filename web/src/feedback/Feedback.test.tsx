// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { AuthProvider } from '../auth/AuthProvider'
import {
  FeedbackAction,
  FeedbackProvider,
  FeedbackSettings,
  FeedbackSignOut,
} from './Feedback'
import type { AuthService } from '../auth/service'

const fixture = vi.hoisted(() => ({
  enqueue: vi.fn(),
  clear: vi.fn(),
  request: vi.fn(),
  capture: vi.fn(),
  inspect: vi.fn(),
  start: vi.fn(),
  clearScope: vi.fn(),
  receipt: null as
    null | ((receipt: { id: string; gameId: string; status: string }) => void),
}))
vi.mock('feedback-capture', () => ({ captureViewport: fixture.capture }))
vi.mock('./config', () => ({
  feedbackConfig: {
    enabled: true,
    environment: 'preview',
    origin: 'https://meal.test',
    appId: 'meal-preview',
    buildSha: 'a'.repeat(40),
    screenshots: true,
  },
}))
vi.mock('./client', () => ({
  enabled: () => true,
  clearKnownScope: fixture.clearScope,
  reconcileScope: vi.fn().mockResolvedValue(undefined),
  request: fixture.request,
  inspectScope: fixture.inspect,
  makeOutbox: (
    _scope: unknown,
    _token: unknown,
    _valid: unknown,
    _change: unknown,
    receipt: typeof fixture.receipt,
  ) => {
    fixture.receipt = receipt
    return {
      start: fixture.start,
      stop: vi.fn(),
      clear: fixture.clear,
      enqueue: fixture.enqueue,
    }
  },
}))
const service: AuthService = {
  getSession: vi.fn().mockResolvedValue({
    id: 'account-one',
    email: 'private@example.test',
    emailVerified: true,
  }),
  getToken: vi.fn().mockResolvedValue('session'),
  signInWithPassword: vi.fn(),
  sendPasswordResetCode: vi.fn(),
  setPassword: vi.fn(),
  signOut: vi.fn().mockResolvedValue(undefined),
}
function mount() {
  render(
    <MemoryRouter initialEntries={['/week']}>
      <AuthProvider serviceFactory={async () => service}>
        <FeedbackProvider>
          <FeedbackAction />
          <FeedbackSignOut />
          <FeedbackSettings />
        </FeedbackProvider>
      </AuthProvider>
    </MemoryRouter>,
  )
}
beforeEach(() => {
  vi.mocked(service.signOut).mockClear()
  fixture.inspect
    .mockReset()
    .mockResolvedValue({ state: 'available', items: 0, bytes: 0 })
  fixture.start.mockReset().mockResolvedValue(undefined)
  fixture.clearScope.mockReset().mockResolvedValue(undefined)
  fixture.enqueue.mockReset().mockResolvedValue({ status: 'saved-local' })
  fixture.clear.mockReset().mockResolvedValue({ items: 0 })
  fixture.request.mockReset().mockResolvedValue({
    status: 'connected',
    environment: 'preview',
    appId: 'meal-preview',
    buildSha: 'a'.repeat(40),
    outboxKey: 'opaqueAccountKey0123456789',
    screenshots: true,
  })
  fixture.capture.mockReset()
  vi.stubGlobal('matchMedia', () => ({ matches: false }))
  vi.spyOn(window, 'confirm').mockReturnValue(true)
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
async function open() {
  mount()
  await screen.findByText('connected')
  fireEvent.click(screen.getByRole('button', { name: 'Feedback' }))
  return screen.getByLabelText('Notes')
}
test('Cancel/reopen retains notes without sending and restores focus', async () => {
  const notes = await open()
  expect(document.activeElement).toBe(notes)
  fireEvent.change(notes, { target: { value: 'Review only' } })
  expect((screen.getByRole('checkbox') as HTMLInputElement).checked).toBe(false)
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(fixture.enqueue).not.toHaveBeenCalled()
  expect(document.activeElement).toBe(
    screen.getByRole('button', { name: 'Feedback' }),
  )
  fireEvent.click(screen.getByRole('button', { name: 'Feedback' }))
  expect((screen.getByLabelText('Notes') as HTMLTextAreaElement).value).toBe(
    'Review only',
  )
})
test('repeated Send saves once; Received requires a separate receipt', async () => {
  const notes = await open()
  fireEvent.change(notes, { target: { value: 'Synthetic bug' } })
  const send = screen.getByRole('button', { name: 'Send' })
  fireEvent.click(send)
  fireEvent.click(send)
  await screen.findByText(
    'Saved on this device. Waiting for a verified receipt.',
  )
  expect(fixture.enqueue).toHaveBeenCalledOnce()
  expect(screen.queryByText('Received by Framebriar.')).toBeNull()
  const { report } = fixture.enqueue.mock.calls[0][0]
  expect(report.scene).toBe('week')
  expect(report.buildSha).toBe('a'.repeat(40))
  expect(report.sessionId).toMatch(/^[0-9a-f-]{36}$/)
  expect(JSON.stringify(report)).not.toContain('private@example')
  fixture.receipt?.({
    id: report.id,
    gameId: report.gameId,
    status: 'processed',
  })
  await screen.findByText('Received by Framebriar.')
})
test('storage failure retains immutable review and retry ID', async () => {
  fixture.enqueue.mockRejectedValueOnce(Error('quota'))
  const notes = await open()
  fireEvent.change(notes, { target: { value: 'Keep me' } })
  fireEvent.click(screen.getByRole('button', { name: 'Send' }))
  await screen.findByText(/Could not confirm the save/)
  expect((screen.getByLabelText('Notes') as HTMLTextAreaElement).value).toBe(
    'Keep me',
  )
  fireEvent.click(screen.getByRole('button', { name: 'Send' }))
  await screen.findByText(/Saved on this device/)
  expect(fixture.enqueue.mock.calls[0][0].report.id).toBe(
    fixture.enqueue.mock.calls[1][0].report.id,
  )
})
test('late capture cannot resurrect screenshot after Cancel', async () => {
  let finish: (blob: Blob) => void = () => {}
  fixture.capture.mockReturnValue(
    new Promise<Blob>((resolve) => {
      finish = resolve
    }),
  )
  await open()
  fireEvent.click(screen.getByRole('checkbox'))
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  finish(new Blob(['synthetic'], { type: 'image/png' }))
  fireEvent.click(screen.getByRole('button', { name: 'Feedback' }))
  await waitFor(() => expect(screen.queryByRole('img')).toBeNull())
  expect((screen.getByRole('checkbox') as HTMLInputElement).checked).toBe(false)
})
test('signout aborts pending save and waits before clear', async () => {
  let aborted = false
  fixture.enqueue.mockImplementation(
    ({ signal }: { signal: AbortSignal }) =>
      new Promise((_, reject) => {
        signal.addEventListener('abort', () => {
          aborted = true
          reject(Error('aborted'))
        })
      }),
  )
  const notes = await open()
  fireEvent.change(notes, { target: { value: 'Pending' } })
  fireEvent.click(screen.getByRole('button', { name: 'Send' }))
  await waitFor(() => expect(fixture.enqueue).toHaveBeenCalledOnce())
  fireEvent.click(screen.getByRole('button', { name: 'Sign out' }))
  await waitFor(() => expect(fixture.clear).toHaveBeenCalledOnce())
  expect(aborted).toBe(true)
  expect(window.confirm).toHaveBeenCalled()
})

test.each(['expired', 'revoked', 'not-connected'])(
  'reload with %s access binds saved scope without delivery and can delete',
  async (status) => {
    fixture.request.mockResolvedValue({
      status,
      environment: 'preview',
      appId: 'meal-preview',
      buildSha: 'a'.repeat(40),
      outboxKey: 'opaqueAccountKey0123456789',
    })
    fixture.inspect.mockResolvedValue({
      state: 'available',
      items: 2,
      bytes: 20,
    })
    mount()
    await screen.findByRole('button', { name: 'Delete unsent reports (2)' })
    expect(fixture.start).not.toHaveBeenCalled()
    fireEvent.click(
      screen.getByRole('button', { name: 'Delete unsent reports (2)' }),
    )
    await waitFor(() =>
      expect(fixture.clearScope).toHaveBeenCalledWith(
        'meal-preview-opaqueAccountKey0123456789',
      ),
    )
  },
)

test('reload with expired access still clears the verified saved queue on signout', async () => {
  fixture.request.mockResolvedValue({
    status: 'expired',
    environment: 'preview',
    appId: 'meal-preview',
    buildSha: 'a'.repeat(40),
    outboxKey: 'opaqueAccountKey0123456789',
  })
  fixture.inspect.mockResolvedValue({ state: 'available', items: 2, bytes: 20 })
  mount()
  await screen.findByRole('button', { name: 'Delete unsent reports (2)' })
  fireEvent.click(screen.getByRole('button', { name: 'Sign out' }))
  await waitFor(() =>
    expect(fixture.clearScope).toHaveBeenCalledWith(
      'meal-preview-opaqueAccountKey0123456789',
    ),
  )
  expect(fixture.start).not.toHaveBeenCalled()
  expect(window.confirm).toHaveBeenCalled()
})

test('unavailable status without verified scope cannot claim deletion', async () => {
  fixture.request.mockRejectedValue(Error('offline'))
  mount()
  fireEvent.click(
    await screen.findByRole('button', { name: 'Delete unsent reports (0)' }),
  )
  await screen.findByText('Storage could not be cleared. Please retry.')
  expect(fixture.clearScope).not.toHaveBeenCalled()
  expect(
    screen.queryByText('Unsent reports deleted from this device.'),
  ).toBeNull()
})

test.each([false, true])(
  'unverified cleanup fallback signout respects confirmation %s',
  async (confirmed) => {
    fixture.request.mockRejectedValue(Error('offline'))
    vi.mocked(window.confirm).mockReturnValue(confirmed)
    mount()
    fireEvent.click(await screen.findByRole('button', { name: 'Sign out' }))
    await waitFor(() =>
      expect(window.confirm).toHaveBeenCalledWith(
        'Unsent feedback may remain on this device because storage could not be verified. Sign out anyway? It will not be sent while signed out.',
      ),
    )
    await waitFor(() =>
      expect(service.signOut).toHaveBeenCalledTimes(confirmed ? 1 : 0),
    )
    expect(fixture.clearScope).not.toHaveBeenCalled()
    expect(
      screen.queryByText('Unsent reports deleted from this device.'),
    ).toBeNull()
  },
)

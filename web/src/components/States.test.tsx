// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom'
import { afterEach, describe, expect, test, vi } from 'vitest'
import { OfflineBanner } from './OfflineBanner'
import { PageErrorBoundary } from './PageErrorBoundary'
import { RouteFocus, focusPageHeading } from './RouteFocus'
import { PageSkeleton, StateCard } from './StateCard'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('StateCard', () => {
  test('shows a heading, one sentence and a link action', () => {
    render(
      <MemoryRouter>
        <StateCard heading="No recipes yet" action={{ label: 'Add recipe', to: '/recipes/new' }}>Add your first one.</StateCard>
      </MemoryRouter>,
    )
    expect(screen.getByRole('heading', { name: 'No recipes yet' })).toBeTruthy()
    expect(screen.getByText('Add your first one.')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Add recipe' }).getAttribute('href')).toBe('/recipes/new')
  })

  test('a button action calls back', () => {
    const onClick = vi.fn()
    render(<StateCard heading="Oops" action={{ label: 'Try again', onClick }} />)
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(onClick).toHaveBeenCalled()
  })

  test('a disabled action is rendered with a visible reason, not hidden', () => {
    const onClick = vi.fn()
    render(
      <StateCard heading="Feedback" action={{ label: 'Rate dinner', onClick, disabled: true, disabledReason: 'Feedback after cooking — coming soon' }} />,
    )
    const button = screen.getByRole('button', { name: 'Rate dinner' }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
    expect(screen.getByText('Feedback after cooking — coming soon')).toBeTruthy()
    expect(button.getAttribute('aria-describedby')).toBe(screen.getByText('Feedback after cooking — coming soon').id)
    fireEvent.click(button)
    expect(onClick).not.toHaveBeenCalled()
  })

  test('errors use role=alert, empties have no live role', () => {
    const { rerender } = render(<StateCard heading="Broken" tone="error" />)
    expect(screen.getByRole('alert')).toBeTruthy()
    rerender(<StateCard heading="Empty" />)
    expect(screen.queryByRole('alert')).toBeNull()
  })
})

describe('PageSkeleton', () => {
  test('is a busy region with an accessible label', () => {
    render(<PageSkeleton label="Loading recipes" cards={2} />)
    const region = screen.getByRole('status', { name: 'Loading recipes' })
    expect(region.getAttribute('aria-busy')).toBe('true')
    expect(region.querySelectorAll('.page-skeleton__card')).toHaveLength(2)
  })
})

describe('PageErrorBoundary', () => {
  function Boom({ fail }: { fail: { current: boolean } }) {
    if (fail.current) throw new Error('kaboom')
    return <h1>Page is fine</h1>
  }

  test('shows a StateCard and Try again remounts the page', () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const fail = { current: true }
    render(<PageErrorBoundary><Boom fail={fail} /></PageErrorBoundary>)
    expect(screen.getByRole('alert')).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Something went wrong on this page' })).toBeTruthy()
    fail.current = false
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(screen.getByRole('heading', { name: 'Page is fine' })).toBeTruthy()
    expect(screen.queryByRole('alert')).toBeNull()
  })

  test('still fails gracefully if Try again fails again', () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const fail = { current: true }
    render(<PageErrorBoundary><Boom fail={fail} /></PageErrorBoundary>)
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(screen.getByRole('alert')).toBeTruthy()
  })

  test('clears the error when resetKey changes (navigation)', () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const fail = { current: true }
    const { rerender } = render(<PageErrorBoundary resetKey="/a"><Boom fail={fail} /></PageErrorBoundary>)
    expect(screen.getByRole('alert')).toBeTruthy()
    fail.current = false
    rerender(<PageErrorBoundary resetKey="/b"><Boom fail={fail} /></PageErrorBoundary>)
    expect(screen.getByRole('heading', { name: 'Page is fine' })).toBeTruthy()
  })
})

describe('OfflineBanner', () => {
  function setOnline(value: boolean) {
    vi.spyOn(window.navigator, 'onLine', 'get').mockReturnValue(value)
  }

  test('is hidden while online', () => {
    setOnline(true)
    render(<OfflineBanner />)
    expect(screen.queryByRole('status')).toBeNull()
  })

  test('shows immediately when already offline', () => {
    setOnline(false)
    render(<OfflineBanner />)
    expect(screen.getByRole('status').textContent).toBe('You’re offline. This app uses browser-only storage; check the save status before leaving.')
  })

  test('reacts to offline and online events', () => {
    setOnline(true)
    render(<OfflineBanner />)
    act(() => {
      setOnline(false)
      window.dispatchEvent(new Event('offline'))
    })
    expect(screen.getByRole('status')).toBeTruthy()
    act(() => {
      setOnline(true)
      window.dispatchEvent(new Event('online'))
    })
    expect(screen.queryByRole('status')).toBeNull()
  })
})

describe('route focus', () => {
  function Harness() {
    const navigate = useNavigate()
    return (
      <>
        <button onClick={() => navigate('/b')}>go</button>
        <main>
          <Routes>
            <Route path="/a" element={<h1>Page A</h1>} />
            <Route path="/b" element={<h1>Page B</h1>} />
          </Routes>
        </main>
        <RouteFocus />
      </>
    )
  }

  test('does not steal focus on first load, then focuses the new h1 after navigation', async () => {
    render(<MemoryRouter initialEntries={['/a']}><Harness /></MemoryRouter>)
    expect(document.activeElement).toBe(document.body)
    fireEvent.click(screen.getByRole('button', { name: 'go' }))
    const heading = await screen.findByRole('heading', { name: 'Page B' })
    await vi.waitFor(() => expect(document.activeElement).toBe(heading))
    expect(heading.getAttribute('tabindex')).toBe('-1')
  })

  test('focusPageHeading reports whether a heading exists', () => {
    expect(focusPageHeading()).toBe(false)
    render(<h1>Hi</h1>)
    expect(focusPageHeading()).toBe(true)
    expect(document.activeElement?.textContent).toBe('Hi')
  })
})

// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import App from './App'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

test('shows API status on success', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ status: 'ok', service: 'meal-planner-api', version: 'dev' }),
    }),
  )
  render(<App />)
  expect(await screen.findByText('ok')).toBeTruthy()
})

test('shows an error state when the API fails', async () => {
  vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')))
  render(<App />)
  expect((await screen.findByRole('alert')).textContent).toContain('offline')
})

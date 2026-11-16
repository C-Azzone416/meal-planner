// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { NewRecipe } from '../planner/types'
import { DraftReviewAPIError, type HostedDraftResponse } from './api'
import { ProjectionEditor } from './ProjectionEditor'

const item: HostedDraftResponse = {
  api_version: 'draft-intent-v1',
  draft: {
    schema_version: 1, content_schema_version: 1, id: '11111111-1111-4111-8111-111111111111', revision: 7,
    status: 'draft', origin: 'agent', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    content: {
      title: '<img src=x onerror=alert(1)> soup',
      ingredients: [{ text: '<b>onion</b>' }], instructions: [{ text: 'Simmer <script>bad()</script>' }],
      sources: [{ id: 's1', kind: 'user_supplied', label: '<iframe>bad</iframe>' }], guidance: [],
    },
    review: { revision: 9, state: 'ready', draft_revision: 7 }, approved_recipe_id: null,
  },
  validation: { notice: 'Structural only', errors: [], warnings: [] },
}

afterEach(cleanup)

function change(label: RegExp | string, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } })
}

function completeForm() {
  fireEvent.click(screen.getByRole('button', { name: 'Use canonical title' }))
  change(/^Icon/, '🍲')
  change(/^Active minutes/, '0')
  change(/^Total minutes/, '30')
  change(/^Attention/, 'low')
  change(/^Default servings/, '2')
  change(/^Tags/, 'none')
  fireEvent.click(screen.getByRole('button', { name: 'Use source text as name' }))
  change(/^Amount choice/, 'none')
  change(/^Unit choice/, 'none')
  change(/^Aisle/, 'Produce')
  change(/^Pantry status/, 'no')
  fireEvent.click(screen.getByRole('button', { name: 'Use canonical text' }))
  change(/^Phase/, 'cook_tonight')
  change(/^Timer choice/, 'none')
}

it('renders hostile canonical text as inert context and does not silently populate projection fields', () => {
  render(<ProjectionEditor item={item} onSave={vi.fn()} />)
  expect(screen.getByText('<img src=x onerror=alert(1)> soup')).toBeTruthy()
  expect(document.querySelector('img')).toBeNull()
  expect(document.querySelector('script')).toBeNull()
  expect((screen.getByLabelText(/^Recipe title/) as HTMLInputElement).value).toBe('')
  expect((screen.getByLabelText(/^Amount choice/) as HTMLSelectElement).value).toBe('')
  expect(screen.getByText(/draft revision/).textContent).toContain('7')
  expect(screen.getByText(/draft revision/).textContent).toContain('9')
})

it('blocks double submit and sends one complete projection bound to the supplied item', async () => {
  let resolve!: () => void
  const pending = new Promise<void>((done) => { resolve = done })
  const onSave = vi.fn<(draft: HostedDraftResponse, projection: NewRecipe) => Promise<void>>(() => pending)
  render(<ProjectionEditor item={item} onSave={onSave} />)
  completeForm()
  const save = screen.getByRole('button', { name: 'Save reviewed recipe' })
  fireEvent.click(save)
  fireEvent.click(save)
  expect(onSave).toHaveBeenCalledTimes(1)
  expect(onSave.mock.calls[0][0]).toBe(item)
  expect(onSave.mock.calls[0][1]).toEqual(expect.objectContaining({ activeMinutes: 0, totalMinutes: 30, tags: [] }))
  expect(onSave.mock.calls[0][1].ingredients[0]).toEqual(expect.objectContaining({ quantity: null, unit: '', aisle: 'Produce', isPantry: false }))
  expect((screen.getByRole('button', { name: 'Saving exact projection…' }) as HTMLButtonElement).disabled).toBe(true)
  resolve()
  expect((await screen.findByRole('status')).textContent).toContain('Recipe saved')
})

it('locks an uncertain attempt and retries the identical projection', async () => {
  const onSave = vi.fn()
    .mockRejectedValueOnce(new TypeError('Network failed'))
    .mockResolvedValueOnce({})
  render(<ProjectionEditor item={item} onSave={onSave} />)
  completeForm()
  fireEvent.click(screen.getByRole('button', { name: 'Save reviewed recipe' }))
  expect((await screen.findByRole('alert')).textContent).toContain('Network failed')
  expect((screen.getByLabelText(/^Recipe title/) as HTMLInputElement).closest('fieldset')?.disabled).toBe(true)
  fireEvent.click(screen.getByRole('button', { name: 'Retry exact projection' }))
  await waitFor(() => expect(onSave).toHaveBeenCalledTimes(2))
  expect(onSave.mock.calls[1][0]).toBe(item)
  expect(onSave.mock.calls[1][1]).toEqual(onSave.mock.calls[0][1])
})

it('fails closed on a stale revision and prevents another save until reload', async () => {
  const onSave = vi.fn().mockRejectedValue(new DraftReviewAPIError(409, 'changed'))
  render(<ProjectionEditor item={item} onSave={onSave} />)
  completeForm()
  fireEvent.click(screen.getByRole('button', { name: 'Save reviewed recipe' }))
  expect((await screen.findByRole('alert')).textContent).toContain('stale')
  const retry = screen.getByRole('button', { name: 'Reload required' }) as HTMLButtonElement
  expect(retry.disabled).toBe(true)
  fireEvent.click(retry)
  expect(onSave).toHaveBeenCalledTimes(1)
})

it('unlocks fields only after a definitive pre-write rejection', async () => {
  const onSave = vi.fn().mockRejectedValue(new DraftReviewAPIError(400, 'Rejected without changing the draft.'))
  render(<ProjectionEditor item={item} onSave={onSave} />)
  completeForm()
  fireEvent.click(screen.getByRole('button', { name: 'Save reviewed recipe' }))
  expect((await screen.findByRole('alert')).textContent).toContain('Rejected')
  expect((screen.getByLabelText(/^Recipe title/) as HTMLInputElement).closest('fieldset')?.disabled).toBe(false)
  expect((screen.getByRole('button', { name: 'Save reviewed recipe' }) as HTMLButtonElement).disabled).toBe(false)
})

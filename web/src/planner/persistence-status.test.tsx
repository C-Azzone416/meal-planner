// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, expect, test } from 'vitest'
import { accountStorageKey, LEGACY_NOTICE, serialize, STORAGE_KEY, type StorageLike } from './persist'
import { createSeedState } from './seed'
import { PlannerProvider, usePlanner } from './store'

afterEach(cleanup)
function storageFake() {
  const data = new Map<string, string>()
  let failRead = false, failWrite = false
  const storage: StorageLike = {
    getItem: key => { if (failRead) throw new Error('disabled'); return data.get(key) ?? null },
    setItem: (key, value) => { if (failWrite) throw new Error('quota'); data.set(key, value) },
    removeItem: key => { data.delete(key) },
  }
  return { data, storage, failRead: (value: boolean) => { failRead = value }, failWrite: (value: boolean) => { failWrite = value } }
}
function mount(storage: StorageLike | null, accountId = 'account-a') {
  return renderHook(usePlanner, { wrapper: ({ children }: { children: ReactNode }) => <PlannerProvider accountId={accountId} storage={storage}>{children}</PlannerProvider> })
}

test('disabled storage and quota failure are visibly unsaved; retry persists latest changes and reload restores', () => {
  const disabled = mount(null)
  expect(disabled.result.current.persistenceStatus).toBe('unsaved')
  expect(disabled.result.current.persistenceNotice).toContain('not saved')
  disabled.unmount()
  const fake = storageFake()
  const first = mount(fake.storage)
  expect(first.result.current.persistenceStatus).toBe('saved')
  fake.failWrite(true)
  act(() => first.result.current.setSkipped(5))
  expect(first.result.current.persistenceStatus).toBe('unsaved')
  expect(first.result.current.plan.nights[5].status).toBe('skipped')
  fake.failWrite(false)
  act(() => first.result.current.retryPersistence())
  expect(first.result.current.persistenceStatus).toBe('saved')
  first.unmount()
  expect(mount(fake.storage).result.current.plan.nights[5].status).toBe('skipped')
})

test('failed initial read never overwrites hidden existing data, even after retry', () => {
  const fake = storageFake()
  const original = createSeedState(); original.plan.nights[5].status = 'skipped'
  const raw = serialize(original)
  fake.data.set(accountStorageKey('account-a'), raw)
  fake.failRead(true)
  const first = mount(fake.storage)
  act(() => first.result.current.setSkipped(6))
  expect(first.result.current.persistenceStatus).toBe('unsaved')
  expect(fake.data.get(accountStorageKey('account-a'))).toBe(raw)
  fake.failRead(false)
  act(() => first.result.current.retryPersistence())
  expect(first.result.current.persistenceStatus).toBe('unsaved')
  expect(fake.data.get(accountStorageKey('account-a'))).toBe(raw)
  first.unmount()
  expect(mount(fake.storage).result.current.plan.nights[5].status).toBe('skipped')
})

test('account switching remounts data, isolates queued writes and preserves legacy data without guessing ownership', () => {
  const fake = storageFake()
  const legacy = createSeedState(); legacy.recipes[0].title = 'Legacy private title'
  const raw = serialize(legacy); fake.data.set(STORAGE_KEY, raw)
  let accountId = 'account-a'
  const view = renderHook(usePlanner, { wrapper: ({ children }: { children: ReactNode }) => <PlannerProvider storage={fake.storage} accountId={accountId}>{children}</PlannerProvider> })
  expect(view.result.current.legacyNotice).toBe(LEGACY_NOTICE)
  expect(view.result.current.recipes[0].title).not.toBe('Legacy private title')
  const oldActions = view.result.current
  act(() => { oldActions.setSkipped(5); accountId = 'account-b'; view.rerender() })
  expect(view.result.current.plan.nights[5].status).toBe('open')
  act(() => oldActions.setSkipped(6))
  expect(view.result.current.plan.nights[6].status).toBe('open')
  expect(JSON.parse(fake.data.get(accountStorageKey('account-b'))!).state.plan.nights[6].status).toBe('open')
  act(() => view.result.current.setSkipped(0))
  accountId = 'account-a'; view.rerender()
  expect(view.result.current.plan.nights[0].status).toBe('open')
  expect(fake.data.get(STORAGE_KEY)).toBe(raw)
})


test('retry after a failed empty-store read enables later saves too', () => {
  const fake = storageFake(); fake.failRead(true)
  const view = mount(fake.storage)
  expect(view.result.current.persistenceStatus).toBe('unsaved')
  fake.failRead(false)
  act(() => view.result.current.retryPersistence())
  expect(view.result.current.persistenceStatus).toBe('saved')
  act(() => view.result.current.setSkipped(5))
  expect(view.result.current.persistenceStatus).toBe('saved')
  expect(JSON.parse(fake.data.get(accountStorageKey('account-a'))!).state.plan.nights[5].status).toBe('skipped')
})

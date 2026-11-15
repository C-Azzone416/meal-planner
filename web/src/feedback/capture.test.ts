// @vitest-environment jsdom
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { captureAllowed, captureReady, includeInCapture, captureViewport } from './capture'
import { toBlob } from 'html-to-image'
vi.mock('html-to-image', () => ({ toBlob: vi.fn() }))
beforeEach(() => {
  history.replaceState({},'', '/week')
  document.body.innerHTML = '<header><nav>Recipes</nav></header><main><button>Monday meal</button><div data-feedback-private>PRIVATE</div><input type="password" value="SECRET"></main>'
  const real = window.getComputedStyle.bind(window)
  vi.spyOn(window,'getComputedStyle').mockImplementation(node => real(node))
  vi.mocked(toBlob).mockReset().mockResolvedValue(new Blob(['png'],{type:'image/png'}))
})
afterEach(() => { document.body.innerHTML=''; vi.restoreAllMocks() })
test('Packed-style filter preserves cards, buttons/nav and rejects private subtrees before clone', async () => {
  expect(includeInCapture(document.querySelector('button')!)).toBe(true)
  expect(includeInCapture(document.querySelector('nav')!)).toBe(true)
  expect(includeInCapture(document.querySelector('[data-feedback-private]')!)).toBe(false)
  expect(includeInCapture(document.querySelector('input')!)).toBe(false)
  expect(await captureViewport()).toBeInstanceOf(Blob)
  expect(toBlob).toHaveBeenCalledWith(document.documentElement,expect.objectContaining({filter:includeInCapture,skipFonts:true,pixelRatio:1}))
})
test('auth blocked, settings and recipe routes eligible; identity and sensitive portals fail closed', () => {
  expect(captureAllowed('/sign-in')).toBe(false)
  expect(captureAllowed('/house-rules')).toBe(true)
  expect(captureAllowed('/recipes/by-id/test/edit')).toBe(true)
  const node = document.createElement('div'); node.dataset.feedbackIdentity=''; document.body.append(node)
  expect(captureReady()).toBe(false)
  node.dataset.feedbackPrivate=''; expect(captureReady()).toBe(true)
  node.dataset.feedbackSensitivePortal=''; expect(captureReady()).toBe(false)
})
test('no capture resource fetching, SVG child exclusion bypass or iframe cloning', () => {
  for (const tag of ['head','style','link','script','img','iframe','canvas','video']) expect(includeInCapture(document.createElement(tag))).toBe(false)
  const background = document.createElement('div'); background.style.backgroundImage='url(https://private.example/image)'; document.body.append(background)
  expect(includeInCapture(background)).toBe(false)
  const svg = document.createElementNS('http://www.w3.org/2000/svg','svg'); svg.innerHTML='<foreignObject>secret</foreignObject>'; document.body.append(svg)
  expect(includeInCapture(svg)).toBe(false)
})
test('document scroll uses margins not transforms, size/route/session changes discard result', async () => {
  vi.spyOn(window,'scrollY','get').mockReturnValue(123)
  await captureViewport()
  expect(toBlob).toHaveBeenLastCalledWith(document.documentElement,expect.objectContaining({style:{marginTop:'-123px',marginLeft:'0px'}}))
  vi.mocked(toBlob).mockImplementation(async () => { window.dispatchEvent(new Event('feedback-session-ended')); return new Blob(['png'],{type:'image/png'}) })
  expect(await captureViewport()).toBeNull()
  vi.mocked(toBlob).mockImplementation(async () => { history.replaceState({},'', '/house-rules'); return new Blob(['png'],{type:'image/png'}) })
  expect(await captureViewport()).toBeNull()
})
test('over8MiB and over decoded viewport bounds degrade without oversized canvas', async () => {
  vi.mocked(toBlob).mockResolvedValue(new Blob([new Uint8Array(8 * 1024 * 1024 + 1)],{type:'image/png'}))
  expect(await captureViewport()).toBeNull()
  vi.mocked(toBlob).mockClear()
  vi.spyOn(window,'innerWidth','get').mockReturnValue(4097)
  expect(await captureViewport()).toBeNull(); expect(toBlob).not.toHaveBeenCalled()
})

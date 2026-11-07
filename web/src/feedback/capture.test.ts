// @vitest-environment jsdom
import { afterEach, expect, test, vi } from 'vitest'
import { sanitizedViewport, captureViewport } from './capture'
import { toBlob } from 'html-to-image'
vi.mock('html-to-image', () => ({ toBlob: vi.fn() }))
afterEach(() => {
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})
function fixture() {
  history.replaceState({}, '', '/week')
  document.body.innerHTML =
    '<main class="page-stack"><div>Visible meal</div><div data-feedback-private><span>ACCOUNT_CANARY</span></div><input type="password" value="PASSWORD_CANARY"><div data-feedback-ui>INVITATION_CANARY</div><a href="/?secret=URL_CANARY">A recipe</a><img src="SECRET_IMAGE_CANARY"><div style="visibility:hidden">HIDDEN_CANARY</div></main>'
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
    x: 10,
    y: 20,
    left: 10,
    top: 20,
    bottom: 60,
    right: 200,
    width: 190,
    height: 40,
    toJSON: () => ({}),
  })
}
test('sensitive regions, controls, URLs and hidden content never enter rasterizer', async () => {
  fixture()
  vi.mocked(toBlob).mockImplementation(async (root) => {
    expect(root.outerHTML).not.toMatch(/CANARY/)
    expect(root.textContent).toContain('Visible meal')
    return new Blob(['fixture'], { type: 'image/png' })
  })
  expect(await captureViewport()).toBeInstanceOf(Blob)
  expect(toBlob).toHaveBeenCalledOnce()
})
test('sensitive and unknown pages refuse rasterization', async () => {
  fixture()
  for (const path of ['/house-rules', '/sign-in', '/unknown']) {
    history.replaceState({}, '', path)
    expect(sanitizedViewport()).toBeNull()
  }
})
test('viewport coordinates are independent of document/nested scrolling', () => {
  fixture()
  vi.spyOn(window, 'scrollY', 'get').mockReturnValue(1200)
  const clone = sanitizedViewport()!
  expect(clone.firstElementChild?.getAttribute('style')).toContain('top: 20px')
  expect(clone.style.height).toBe(`${innerHeight}px`)
})
test('oversized capture degrades to text only', async () => {
  fixture()
  vi.mocked(toBlob).mockResolvedValue(
    new Blob([new Uint8Array(3 * 1024 * 1024 + 1)], { type: 'image/png' }),
  )
  expect(await captureViewport()).toBeNull()
})

test('ancestor opacity and overflow clipping omit hidden canaries before rasterization', () => {
  fixture()
  const main = document.querySelector('.page-stack')!
  const hidden = document.createElement('div')
  hidden.style.opacity = '0'
  hidden.innerHTML = '<span>OPACITY_CANARY</span>'
  const clipped = document.createElement('div')
  clipped.style.overflow = 'hidden'
  clipped.innerHTML = '<span>OVERFLOW_CANARY</span>'
  main.append(hidden, clipped)
  clipped.getBoundingClientRect = () => ({
    x: 0,
    y: 0,
    left: 0,
    top: 0,
    right: 1,
    bottom: 1,
    width: 1,
    height: 1,
    toJSON: () => ({}),
  })
  expect(sanitizedViewport()!.textContent).not.toContain('OPACITY_CANARY')
  expect(sanitizedViewport()!.textContent).not.toContain('OVERFLOW_CANARY')
})

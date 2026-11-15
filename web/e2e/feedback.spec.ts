import { test, expect, type Page, devices } from '@playwright/test'

import { rm } from 'node:fs/promises'

// Ephemeral WebKit rejects IndexedDB Blob writes even without this app/SDK.
// Use a disposable persistent profile to exercise real screenshot persistence.
const durableTest = test.extend({
  page: async ({ page, browserName, playwright }, providePage, testInfo) => {
    if (browserName !== 'webkit') { await providePage(page); return }
    const profile = testInfo.outputPath('synthetic-webkit-profile')
    const context = await playwright.webkit.launchPersistentContext(profile, { ...devices['iPhone 13'], baseURL: 'http://localhost:4181' })
    try { await providePage(await context.newPage()) }
    finally { await context.close(); await rm(profile, { recursive: true, force: true }) }
  },
})

test.beforeEach(async ({ page }) => {
  // Browser fixtures never contact external fonts, auth or Framebriar.
  await page.route('https://**', route => route.abort())
  // The merged shell loads real account recipes. Feedback fixtures isolate that
  // unrelated read without calling a live recipe database.
  await page.route('**/api/recipes?*', route => route.fulfill({ json: { recipes: [], nextCursor: null } }))
})

const sha = '1234567890123456789012345678901234567890'
async function fakeAPI(page: Page, options: { connected?: boolean; failReports?: boolean } = {}) {
  // Real browser -> authenticated Go handlers -> SQL store -> synthetic Framebriar.
  const reset = await page.request.post('/__fixture/reset', { data: { connected: options.connected ?? true } })
  expect(reset.ok()).toBeTruthy()
  const failure = await page.request.post('/__fixture/failure', { data: { failReports: options.failReports ?? false } })
  expect(failure.ok()).toBeTruthy()
  await page.goto('/e2e/feedback.html')
  await expect(page.getByRole('link', { name: 'House rules', exact: true })).toBeVisible()
  return { evidence: async () => (await page.request.get('/__fixture/evidence')).json() as Promise<{reports: Array<Record<string, unknown>>}> }
}

durableTest('connect, cancel and explicit Send produce one verified receipt', async ({ page }) => {
  const fake = await fakeAPI(page, { connected: false })
  await page.getByRole('link', { name: 'House rules', exact: true }).click()
  await page.getByLabel('Private Framebriar invitation').fill('a'.repeat(43))
  await page.getByRole('button', { name: 'Connect', exact: true }).click()
  await expect(page.getByText('Connection checked.')).toBeVisible()
  await page.getByRole('button', { name: 'Feedback', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('checkbox')).toBeChecked()
  await dialog.getByRole('checkbox').uncheck()
  await dialog.getByRole('textbox', { name: 'Notes', exact: true }).fill('Synthetic keyboard navigation feedback')
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click()
  expect((await fake.evidence()).reports).toHaveLength(0)
  await expect(page.getByRole('button', { name: 'Feedback', exact: true })).toBeFocused()
  await page.getByRole('button', { name: 'Feedback', exact: true }).click()
  await expect(dialog.getByRole('textbox', { name: 'Notes', exact: true })).toHaveValue('Synthetic keyboard navigation feedback')
  await dialog.getByRole('checkbox').uncheck()
  await dialog.getByRole('button', { name: 'Send', exact: true }).click()
  await expect(page.getByText('Received by Framebriar.', { exact: true })).toBeVisible()
  expect((await fake.evidence()).reports).toHaveLength(1)
  expect(JSON.stringify((await fake.evidence()).reports[0])).toContain(sha)
  expect(JSON.stringify((await fake.evidence()).reports[0])).toContain('meal-planner-preview')
  expect(JSON.stringify((await fake.evidence()).reports[0])).not.toContain('synthetic@example.test')
  expect(JSON.stringify((await fake.evidence()).reports[0])).not.toContain('synthetic-account-one')
  expect(JSON.stringify((await fake.evidence()).reports[0])).not.toContain('synthetic-invitation')
  expect(JSON.stringify((await fake.evidence()).reports[0])).not.toContain('http://localhost')
  page.once('dialog', dialog => dialog.accept())
  await page.getByRole('button', { name: 'Disconnect', exact: true }).click()
  await expect(page.getByText('Disconnected.', { exact: true })).toBeVisible()
})

durableTest('failed delivery remains Saved and sign-out requires discard confirmation', async ({ page }) => {
  const fake = await fakeAPI(page, { failReports: true })
  await page.getByRole('button', { name: 'Feedback', exact: true }).click()
  await page.getByLabel('Notes', { exact: true }).fill('Synthetic offline note')
  await page.getByRole('button', { name: 'Send', exact: true }).click()
  await expect(page.getByText('Saved on this device. Waiting for a verified receipt.', { exact: true })).toBeVisible()
  await expect.poll(async () => (await fake.evidence()).reports.length).toBeGreaterThanOrEqual(1)
  page.once('dialog', dialog => dialog.dismiss())
  await page.getByRole('button', { name: 'Sign out', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Feedback', exact: true })).toBeVisible()
  page.once('dialog', dialog => dialog.accept())
  await page.getByRole('button', { name: 'Sign out', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Feedback', exact: true })).toHaveCount(0)
})

durableTest('opt-in screenshot excludes sensitive canaries and uses the visible viewport', async ({ page }) => {
  const fake = await fakeAPI(page)
  await page.evaluate(() => {
    const main = document.querySelector('.page-stack')!
    const privateCanary = document.createElement('div')
    privateCanary.dataset.feedbackPrivate = ''
    privateCanary.textContent = 'SYNTHETIC PRIVATE ACCOUNT CANARY'
    privateCanary.style.cssText = 'position:fixed;top:120px;left:20px;width:80px;height:60px;background:rgb(255,0,255);color:rgb(255,0,255);z-index:20'
    const safeCanary = document.createElement('div')
    safeCanary.textContent = 'Safe screen'
    safeCanary.style.cssText = 'position:fixed;top:200px;left:20px;width:80px;height:60px;background:rgb(0,255,255);color:rgb(0,255,255);z-index:20'
    const clipped = document.createElement('div')
    clipped.style.cssText = 'position:fixed;top:280px;left:20px;width:80px;height:20px;overflow:hidden'
    const clippedCanary = document.createElement('div')
    clippedCanary.textContent = 'INVISIBLE CLIPPED CANARY'
    clippedCanary.style.cssText = 'position:relative;top:45px;width:80px;height:60px;background:rgb(255,255,0);color:rgb(255,255,0)'
    clipped.append(clippedCanary)
    const icon = document.createElement('button')
    icon.style.cssText = 'position:fixed;top:220px;left:130px;width:50px;height:50px;background:white'
    icon.innerHTML = '<svg width="32" height="32"><rect width="32" height="32" fill="rgb(17,83,209)" /></svg>'
    main.append(privateCanary, safeCanary, clipped, icon)
    window.scrollTo(0, 180)
  })
  await page.getByRole('button', { name: 'Feedback', exact: true }).click()
  await expect(page.getByRole('checkbox')).toBeChecked()
  const screenshot = page.locator('.shot-image')
  await expect(screenshot).toBeVisible()
  const pixels = await screenshot.evaluate(async element => {
    const img = element as HTMLImageElement
    await img.decode()
    const canvas = document.createElement('canvas')
    canvas.width = img.naturalWidth; canvas.height = img.naturalHeight
    const ctx = canvas.getContext('2d')!
    ctx.drawImage(img, 0, 0)
    const data = ctx.getImageData(0, 0, canvas.width, canvas.height).data
    let privatePixels = 0, safePixels = 0, clippedPixels = 0, iconPixels = 0
    for (let i = 0; i < data.length; i += 4) {
      if (data[i] === 17 && data[i + 1] === 83 && data[i + 2] === 209) iconPixels++
      if (data[i] > 245 && data[i + 1] < 10 && data[i + 2] > 245) privatePixels++
      if (data[i] < 10 && data[i + 1] > 245 && data[i + 2] > 245) safePixels++
      if (data[i] > 245 && data[i + 1] > 245 && data[i + 2] < 10) clippedPixels++
    }
    return { privatePixels, safePixels, clippedPixels, iconPixels, width: canvas.width, height: canvas.height, viewportWidth: innerWidth, viewportHeight: innerHeight }
  })
  expect(pixels.privatePixels).toBe(0)
  expect(pixels.clippedPixels).toBe(0)
  expect(pixels.safePixels).toBeGreaterThan(100)
  expect(pixels.iconPixels).toBeGreaterThan(100)
  expect(pixels.width).toBe(pixels.viewportWidth)
  expect(pixels.height).toBe(pixels.viewportHeight)
  expect((await fake.evidence()).reports).toHaveLength(0)
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  expect((await fake.evidence()).reports).toHaveLength(0)
})


durableTest('two devices share enrollment; a second account does not inherit enrollment or unsent notes', async ({ page, browser }) => {
  await fakeAPI(page, { failReports: true })
  await page.getByRole('button', { name: 'Feedback', exact: true }).click()
  await page.getByRole('textbox', { name: 'Notes', exact: true }).fill('SYNTHETIC ACCOUNT ONE UNSENT')
  await page.getByRole('button', { name: 'Send', exact: true }).click()
  await expect(page.getByText('Saved on this device. Waiting for a verified receipt.', { exact: true })).toBeVisible()

  const device = await browser.newContext({ baseURL: 'http://localhost:4181', viewport: { width: 390, height: 844 } })
  await device.route('https://**', route => route.abort())
  try {
    const second = await device.newPage()
    await second.goto('/e2e/feedback.html')
    await second.getByRole('link', { name: 'House rules', exact: true }).click()
    await expect(second.getByRole('status').filter({ hasText: /^connected/ })).toBeVisible()
    await expect(second.getByRole('button', { name: 'Delete unsent reports (0)', exact: true })).toBeVisible()
  } finally { await device.close() }

  // A confirmed provider identity switch ends the old session and clears only
  // its queue. This uses the same device storage as the original account.
  await page.evaluate(() => sessionStorage.setItem('synthetic-account', 'two'))
  await page.getByRole('link', { name: 'House rules', exact: true }).click()
  await page.getByRole('button', { name: 'Refresh status', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Feedback', exact: true })).toHaveCount(0)
  await page.goto('/e2e/feedback.html')
  await page.getByRole('link', { name: 'House rules', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: /^not-connected/ })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Delete unsent reports (0)', exact: true })).toBeVisible()
  await expect(page.getByText('SYNTHETIC ACCOUNT ONE UNSENT')).toHaveCount(0)
})

// Synthetic upstream contract only, not proof of live Framebriar idempotency.
for (const failure of ['502', 'invalid receipt'] as const) durableTest(`screenshot media ${failure} survives close and reload until final receipt`, async ({ page }) => {
  test.setTimeout(60000)
  await fakeAPI(page)
  const failures = async (failMedia: boolean) => {
    expect((await page.request.post('/__fixture/failure', { data: { failMedia: failMedia && failure === '502', invalidMediaReceipt: failMedia && failure === 'invalid receipt' } })).ok()).toBeTruthy()
  }
  const evidence = async () => (await page.request.get('/__fixture/evidence')).json() as Promise<{
    reports: Array<{ id: string; media: { sha256: string } }>
    mediaAttempts: Array<{ id: string; sha256: string; bytes: number; failed: boolean }>
    accepted: Record<string, boolean>
  }>
  await failures(true)
  await page.getByRole('button', { name: 'Feedback', exact: true }).click()
  await page.getByLabel('Notes', { exact: true }).fill('Synthetic screenshot retry')
  await expect(page.locator('.shot-image')).toBeVisible()
  await page.getByRole('button', { name: 'Send', exact: true }).click()
  await expect.poll(async () => (await evidence()).mediaAttempts.length).toBeGreaterThan(0)
  const first = (await evidence()).mediaAttempts[0]
  expect(first.failed).toBe(true)
  expect(first.bytes).toBeGreaterThan(0)
  expect((await evidence()).reports.length).toBeGreaterThan(0)
  await expect(page.getByText('Received by Framebriar.', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Feedback', exact: true }).click()
  await expect(page.getByText('1 unsent report(s) on this device.', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  await page.goto('/e2e/feedback.html')
  await page.getByRole('button', { name: 'Feedback', exact: true }).click()
  await expect(page.getByText('1 unsent report(s) on this device.', { exact: true })).toBeVisible()
  await expect(page.getByText('Received by Framebriar.', { exact: true })).toHaveCount(0)
  expect(Object.keys((await evidence()).accepted)).toHaveLength(0)
  await failures(false)
  // New instance restores the actual IndexedDB outbox and starts delivery.
  await page.goto('/e2e/feedback.html')
  await expect(page.getByText('Received by Framebriar.', { exact: true })).toBeVisible({ timeout: 30000 })
  await page.getByRole('button', { name: 'Feedback', exact: true }).click()
  await expect(page.getByText('0 unsent report(s) on this device.', { exact: true })).toBeVisible()
  const final = await evidence()
  expect(Object.keys(final.accepted)).toEqual([first.id])
  expect(final.mediaAttempts.some(a => !a.failed)).toBe(true)
  for (const a of final.mediaAttempts) { expect(a.id).toBe(first.id); expect(a.sha256).toBe(first.sha256) }
  for (const r of final.reports) { expect(r.id).toBe(first.id); expect(r.media.sha256).toBe(first.sha256) }
})


durableTest('faithful automatic image keeps real buttons/nav/cards and annotation marks reach explicit Send', async ({ page }) => {
  await page.addInitScript(() => {
    const original = window.fetch
    window.fetch = async (input, init) => {
      if (String(input).endsWith('/api/feedback/reports') && init?.body instanceof FormData) {
        const media = init.body.get('media')
        if (media instanceof Blob) (window as unknown as { sentMedia: Blob }).sentMedia = media
      }
      return original(input, init)
    }
  })
  await fakeAPI(page)
  // Compare actual UI pixels against the captured image, not just image presence.
  const regions = await page.evaluate(() => {
    const candidates = [...document.querySelectorAll('.wordmark,.nav-link,.day-button,.meal-card,.paper-title')]
    return candidates.map(node => { const r = node.getBoundingClientRect(); return { x:Math.ceil(r.left)+3,y:Math.ceil(r.top)+3,w:Math.floor(r.width)-6,h:Math.floor(r.height)-6 } })
      .filter(r => r.w>5 && r.h>5 && r.x>=0 && r.y>=0 && r.x+r.w<innerWidth && r.y+r.h<innerHeight)
  })
  expect(regions.length).toBeGreaterThanOrEqual(2)
  const original = (await page.screenshot({scale:'css'})).toString('base64')
  const requests: string[] = []
  page.on('request', request => { if (/^https:/.test(request.url())) requests.push(request.url()) })
  await page.getByRole('button',{name:'Feedback',exact:true}).click()
  const image = page.locator('.shot-image')
  await expect(image).toBeVisible()
  const parity = await image.evaluate(async (element, { original, regions }) => {
    const expected = new Image(); expected.src='data:image/png;base64,'+original; await expected.decode()
    const actual = element as HTMLImageElement; await actual.decode()
    const pixels = (img:HTMLImageElement) => { const c=document.createElement('canvas'); c.width=img.naturalWidth;c.height=img.naturalHeight; const ctx=c.getContext('2d')!;ctx.drawImage(img,0,0);return ctx.getImageData(0,0,c.width,c.height).data }
    const a=pixels(expected), b=pixels(actual)
    return regions.map(r => { let matching=0,total=0;for(let y=r.y;y<r.y+r.h;y++)for(let x=r.x;x<r.x+r.w;x++){ const i=(y*actual.naturalWidth+x)*4;total++;if(Math.abs(a[i]-b[i])+Math.abs(a[i+1]-b[i+1])+Math.abs(a[i+2]-b[i+2])<36)matching++ }return matching/total })
  },{original,regions})
  expect(parity.filter(value => value > .8).length).toBeGreaterThanOrEqual(2)
  expect(requests).toEqual([])
  const canvas=page.locator('.shot-canvas')
  await canvas.scrollIntoViewIfNeeded()
  const box=(await canvas.boundingBox())!
  await canvas.dispatchEvent('pointerdown',{pointerId:1,pointerType:'mouse',button:0,clientX:box.x+box.width/2,clientY:box.y+box.height/2})
  await canvas.dispatchEvent('pointerup',{pointerId:1,pointerType:'mouse',button:0,clientX:box.x+box.width/2+20,clientY:box.y+box.height/2})
  await expect(page.getByText('1 mark.',{exact:true})).toBeVisible()
  await page.getByRole('button',{name:'Undo',exact:true}).click()
  await expect(page.getByText('Draw on the screenshot with your finger or mouse.')).toBeVisible()
  await canvas.dispatchEvent('pointerdown',{pointerId:2,pointerType:'touch',button:0,clientX:box.x+box.width/2,clientY:box.y+box.height/2})
  await canvas.dispatchEvent('pointerup',{pointerId:2,pointerType:'touch',button:0,clientX:box.x+box.width/2+20,clientY:box.y+box.height/2})
  await page.getByLabel('Notes',{exact:true}).fill('Synthetic annotated UI feedback')
  await page.getByRole('button',{name:'Send',exact:true}).click()
  await page.waitForFunction(() => (window as unknown as { sentMedia?: Blob }).sentMedia instanceof Blob)
  const sentPixels=await page.evaluate(async () => {
    const blob=(window as unknown as {sentMedia:Blob}).sentMedia
    const image=await createImageBitmap(blob)
    const c=document.createElement('canvas');c.width=image.width;c.height=image.height
    const ctx=c.getContext('2d')!;ctx.drawImage(image,0,0);image.close()
    const data=ctx.getImageData(0,0,c.width,c.height).data;let n=0
    for(let i=0;i<data.length;i+=4)if(data[i]===217&&data[i+1]===45&&data[i+2]===32)n++
    const digest = Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', await blob.arrayBuffer()))).map(b=>b.toString(16).padStart(2,'0')).join('')
    return { redPixels:n,digest }
  })
  expect(sentPixels.redPixels).toBeGreaterThan(10)
  await expect(page.getByText('Received by Framebriar.',{exact:true})).toBeVisible()
  const evidence = await (await page.request.get('/__fixture/evidence')).json()
  expect(Object.keys(evidence.accepted)).toHaveLength(1)
  expect(evidence.mediaAttempts.some((attempt: {sha256:string;failed:boolean}) => !attempt.failed && attempt.sha256 === sentPixels.digest)).toBe(true)
})

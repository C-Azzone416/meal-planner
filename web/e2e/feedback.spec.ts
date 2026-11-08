import { test, expect, type Page } from '@playwright/test'

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
  await expect(page.getByRole('button', { name: 'Feedback', exact: true })).toBeVisible()
  return { evidence: async () => (await page.request.get('/__fixture/evidence')).json() as Promise<{reports: Array<Record<string, unknown>>}> }
}

test('connect, cancel and explicit Send produce one verified receipt', async ({ page }) => {
  const fake = await fakeAPI(page, { connected: false })
  await page.getByRole('link', { name: 'House rules', exact: true }).click()
  await page.getByLabel('Private Framebriar invitation').fill('a'.repeat(43))
  await page.getByRole('button', { name: 'Connect', exact: true }).click()
  await expect(page.getByText('Connection checked.')).toBeVisible()
  await page.getByRole('button', { name: 'Feedback', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('checkbox')).not.toBeChecked()
  await dialog.getByRole('textbox', { name: 'Notes', exact: true }).fill('Synthetic keyboard navigation feedback')
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click()
  expect((await fake.evidence()).reports).toHaveLength(0)
  await expect(page.getByRole('button', { name: 'Feedback', exact: true })).toBeFocused()
  await page.getByRole('button', { name: 'Feedback', exact: true }).click()
  await expect(dialog.getByRole('textbox', { name: 'Notes', exact: true })).toHaveValue('Synthetic keyboard navigation feedback')
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

test('failed delivery remains Saved and sign-out requires discard confirmation', async ({ page }) => {
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

test('opt-in screenshot excludes sensitive canaries and uses the visible viewport', async ({ page }) => {
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
    main.append(privateCanary, safeCanary, clipped)
    window.scrollTo(0, 180)
  })
  await page.getByRole('button', { name: 'Feedback', exact: true }).click()
  await expect(page.getByRole('checkbox')).not.toBeChecked()
  await page.getByRole('checkbox').click()
  const screenshot = page.getByAltText('Review the sanitized visible screen screenshot')
  await expect(screenshot).toBeVisible()
  const pixels = await screenshot.evaluate(async element => {
    const img = element as HTMLImageElement
    await img.decode()
    const canvas = document.createElement('canvas')
    canvas.width = img.naturalWidth; canvas.height = img.naturalHeight
    const ctx = canvas.getContext('2d')!
    ctx.drawImage(img, 0, 0)
    const data = ctx.getImageData(0, 0, canvas.width, canvas.height).data
    let privatePixels = 0, safePixels = 0, clippedPixels = 0
    for (let i = 0; i < data.length; i += 4) {
      if (data[i] > 245 && data[i + 1] < 10 && data[i + 2] > 245) privatePixels++
      if (data[i] < 10 && data[i + 1] > 245 && data[i + 2] > 245) safePixels++
      if (data[i] > 245 && data[i + 1] > 245 && data[i + 2] < 10) clippedPixels++
    }
    return { privatePixels, safePixels, clippedPixels, width: canvas.width, height: canvas.height, viewportWidth: innerWidth, viewportHeight: innerHeight }
  })
  expect(pixels.privatePixels).toBe(0)
  expect(pixels.clippedPixels).toBe(0)
  expect(pixels.safePixels).toBeGreaterThan(100)
  expect(pixels.width).toBe(pixels.viewportWidth)
  expect(pixels.height).toBe(pixels.viewportHeight)
  expect((await fake.evidence()).reports).toHaveLength(0)
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  expect((await fake.evidence()).reports).toHaveLength(0)
})


test('two devices share enrollment; a second account does not inherit enrollment or unsent notes', async ({ page, browser }) => {
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

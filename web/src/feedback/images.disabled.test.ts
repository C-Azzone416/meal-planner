import { expect, test, vi } from 'vitest'
import { finalScreenshot, useAnnotationDraft, ScreenshotEditor } from './images.disabled'
test('disabled seam is text-only and never silently accepts an unexpected image', async () => {
  const draft = useAnnotationDraft('synthetic')
  const decode = vi.fn(); vi.stubGlobal('createImageBitmap', decode)
  try {
    expect(await finalScreenshot({include:false,original:null,drawing:draft.drawing})).toEqual({kind:'none'})
    expect(await finalScreenshot({include:true,original:new Blob(['png']),drawing:draft.drawing})).toEqual({kind:'failed'})
    expect(ScreenshotEditor({blob:new Blob(['png']),draft,disabled:false})).toBeNull()
    expect(decode).not.toHaveBeenCalled()
  } finally { vi.unstubAllGlobals() }
})

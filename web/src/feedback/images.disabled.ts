// Screenshot-disabled build seam. Type-only imports are erased; no editor,
// image decoder, compositor, drawing hook or canvas implementation is bundled.
import type { finalScreenshot as activeFinal } from './annotation-final'
import type { AnnotationDraft } from './use-annotation-draft'
import type { ComponentProps } from 'react'
import type { ScreenshotEditor as ActiveEditor } from './ScreenshotEditor'
const noop = () => {}
const draft: AnnotationDraft = {
  drawing: { strokes: [], active: null, cleared: null },
  begin: noop, extend: noop, end: noop, cancel: noop,
  undo: noop, clear: noop, discard: noop,
}
export function useAnnotationDraft(_scope: string | null): AnnotationDraft { void _scope; return draft }
export function ScreenshotEditor(_props: ComponentProps<typeof ActiveEditor>) { void _props; return null }
export const finalScreenshot: typeof activeFinal = async ({ include, original }) =>
  include && original ? { kind: 'failed' } : { kind: 'none' }

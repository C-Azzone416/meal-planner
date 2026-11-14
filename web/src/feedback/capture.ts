import { toBlob } from 'html-to-image'

export const MAX_MEDIA = 3 * 1024 * 1024
const blocked =
  'input,textarea,select,button,img,svg,canvas,video,iframe,[data-feedback-private],[data-feedback-ui],header,nav'
const safePages = new Set([
  '/week',
  '/recipes',
  '/prep',
  '/shopping',
  '/drafts',
  '/cook',
])

// Omit rather than reconstruct partially clipped content. This keeps scroll
// containers, opacity-hidden ancestors and overflow masks from exposing text
// that was not visible in the original viewport.
function visibleWithinAncestors(node: HTMLElement, rect: DOMRect): boolean {
  for (
    let ancestor: HTMLElement | null = node;
    ancestor;
    ancestor = ancestor.parentElement
  ) {
    const style = getComputedStyle(ancestor)
    if (
      style.opacity === '0' ||
      style.display === 'none' ||
      style.visibility === 'hidden' ||
      style.contentVisibility === 'hidden'
    )
      return false
    if (ancestor === node) continue
    const bounds = ancestor.getBoundingClientRect()
    const clipsX = /^(hidden|clip|scroll|auto)$/.test(
      style.overflowX === 'visible'
        ? style.overflow
        : style.overflowX || style.overflow,
    )
    const clipsY = /^(hidden|clip|scroll|auto)$/.test(
      style.overflowY === 'visible'
        ? style.overflow
        : style.overflowY || style.overflow,
    )
    if (clipsX && (rect.left < bounds.left || rect.right > bounds.right))
      return false
    if (clipsY && (rect.top < bounds.top || rect.bottom > bounds.bottom))
      return false
  }
  return true
}

/** Build a fresh, inert drawing of the visible viewport. No source attributes,
 * controls, URLs, or hidden DOM are cloned, even into an intermediate SVG. */
export function sanitizedViewport(): HTMLElement | null {
  if (
    !safePages.has(location.pathname) ||
    (window.visualViewport && window.visualViewport.scale !== 1)
  )
    return null
  const source = document.querySelector('.page-stack')
  if (!source) return null
  const width = window.innerWidth,
    height = window.innerHeight
  const root = document.createElement('div')
  Object.assign(root.style, {
    position: 'relative',
    width: `${width}px`,
    height: `${height}px`,
    overflow: 'hidden',
    background: '#f4f1e9',
  })
  for (const node of source.querySelectorAll('*')) {
    if (!(node instanceof HTMLElement) || node.closest(blocked)) continue
    const rect = node.getBoundingClientRect(),
      style = getComputedStyle(node)
    if (
      !rect.width ||
      !rect.height ||
      rect.bottom <= 0 ||
      rect.right <= 0 ||
      rect.top >= height ||
      rect.left >= width ||
      style.visibility !== 'visible' ||
      style.display === 'none' ||
      !visibleWithinAncestors(node, rect)
    )
      continue
    // Direct text nodes only: children are positioned independently from actual
    // viewport rectangles, so document and nested scroll offsets are exact.
    const text = Array.from(node.childNodes)
      .filter((n) => n.nodeType === Node.TEXT_NODE)
      .map((n) => n.textContent)
      .join('')
      .trim()
    const draw = document.createElement('div')
    Object.assign(draw.style, {
      position: 'absolute',
      left: `${rect.left}px`,
      top: `${rect.top}px`,
      width: `${rect.width}px`,
      height: `${rect.height}px`,
      boxSizing: 'border-box',
      overflow: 'hidden',
      backgroundColor: style.backgroundColor,
      borderRadius: style.borderRadius,
      color: style.color,
      font: style.font,
      lineHeight: style.lineHeight,
      padding: style.padding,
      textAlign: style.textAlign,
      whiteSpace: style.whiteSpace,
    })
    draw.textContent = text
    root.append(draw)
  }
  return root
}

export async function captureViewport(): Promise<Blob | null> {
  // Artifact guard: executable marker exists only in the active capture module.
  const marker = 'FRAMEBRIAR_CAPTURE_ACTIVE'
  const root = sanitizedViewport()
  if (root) root.dataset.capture = marker
  if (!root) return null
  // html-to-image copies computed styles. A detached tree has incomplete
  // computed styles in real engines, so mount only this already-sanitized
  // drawing offscreen. No original/private node is ever mounted or cloned.
  const mount = document.createElement('div')
  Object.assign(mount.style, { position: 'fixed', left: '-100000px', top: '0' })
  mount.setAttribute('aria-hidden', 'true')
  mount.append(root)
  document.body.append(mount)
  try {
    const blob = await toBlob(root, {
      width: innerWidth,
      height: innerHeight,
      pixelRatio: 1,
      skipFonts: true,
    })
    return blob?.type === 'image/png' && blob.size > 0 && blob.size <= MAX_MEDIA
      ? blob
      : null
  } finally {
    mount.remove()
  }
}

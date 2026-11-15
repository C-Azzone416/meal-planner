// Adapted from Packed Smart c1a381e screenshot-capture/capture-exclusion.
// Capture the real viewport, not a reconstructed drawing. Private nodes are
// rejected BEFORE html-to-image clones them; settings can still be reported.
import { toBlob } from 'html-to-image'
export const MAX_MEDIA = 8 * 1024 * 1024
export function captureAllowed(path: string) {
  return /^\/(week|prep|shopping|drafts|cook|house-rules)$/.test(path) || /^\/recipes(?:\/[^/]+){0,3}$/.test(path)
}
const excluded = '[data-feedback-private],[data-feedback-ui],[data-feedback-exclude],[data-feedback-sensitive-portal],input[type="password"],input[type="email"]'
function resourceStyle(node: Element) {
  // No capture-time resource requests, including CSS images/masks/pseudo-elements.
  for (const pseudo of [null, '::before', '::after']) {
    const style = getComputedStyle(node, pseudo)
    for (let i = 0; i < style.length; i++)
      if (/url\s*\(/i.test(style.getPropertyValue(style[i]))) return true
  }
  return false
}
export function includeInCapture(node: Node): boolean {
  if (!(node instanceof Element)) return true
  if (node.closest(excluded)) return false
  if (node.matches('head,script,style,link,meta,iframe,object,embed,video,audio,canvas,img,foreignObject')) return false
  // html-to-image deep-clones SVG descendants. Permit only inert local shapes,
  // never a nested foreignObject, image/use, resource URL or private marker.
  if (node instanceof SVGElement) {
    const svg = node.closest('svg')!
    if (svg.querySelector(`${excluded},image,use,foreignObject,script,style,a`) ||
      [svg,...svg.querySelectorAll('*')].some(n => Array.from(n.attributes).some(a => /href|^on/i.test(a.name)) || resourceStyle(n))) return false
  }
  if (resourceStyle(node)) return false
  const style = getComputedStyle(node)
  return style.display !== 'none' && style.visibility !== 'hidden' && style.opacity !== '0'
}
export function captureReady() {
  if (!captureAllowed(location.pathname) || document.querySelector('[data-feedback-sensitive-portal]') ||
    (window.visualViewport && window.visualViewport.scale !== 1)) return false
  for (const region of document.querySelectorAll('[data-feedback-identity]'))
    if (!region.closest('[data-feedback-private],[data-feedback-exclude="account-identity"]')) return false
  return !resourceStyle(document.documentElement)
}
export async function captureViewport(): Promise<Blob | null> {
  if (!captureReady()) return null
  const width = innerWidth, height = innerHeight, path = location.href
  if (!width || !height || width > 4096 || height > 4096 || width * height > 12_000_000) return null
  const root = document.documentElement
  root.dataset.capture = 'FRAMEBRIAR_CAPTURE_ACTIVE'
  let invalidated = false
  const observer = new MutationObserver(() => { if (!captureReady()) invalidated = true })
  observer.observe(document.body, { subtree:true, childList:true, attributes:true })
  const invalidate = () => { invalidated = true }
  window.addEventListener('feedback-session-ended', invalidate)
  try {
    for (const pixelRatio of [1, 0.5, 0.25]) {
    const blob = await toBlob(root, {
      width, height, pixelRatio, skipFonts:true,
      style:{marginTop:`${-scrollY}px`,marginLeft:`${-scrollX}px`},
      filter:includeInCapture,
    })
    if (invalidated || !captureReady() || location.href !== path || innerWidth !== width || innerHeight !== height) return null
    if (blob?.type === 'image/png' && blob.size > 0 && blob.size <= MAX_MEDIA) return blob
    if (!blob) return null
    }
    return null
  } catch { return null }
  finally {
    observer.disconnect()
    window.removeEventListener('feedback-session-ended',invalidate)
    delete root.dataset.capture
  }
}

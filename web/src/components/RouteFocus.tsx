import { useEffect, useRef } from 'react'
import { useLocation } from 'react-router-dom'

/** Move focus to the page's h1 (the first h1 in <main>, else the first on the page). */
export function focusPageHeading(root: ParentNode = document): boolean {
  const heading = root.querySelector<HTMLElement>('main h1') ?? root.querySelector<HTMLElement>('h1')
  if (!heading) return false
  if (!heading.hasAttribute('tabindex')) heading.setAttribute('tabindex', '-1')
  heading.focus({ preventScroll: true })
  return true
}

/**
 * Focus the page h1 after each route change (not on first load, which would steal focus
 * from the browser's own initial focus). Mount once inside the Router, after the routed content.
 */
export function useFocusHeadingOnRouteChange(): void {
  const { pathname } = useLocation()
  const previous = useRef(pathname)
  useEffect(() => {
    if (previous.current === pathname) return
    previous.current = pathname
    const frame = requestAnimationFrame(() => focusPageHeading())
    return () => cancelAnimationFrame(frame)
  }, [pathname])
}

export function RouteFocus() {
  useFocusHeadingOnRouteChange()
  return null
}

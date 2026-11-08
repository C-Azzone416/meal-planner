import { expect, test } from 'vitest'
import appSource from './App.tsx?raw'
import vercelConfig from '../../vercel.json?raw'

/**
 * The dev server and the tests serve every client route; Vercel serves only what vercel.json rewrites to
 * /index.html, so a client route without a rewrite 404s on direct load or reload. This keeps the two in step.
 */
interface Rewrite {
  source: string
  destination: string
}

const rewrites: Rewrite[] = JSON.parse(vercelConfig).rewrites

/** Vercel path syntax subset: literal segments, `:name` (one segment), `:name*` (zero or more), `:name+` (one or more). */
function sourceToRegExp(source: string): RegExp {
  const pattern = source
    .split(/(\/:\w+[*+]?)/)
    .map((part) => {
      const token = /^\/:\w+([*+]?)$/.exec(part)
      if (!token) return part.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
      return token[1] === '*' ? '(?:/.*)?' : token[1] === '+' ? '/.+' : '/[^/]+'
    })
    .join('')
  return new RegExp(`^${pattern}/?$`)
}

/** The destination of the first rewrite that matches, as Vercel evaluates them in order. */
function destinationFor(pathname: string): string | undefined {
  return rewrites.find((rewrite) => sourceToRegExp(rewrite.source).test(pathname))?.destination
}

const clientRoutes = [...appSource.matchAll(/<Route\s+path="([^"]+)"/g)]
  .map((match) => match[1])
  .filter((path) => path !== '*' && path !== '/')

test('every client route declared in App.tsx has a rewrite to /index.html', () => {
  expect(clientRoutes.length).toBeGreaterThanOrEqual(13)
  for (const route of clientRoutes) {
    const url = route.replace(/:\w+/g, 'abc')
    expect(destinationFor(url), `${route} (as ${url})`).toBe('/index.html')
  }
})

test.each([
  '/sign-in',
  '/week',
  '/recipes',
  '/recipes/',
  '/recipes/new',
  '/recipes/abc',
  '/recipes/abc/edit',
  '/recipes/by-id/new',
  '/recipes/by-id/new/edit',
  '/prep',
  '/shopping',
  '/drafts',
  '/cook',
  '/house-rules',
])('%s is served by the SPA', (url) => {
  expect(destinationFor(url)).toBe('/index.html')
})

test.each(['/api/recipes', '/api/recipes/abc', '/api/health', '/api/auth-proxy', '/api/anything/else'])('%s is never rewritten to the SPA', (url) => {
  expect(destinationFor(url)).not.toBe('/index.html')
})

test('the auth proxy rewrite stays first and API paths cannot be captured by an SPA rule', () => {
  expect(rewrites[0].source).toBe('/api/auth-proxy/:path*')
  for (const rewrite of rewrites.filter((item) => item.destination === '/index.html')) {
    expect(rewrite.source.startsWith('/api'), rewrite.source).toBe(false)
    expect(sourceToRegExp(rewrite.source).test('/api/recipes'), rewrite.source).toBe(false)
  }
})

// Build-time allowlist. No secret values are read or copied into the public object.
export function publicConfig(env, mode = 'production') {
  const target = env.FRAMEBRIAR_ENVIRONMENT
  const environment = target === 'preview' || target === 'production' ? target : null
  const appId = environment === 'preview' ? 'meal-planner-preview' : environment === 'production' ? 'meal-planner' : ''
  const buildSha = /^[0-9a-f]{40}$/.test(env.FRAMEBRIAR_BUILD_SHA ?? '') ? env.FRAMEBRIAR_BUILD_SHA : ''
  let origin = ''
  try {
    const url = new URL(env.FRAMEBRIAR_ALLOWED_ORIGIN)
    const loopback = url.hostname === 'localhost' || url.hostname.endsWith('.localhost') || /^127\./.test(url.hostname) || url.hostname === '[::1]'
    const local = mode !== 'production' && loopback
    if ((!loopback || local) && !url.hostname.includes('*') && url.origin === env.FRAMEBRIAR_ALLOWED_ORIGIN && !url.username && !url.password && (url.protocol === 'https:' || (local && url.protocol === 'http:'))) origin = url.origin
  } catch { /* Incomplete configuration disables feedback. */ }
  const targetMatches = !env.VERCEL_ENV || env.VERCEL_ENV === environment
  const enabled = env.FRAMEBRIAR_ENABLED === '1' && !!environment && !!origin && !!buildSha && env.FRAMEBRIAR_APP_ID === appId && targetMatches && ['0', '1'].includes(env.FRAMEBRIAR_SCREENSHOTS ?? '0')
  return { enabled, environment, origin, appId, buildSha, screenshots: enabled && env.FRAMEBRIAR_SCREENSHOTS === '1' }
}

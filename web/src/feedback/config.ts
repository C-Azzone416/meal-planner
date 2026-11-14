export interface FeedbackConfig {
  enabled: boolean
  environment: 'preview' | 'production' | null
  origin: string
  appId: string
  buildSha: string
  screenshots: boolean
}
declare const __FRAMEBRIAR_CONFIG__: FeedbackConfig
const compiled: FeedbackConfig = typeof __FRAMEBRIAR_CONFIG__ === 'undefined'
  ? { enabled: false, environment: null, origin: '', appId: '', buildSha: '', screenshots: false }
  : __FRAMEBRIAR_CONFIG__
// Unique Vercel URLs must not activate the stable-origin integration.
const originMatches = typeof window !== 'undefined' && window.location.origin === compiled.origin
export const feedbackConfig: FeedbackConfig = Object.freeze({ ...compiled, enabled: compiled.enabled && originMatches, screenshots: compiled.screenshots && originMatches })

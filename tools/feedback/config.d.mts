export interface PublicConfig {
  enabled: boolean
  environment: 'preview' | 'production' | null
  origin: string
  appId: string
  buildSha: string
  screenshots: boolean
}
export function publicConfig(env: Record<string, string | undefined>, mode?: string): PublicConfig

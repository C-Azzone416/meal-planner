import { BetterAuthVanillaAdapter } from '@neon/neon-js'

export interface AuthUser {
  id: string
  email: string
  emailVerified: boolean
}

export interface AuthService {
  getSession(): Promise<AuthUser | null>
  sendCode(email: string): Promise<void>
  verifyCode(email: string, code: string): Promise<AuthUser>
  signOut(): Promise<void>
}

export class AuthCodeVerificationError extends Error {
  constructor() {
    super('That code could not be verified.')
    this.name = 'AuthCodeVerificationError'
  }
}

export class AuthSessionEstablishmentError extends Error {
  constructor() {
    super('The secure sign-in session could not be established.')
    this.name = 'AuthSessionEstablishmentError'
  }
}

interface AuthConfigResponse {
  enabled: boolean
  baseUrl?: string
}

type NeonSession = {
  user?: {
    id?: string
    email?: string
    emailVerified?: boolean
    email_verified?: boolean
  }
}
function toAuthUser(session: NeonSession | null | undefined): AuthUser | null {
  const user = session?.user
  if (!user?.id || !user.email) return null

  return {
    id: user.id,
    email: user.email,
    emailVerified: Boolean(user.emailVerified ?? user.email_verified),
  }
}

function authError(error: unknown, fallback: string): Error {
  if (error && typeof error === 'object' && 'message' in error) {
    const message = String(error.message)
    if (message.trim()) return new Error(message)
  }
  return new Error(fallback)
}

function nestedString(error: unknown, key: string): string | undefined {
  if (!error || typeof error !== 'object') return undefined
  const value = (error as Record<string, unknown>)[key]
  return typeof value === 'string' ? value : undefined
}

export function isInvalidOrExpiredCodeError(error: unknown): boolean {
  const directCode = nestedString(error, 'code')
  const body = error && typeof error === 'object'
    ? (error as Record<string, unknown>).body
    : undefined
  const bodyCode = nestedString(body, 'code')
  return [directCode, bodyCode].some((code) => code === 'INVALID_OTP' || code === 'OTP_EXPIRED')
}

export async function loadAuthService(
  fetcher: typeof fetch = fetch,
  pageOrigin: string = globalThis.location?.origin ?? '',
): Promise<AuthService | null> {
  const response = await fetcher('/api/auth-config', {
    headers: { Accept: 'application/json' },
  })
  if (!response.ok) throw new Error('Sign-in configuration is unavailable.')

  const config = (await response.json()) as AuthConfigResponse
  if (!config.enabled) return null
  if (!config.baseUrl) throw new Error('Sign-in configuration is incomplete.')

  let origin: URL
  let url: URL
  try {
    origin = new URL(pageOrigin)
    url = new URL(config.baseUrl, origin)
  } catch {
    throw new Error('Sign-in configuration is invalid.')
  }
  if (
    origin.protocol !== 'https:'
    || url.protocol !== 'https:'
    || url.origin !== origin.origin
    || url.pathname.replace(/\/$/, '') !== '/api/auth-proxy'
    || url.search
    || url.hash
  ) {
    throw new Error('Sign-in configuration is invalid.')
  }

  const client = BetterAuthVanillaAdapter({
    fetchOptions: { credentials: 'include' },
  })(url.toString()).getBetterAuthInstance()

  const getCurrentUser = async () => {
    const { data, error } = await client.getSession()
    if (error) throw authError(error, 'Could not check your sign-in.')
    return toAuthUser(data as NeonSession | null)
  }

  const authorizeUser = async (user: AuthUser | null) => {
    if (!user?.emailVerified) return null

    const { data, error } = await client.token()
    if (error || !data?.token) return null
    const authorization = await fetcher('/api/auth/authorize', {
      method: 'GET',
      credentials: 'same-origin',
      headers: {
        Accept: 'application/json',
        Authorization: `Bearer ${data.token}`,
      },
    })
    if (authorization.status === 401 || authorization.status === 403) return null
    if (!authorization.ok) throw new Error('Sign-in authorization is unavailable.')
    return user
  }

  return {
    async getSession() {
      const user = await authorizeUser(await getCurrentUser())
      if (!user) await client.signOut()
      return user
    },

    async sendCode(email) {
      const { error } = await client.emailOtp.sendVerificationOtp({
        email,
        type: 'sign-in',
      })
      if (error) throw authError(error, 'Could not send a sign-in code.')
    },

    async verifyCode(email, code) {
      const { error } = await client.signIn.emailOtp({ email, otp: code })
      if (error) {
        if (isInvalidOrExpiredCodeError(error)) throw new AuthCodeVerificationError()
        throw new AuthSessionEstablishmentError()
      }

      const user = await authorizeUser(await getCurrentUser())
      if (!user) {
        await client.signOut()
        throw new AuthSessionEstablishmentError()
      }
      return user
    },

    async signOut() {
      const { error } = await client.signOut()
      if (error) throw authError(error, 'Could not sign out.')
    },
  }
}

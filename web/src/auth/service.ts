import { BetterAuthVanillaAdapter } from '@neon/neon-js'

export interface AuthUser {
  id: string
  email: string
  emailVerified: boolean
}

export interface AuthService {
  getSession(): Promise<AuthUser | null>
  signInWithPassword(email: string, password: string): Promise<AuthUser>
  sendPasswordResetCode(email: string): Promise<void>
  setPassword(email: string, code: string, password: string): Promise<AuthUser>
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

export class AuthCredentialsError extends Error {
  constructor() {
    super('The email or password was not accepted.')
    this.name = 'AuthCredentialsError'
  }
}

export class AuthPasswordPolicyError extends Error {
  constructor() {
    super('Use a password between 8 and 128 characters.')
    this.name = 'AuthPasswordPolicyError'
  }
}

interface AuthConfigResponse {
  enabled: boolean
  baseUrl?: string
}

type NeonSession = {
  session?: {
    token?: string
  }
  user?: {
    id?: string
    email?: string
    emailVerified?: boolean
    email_verified?: boolean
  }
}
type NeonAuthorizedSession = {
  token: string
  user: AuthUser
}

function toAuthorizedSession(session: NeonSession | null | undefined): NeonAuthorizedSession | null {
  const user = session?.user
  const token = session?.session?.token
  if (!user?.id || !user.email || !token) return null

  return {
    token,
    user: {
      id: user.id,
      email: user.email,
      emailVerified: Boolean(user.emailVerified ?? user.email_verified),
    },
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
  const details = error && typeof error === 'object'
    ? (error as Record<string, unknown>).error
    : undefined
  const detailsCode = nestedString(details, 'code')
  return [directCode, bodyCode, detailsCode].some((code) => code === 'INVALID_OTP' || code === 'OTP_EXPIRED')
}

function isInvalidCredentialsError(error: unknown): boolean {
  const directCode = nestedString(error, 'code')
  const body = error && typeof error === 'object'
    ? (error as Record<string, unknown>).body
    : undefined
  const bodyCode = nestedString(body, 'code')
  const details = error && typeof error === 'object'
    ? (error as Record<string, unknown>).error
    : undefined
  const detailsCode = nestedString(details, 'code')
  return [directCode, bodyCode, detailsCode].some((code) => (
    code === 'INVALID_EMAIL_OR_PASSWORD'
    || code === 'INVALID_PASSWORD'
    || code === 'USER_NOT_FOUND'
  ))
}

function isPasswordPolicyError(error: unknown): boolean {
  const directCode = nestedString(error, 'code')
  const body = error && typeof error === 'object'
    ? (error as Record<string, unknown>).body
    : undefined
  const bodyCode = nestedString(body, 'code')
  const details = error && typeof error === 'object'
    ? (error as Record<string, unknown>).error
    : undefined
  const detailsCode = nestedString(details, 'code')
  return [directCode, bodyCode, detailsCode].some((code) => (
    code === 'PASSWORD_TOO_SHORT' || code === 'PASSWORD_TOO_LONG'
  ))
}

async function authOperationError(
  operation: () => Promise<{ error?: unknown }>,
): Promise<unknown> {
  try {
    const result = await operation()
    return result.error
  } catch (error) {
    return error
  }
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

  const getCurrentSession = async () => {
    const { data, error } = await client.getSession({
      fetchOptions: { headers: { 'X-Force-Fetch': 'true' } },
    })
    if (error) throw authError(error, 'Could not check your sign-in.')
    return toAuthorizedSession(data as NeonSession | null)
  }

  const authorizeUser = async (session: NeonAuthorizedSession | null) => {
    if (!session?.user.emailVerified) return null

    const authorization = await fetcher('/api/auth/authorize', {
      method: 'GET',
      credentials: 'same-origin',
      headers: {
        Accept: 'application/json',
        Authorization: `Bearer ${session.token}`,
      },
    })
    if (authorization.status === 401 || authorization.status === 403) return null
    if (!authorization.ok) throw new Error('Sign-in authorization is unavailable.')
    return session.user
  }

  const currentAuthorizedUser = async () => authorizeUser(await getCurrentSession())

  const passwordSignIn = async (email: string, password: string, revokeOtherSessions = false) => {
    const signInError = await authOperationError(() => client.signIn.email({ email, password }))
    if (signInError) {
      if (isInvalidCredentialsError(signInError)) throw new AuthCredentialsError()
      throw new AuthSessionEstablishmentError()
    }

    const user = await currentAuthorizedUser()
    if (!user) {
      await authOperationError(() => client.signOut())
      throw new AuthSessionEstablishmentError()
    }
    if (revokeOtherSessions) {
      const revokeError = await authOperationError(() => client.revokeOtherSessions())
      if (revokeError) {
        await authOperationError(() => client.signOut())
        throw new AuthSessionEstablishmentError()
      }
    }
    return user
  }

  return {
    async getSession() {
      const user = await authorizeUser(await getCurrentSession())
      if (!user) await client.signOut()
      return user
    },

    async signInWithPassword(email, password) {
      return passwordSignIn(email, password)
    },

    async sendPasswordResetCode(email) {
      const error = await authOperationError(() => client.emailOtp.requestPasswordReset({ email }))
      if (error) throw authError(error, 'Could not send a password reset code.')
    },

    async setPassword(email, code, password) {
      const error = await authOperationError(() => client.emailOtp.resetPassword({
        email,
        otp: code,
        password,
      }))
      if (error) {
        if (isInvalidOrExpiredCodeError(error)) throw new AuthCodeVerificationError()
        if (isPasswordPolicyError(error)) throw new AuthPasswordPolicyError()
        throw new AuthSessionEstablishmentError()
      }

      return passwordSignIn(email, password, true)
    },

    async signOut() {
      const error = await authOperationError(() => client.signOut())
      if (error) throw authError(error, 'Could not sign out.')
    },
  }
}

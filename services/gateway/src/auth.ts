import { createRemoteJWKSet, decodeJwt, decodeProtectedHeader, jwtVerify, type JWTPayload } from 'jose'
import type { GatewayConfig } from './config.js'

export interface AuthenticatedIdentity {
  sub: string
  expiresAtMs: number
  claims: JWTPayload
}

export class AuthenticationError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'AuthenticationError'
  }
}

export function validateJwtClaims(payload: JWTPayload, config: Pick<GatewayConfig, 'jwtIssuer' | 'jwtAudience'>, nowMs = Date.now()): AuthenticatedIdentity {
  if (!config.jwtIssuer || payload.iss !== config.jwtIssuer) throw new AuthenticationError('invalid token issuer')
  const audiences = Array.isArray(payload.aud) ? payload.aud : [payload.aud]
  if (!audiences.includes(config.jwtAudience)) throw new AuthenticationError('invalid token audience')
  if (typeof payload.exp !== 'number' || !Number.isFinite(payload.exp)) throw new AuthenticationError('token expiration is required')
  const expiresAtMs = payload.exp * 1_000
  if (expiresAtMs <= nowMs) throw new AuthenticationError('token has expired')
  if (typeof payload.sub !== 'string' || payload.sub.length === 0) throw new AuthenticationError('token subject is required')
  return { sub: payload.sub, expiresAtMs, claims: payload }
}

export class SupabaseAuthenticator {
  private readonly jwks: ReturnType<typeof createRemoteJWKSet>

  constructor(private readonly config: GatewayConfig, private readonly fetchImpl: typeof fetch = fetch) {
    if (!config.supabaseAuthUrl) throw new AuthenticationError('Supabase authentication is not configured')
    this.jwks = createRemoteJWKSet(new URL(`${config.supabaseAuthUrl}/.well-known/jwks.json`))
  }

  async verify(accessToken: string): Promise<AuthenticatedIdentity> {
    let header: ReturnType<typeof decodeProtectedHeader>
    let unverifiedPayload: JWTPayload
    try {
      header = decodeProtectedHeader(accessToken)
      unverifiedPayload = decodeJwt(accessToken)
    } catch {
      throw new AuthenticationError('malformed access token')
    }

    if (header.alg === 'HS256') {
      if (!this.config.supabasePublishableKey) {
        throw new AuthenticationError('legacy HS256 token verification requires SUPABASE_PUBLISHABLE_KEY')
      }
      const identity = validateJwtClaims(unverifiedPayload, this.config)
      return this.verifyLegacyToken(accessToken, identity)
    }

    try {
      const verified = await jwtVerify(accessToken, this.jwks, {
        issuer: this.config.jwtIssuer,
        audience: this.config.jwtAudience
      })
      return validateJwtClaims(verified.payload, this.config)
    } catch {
      throw new AuthenticationError('token signature verification failed')
    }
  }

  private async verifyLegacyToken(accessToken: string, identity: AuthenticatedIdentity): Promise<AuthenticatedIdentity> {
    const response = await this.fetchImpl(`${this.config.supabaseAuthUrl}/user`, {
      headers: {
        authorization: `Bearer ${accessToken}`,
        apikey: this.config.supabasePublishableKey!
      }
    })
    if (!response.ok) throw new AuthenticationError('legacy token validation failed')
    let user: unknown
    try {
      user = await response.json()
    } catch {
      throw new AuthenticationError('legacy token validation returned invalid JSON')
    }
    if (!user || typeof user !== 'object' || (user as { id?: unknown }).id !== identity.sub) {
      throw new AuthenticationError('legacy token subject mismatch')
    }
    return identity
  }
}

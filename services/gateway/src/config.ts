export type Environment = 'development' | 'test' | 'production'

export interface GatewayConfig {
  environment: Environment
  host: string
  port: number
  mudHost: string
  mudPort: number
  allowedOrigins: ReadonlySet<string>
  requireSecureTransport: boolean
  authDisabled: boolean
  supabaseUrl?: string
  supabaseAuthUrl?: string
  supabasePublishableKey?: string
  jwtIssuer?: string
  jwtAudience: string
  authTimeoutMs: number
  tcpConnectTimeoutMs: number
  maxConnections: number
  maxFrameBytes: number
  inputBytesPerSecond: number
  maxBufferedBytes: number
  shutdownGraceMs: number
}

export class ConfigError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'ConfigError'
  }
}

const DEFAULT_ALLOWED_ORIGINS = ['http://localhost:3000', 'http://127.0.0.1:3000']

function requiredInteger(value: string | undefined, name: string, fallback: number, min: number): number {
  if (value === undefined || value === '') return fallback
  const parsed = Number(value)
  if (!Number.isInteger(parsed) || parsed < min) {
    throw new ConfigError(`${name} must be an integer greater than or equal to ${min}`)
  }
  return parsed
}

function requiredPort(value: string | undefined, name: string, fallback: number): number {
  const port = requiredInteger(value, name, fallback, 1)
  if (port > 65_535) throw new ConfigError(`${name} must be at most 65535`)
  return port
}

function parseBoolean(value: string | undefined, name: string): boolean {
  if (value === undefined || value === '') return false
  if (value === 'true') return true
  if (value === 'false') return false
  throw new ConfigError(`${name} must be true or false`)
}

function parseOrigins(value: string | undefined, environment: Environment): ReadonlySet<string> {
  const origins = (value === undefined || value.trim() === '')
    ? (environment === 'production' ? [] : DEFAULT_ALLOWED_ORIGINS)
    : value.split(',').map((origin) => origin.trim()).filter(Boolean)

  if (origins.length === 0) throw new ConfigError('ALLOWED_ORIGINS must contain at least one exact origin')
  for (const origin of origins) {
    let parsed: URL
    try {
      parsed = new URL(origin)
    } catch {
      throw new ConfigError(`ALLOWED_ORIGINS contains an invalid origin: ${origin}`)
    }
    if (parsed.origin !== origin || parsed.pathname !== '/' || parsed.search || parsed.hash) {
      throw new ConfigError(`ALLOWED_ORIGINS entries must be exact origins without paths: ${origin}`)
    }
  }
  return new Set(origins)
}

function normalizeSupabaseUrl(value: string | undefined): string | undefined {
  if (!value) return undefined
  let url: URL
  try {
    url = new URL(value)
  } catch {
    throw new ConfigError('SUPABASE_URL must be an absolute URL')
  }
  if (url.protocol !== 'https:' && url.protocol !== 'http:') {
    throw new ConfigError('SUPABASE_URL must use http or https')
  }
  return url.origin
}

function normalizeSupabaseAuthUrl(value: string | undefined): string | undefined {
  if (!value) return undefined
  let url: URL
  try {
    url = new URL(value)
  } catch {
    throw new ConfigError('SUPABASE_AUTH_URL must be an absolute URL')
  }
  if (url.protocol !== 'https:' && url.protocol !== 'http:') {
    throw new ConfigError('SUPABASE_AUTH_URL must use http or https')
  }
  if (url.username || url.password || url.search || url.hash) {
    throw new ConfigError('SUPABASE_AUTH_URL must not contain credentials, a query, or a fragment')
  }
  return url.toString().replace(/\/$/, '')
}

export function loadConfig(env: NodeJS.ProcessEnv = process.env): GatewayConfig {
  const environment = (env.NODE_ENV ?? 'development') as Environment
  if (!['development', 'test', 'production'].includes(environment)) {
    throw new ConfigError('NODE_ENV must be development, test, or production')
  }

  const authDisabled = parseBoolean(env.AUTH_DISABLED, 'AUTH_DISABLED')
  if (authDisabled && environment !== 'test') {
    throw new ConfigError('AUTH_DISABLED is permitted only when NODE_ENV=test')
  }
  if (environment === 'production' && authDisabled) {
    throw new ConfigError('production must not run with authentication disabled')
  }

  const supabaseUrl = normalizeSupabaseUrl(env.SUPABASE_URL)
  if (!authDisabled && !supabaseUrl) {
    throw new ConfigError('SUPABASE_URL is required when authentication is enabled')
  }

  const supabaseAuthUrl = normalizeSupabaseAuthUrl(env.SUPABASE_AUTH_URL)
    ?? (supabaseUrl ? `${supabaseUrl}/auth/v1` : undefined)
  const jwtIssuer = env.SUPABASE_JWT_ISSUER ?? (supabaseUrl ? `${supabaseUrl}/auth/v1` : undefined)
  return {
    environment,
    host: env.HOST ?? '0.0.0.0',
    // Port 0 is useful only for isolated integration tests; production must bind an explicit port.
    port: environment === 'test'
      ? requiredInteger(env.PORT, 'PORT', 8080, 0)
      : requiredPort(env.PORT, 'PORT', 8080),
    mudHost: env.MUD_HOST ?? '127.0.0.1',
    mudPort: requiredPort(env.MUD_PORT, 'MUD_PORT', 4000),
    allowedOrigins: parseOrigins(env.ALLOWED_ORIGINS, environment),
    requireSecureTransport: environment === 'production',
    authDisabled,
    supabaseUrl,
    supabaseAuthUrl,
    supabasePublishableKey: env.SUPABASE_PUBLISHABLE_KEY,
    jwtIssuer,
    jwtAudience: env.SUPABASE_JWT_AUDIENCE ?? 'authenticated',
    authTimeoutMs: requiredInteger(env.AUTH_TIMEOUT_MS, 'AUTH_TIMEOUT_MS', 10_000, 100),
    tcpConnectTimeoutMs: requiredInteger(env.TCP_CONNECT_TIMEOUT_MS, 'TCP_CONNECT_TIMEOUT_MS', 5_000, 100),
    maxConnections: requiredInteger(env.MAX_CONNECTIONS, 'MAX_CONNECTIONS', 200, 1),
    maxFrameBytes: requiredInteger(env.MAX_FRAME_BYTES, 'MAX_FRAME_BYTES', 16_384, 128),
    inputBytesPerSecond: requiredInteger(env.INPUT_BYTES_PER_SECOND, 'INPUT_BYTES_PER_SECOND', 4_096, 1),
    maxBufferedBytes: requiredInteger(env.MAX_BUFFERED_BYTES, 'MAX_BUFFERED_BYTES', 1_048_576, 1_024),
    shutdownGraceMs: requiredInteger(env.SHUTDOWN_GRACE_MS, 'SHUTDOWN_GRACE_MS', 10_000, 100)
  }
}

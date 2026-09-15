import { isIP } from 'node:net'

/**
 * Bounds browser onboarding upgrade attempts by the address of the TCP peer
 * connected directly to this Gateway. Reverse-proxy forwarding headers are
 * intentionally not an authority here: accepting them without an explicit
 * trusted-proxy configuration would let a caller choose its own limit key.
 */
export function authoritativeOnboardingSourceAddress(remoteAddress: string | undefined): string | undefined {
  if (!remoteAddress) return undefined
  const address = remoteAddress.trim()
  if (!address || isIP(address) === 0) return undefined

  // Node may report an IPv4 client through an IPv6 listener in this form.
  // Fold it into the ordinary IPv4 key so the same peer cannot use both forms.
  const mappedIpv4 = /^::ffff:(\d{1,3}(?:\.\d{1,3}){3})$/i.exec(address)
  return mappedIpv4 ? mappedIpv4[1] : address.toLowerCase()
}

export interface OnboardingSourceAttemptLimiterOptions {
  /** Zero is an explicit feature-off setting and keeps no source state. */
  maxAttempts: number
  windowMs: number
  maxKeys: number
  now?: () => number
}

type AttemptWindow = { startedAtMs: number, attempts: number }

/**
 * A fixed-window, in-memory limiter with a hard upper bound on stored source
 * keys. When the key bound is full, new sources are denied instead of evicting
 * a live source and making that source's limit bypassable.
 */
export class OnboardingSourceAttemptLimiter {
  private readonly windows = new Map<string, AttemptWindow>()
  private readonly now: () => number

  constructor(private readonly options: OnboardingSourceAttemptLimiterOptions) {
    if (!Number.isInteger(options.maxAttempts) || options.maxAttempts < 0) throw new RangeError('maxAttempts must be a non-negative integer')
    if (!Number.isInteger(options.windowMs) || options.windowMs <= 0) throw new RangeError('windowMs must be a positive integer')
    if (!Number.isInteger(options.maxKeys) || options.maxKeys <= 0) throw new RangeError('maxKeys must be a positive integer')
    this.now = options.now ?? Date.now
  }

  /** Returns true only when this source may begin another onboarding attempt. */
  allow(sourceAddress: string | undefined): boolean {
    if (this.options.maxAttempts === 0) return true
    if (!sourceAddress) return false

    const now = this.now()
    this.pruneExpired(now)
    const existing = this.windows.get(sourceAddress)
    if (existing) {
      if (existing.attempts >= this.options.maxAttempts) return false
      existing.attempts += 1
      return true
    }

    if (this.windows.size >= this.options.maxKeys) return false
    this.windows.set(sourceAddress, { startedAtMs: now, attempts: 1 })
    return true
  }

  /** Exposed for diagnostics and focused tests; reading also clears expired keys. */
  get size(): number {
    if (this.options.maxAttempts !== 0) this.pruneExpired(this.now())
    return this.windows.size
  }

  private pruneExpired(now: number): void {
    for (const [sourceAddress, window] of this.windows) {
      if (now - window.startedAtMs >= this.options.windowMs) this.windows.delete(sourceAddress)
    }
  }
}

import { shouldOpenGatewaySocket } from './gateway-contract.ts'

export type OnboardingMode = 'provision' | 'claim'
export type RosterStatus = 'loading' | 'error' | 'empty' | 'ready'

export interface OnboardingSocketContract {
  kind: 'onboarding'
  path: '/onboarding'
  subprotocol: 'muhan.onboarding.v1'
  mode: OnboardingMode
}

export interface OnboardingAuthFrame {
  type: 'onboarding-auth'
  accessToken: string
  mode: OnboardingMode
  correlationId: string
}

const strictLowerUuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/

export type OnboardingControlDecision =
  | { kind: 'ready' }
  | { kind: 'echo'; enabled: boolean }
  | { kind: 'provisioned'; characterId: string }
  | { kind: 'claimed'; characterId: string }
  | { kind: 'error'; detail: string }
  | { kind: 'terminated' }
  | { kind: 'failure' }
  | { kind: 'ignore' }

export type OnboardingLifecyclePhase =
  | 'connecting'
  | 'authenticating'
  | 'ready'
  | 'provisioned'
  | 'failed'

export type OnboardingCloseDecision = 'reconnect' | 'terminate'

export const MAX_ONBOARDING_RECONNECT_ATTEMPTS = 3

export function isStrictLowerUuid(value: unknown): value is string {
  return typeof value === 'string' && strictLowerUuid.test(value)
}

/** Interpret browser-visible onboarding controls while keeping protocol decisions pure. */
export function decideOnboardingControl(
  mode: OnboardingMode,
  value: unknown,
  phase: OnboardingLifecyclePhase = 'connecting',
): OnboardingControlDecision {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    return { kind: 'failure' }
  }
  const control = value as Record<string, unknown>
  switch (control.type) {
    case 'onboarding-ready':
    case 'ready':
      return { kind: 'ready' }
    case 'echo':
      return { kind: 'echo', enabled: control.enabled !== false }
    case 'provisioned':
      return mode === 'provision' && isStrictLowerUuid(control.characterId)
        ? { kind: 'provisioned', characterId: control.characterId }
        : { kind: 'failure' }
    case 'claimed':
      return mode === 'claim' && isStrictLowerUuid(control.characterId)
        ? { kind: 'claimed', characterId: control.characterId }
        : { kind: 'failure' }
    case 'error':
      return {
        kind: 'error',
        detail: [control.message, control.reason, control.code].find(
          (value): value is string => typeof value === 'string' && value.length > 0,
        ) ?? '게이트웨이에서 온보딩 오류를 알렸습니다.',
      }
    case 'closed':
      return phase === 'ready' || phase === 'provisioned'
        ? { kind: 'terminated' }
        : { kind: 'failure' }
    case 'pong':
      return { kind: 'ignore' }
    default:
      return { kind: 'failure' }
  }
}

export function createOnboardingAuthFrame(
  accessToken: string,
  mode: OnboardingMode,
  correlationId: string,
): OnboardingAuthFrame {
  if (!strictLowerUuid.test(correlationId)) {
    throw new Error('온보딩 연결 식별자가 올바르지 않습니다.')
  }
  return { type: 'onboarding-auth', accessToken, mode, correlationId }
}

/** Build the isolated endpoint from the configured Gateway URL without copying query secrets. */
export function createOnboardingSocketUrl(gatewayUrl: string): string {
  const url = new URL(gatewayUrl)
  if (url.protocol !== 'ws:' && url.protocol !== 'wss:') {
    throw new Error('MUD Gateway URL은 ws:// 또는 wss:// 주소여야 합니다.')
  }
  url.username = ''
  url.password = ''
  const path = url.pathname.replace(/\/+$/, '')
  url.pathname = path.endsWith('/ws')
    ? `${path.slice(0, -3) || ''}/onboarding`
    : `${path || ''}/onboarding`
  url.search = ''
  url.hash = ''
  return url.toString()
}

export function shouldReconnectOnboardingClose(
  code: number,
  attempt: number,
): boolean {
  if (attempt >= MAX_ONBOARDING_RECONNECT_ATTEMPTS) return false
  return !(
    code === 1000 ||
    code === 1002 ||
    code === 1003 ||
    code === 1007 ||
    code === 1008 ||
    code === 1009 ||
    code === 1010 ||
    code === 4001 ||
    (code >= 4400 && code < 4500)
  )
}

/** Decide whether a close is a bounded transport retry or the end of onboarding. */
export function decideOnboardingClose(
  phase: OnboardingLifecyclePhase,
  code: number,
  attempt: number,
  failed: boolean,
): OnboardingCloseDecision {
  if (failed || phase === 'failed') return 'terminate'
  // Once the Gateway has confirmed provisioning, retrying the same wizard is
  // unsafe. Return to the roster, which is the recovery path for a lost close.
  if (phase === 'provisioned') return 'terminate'
  if (code === 1000) return 'terminate'
  return shouldReconnectOnboardingClose(code, attempt) ? 'reconnect' : 'terminate'
}

/** Empty rosters may start only the isolated onboarding protocol. */
export function shouldOpenOnboardingSocket(status: RosterStatus, mode: OnboardingMode): boolean {
  return status === 'empty' && (mode === 'provision' || mode === 'claim')
}

export function createOnboardingSocketContract(
  status: RosterStatus,
  mode: OnboardingMode,
): OnboardingSocketContract | null {
  if (!shouldOpenOnboardingSocket(status, mode)) return null
  return { kind: 'onboarding', path: '/onboarding', subprotocol: 'muhan.onboarding.v1', mode }
}

export { shouldOpenGatewaySocket }

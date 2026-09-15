import { shouldOpenGatewaySocket } from './gateway-contract.ts'

export type OnboardingMode = 'provision' | 'claim'
export type RosterStatus = 'loading' | 'error' | 'empty' | 'ready'

/**
 * Browser-safe recovery outcomes. These intentionally describe only the
 * already browser-visible onboarding control/close boundary; they never carry
 * a Gateway reason, C output, ticket, identifier, or credential value.
 */
export type OnboardingFailureCategory =
  | 'session'
  | 'legacy-credentials'
  | 'state-changed'
  | 'unavailable'
  | 'unknown'

export interface OnboardingRecovery {
  category: OnboardingFailureCategory
  title: string
  detail: string
}

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
  | { kind: 'error'; recovery: OnboardingRecovery }
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

const RECOVERY_COPY: Record<OnboardingFailureCategory, Omit<OnboardingRecovery, 'category'>> = {
  session: {
    title: '웹 로그인 확인이 필요합니다.',
    detail: '웹 로그인 세션을 다시 확인한 뒤 캐릭터 목록을 새로고침하고 다시 시도해 주세요.',
  },
  'legacy-credentials': {
    title: '기존 캐릭터 확인을 완료하지 못했습니다.',
    detail: '온보딩 터미널에서 기존 캐릭터 이름과 게임 비밀번호를 다시 확인한 뒤 다시 시도해 주세요. 캐릭터 상태가 바뀌었다면 목록을 새로고침하세요.',
  },
  'state-changed': {
    title: '캐릭터 상태를 다시 확인해 주세요.',
    detail: '온보딩 중 캐릭터 상태가 바뀌었을 수 있습니다. 캐릭터 목록을 새로고침한 뒤 상태가 반영되면 다시 시도해 주세요.',
  },
  unavailable: {
    title: '온보딩 통로를 다시 연결할 수 없습니다.',
    detail: '잠시 후 캐릭터 목록을 새로고침하고 다시 시도해 주세요.',
  },
  unknown: {
    title: '온보딩을 완료하지 못했습니다.',
    detail: '세부 오류는 표시하지 않습니다. 캐릭터 목록을 새로고침한 뒤 다시 시도해 주세요.',
  },
}

function recovery(category: OnboardingFailureCategory): OnboardingRecovery {
  return { category, ...RECOVERY_COPY[category] }
}

/**
 * Convert only exact, non-secret Gateway outcomes into recovery guidance.
 * Unrecognised strings are deliberately ignored, so raw C/Gateway errors can
 * never be rendered by the browser.
 */
export function recoveryFromOnboardingControl(
  mode: OnboardingMode,
  value: unknown,
): OnboardingRecovery {
  const reason = typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>).reason
    : undefined
  if (reason === 'onboarding authentication failed' || reason === 'token expired') {
    return recovery('session')
  }
  if (reason === 'onboarding failed') {
    return recovery(mode === 'claim' ? 'legacy-credentials' : 'state-changed')
  }
  return recovery('unknown')
}

/** Map browser-visible close codes without retaining or showing close reasons. */
export function recoveryFromOnboardingClose(
  code: number,
): OnboardingRecovery {
  if (code === 4001) return recovery('session')
  if (code === 1001 || code === 1006 || code === 1011 || code === 1012 || code === 1013) {
    return recovery('unavailable')
  }
  return recovery('unknown')
}

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
        recovery: recoveryFromOnboardingControl(mode, control),
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

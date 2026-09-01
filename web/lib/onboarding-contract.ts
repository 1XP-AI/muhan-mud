import { shouldOpenGatewaySocket } from './gateway-contract.ts'

export type OnboardingMode = 'provision' | 'claim'
export type RosterStatus = 'loading' | 'error' | 'empty' | 'ready'

export interface OnboardingSocketContract {
  kind: 'onboarding'
  path: '/onboarding'
  subprotocol: 'muhan.onboarding.v1'
  mode: OnboardingMode
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

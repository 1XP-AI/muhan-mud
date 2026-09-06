import {
  OnboardingReconciler,
  PostgresOnboardingSnapshotEligibilityFulfillmentRpc,
  PostgresPendingOnboardingSnapshotEligibilitySource,
  fulfillPendingOnboardingSnapshotEligibilityOnce,
  runPolling,
  type Clock,
} from './reconciler.js'

function positiveInteger(value: string | undefined, fallback: number, maximum: number): number {
  if (value === undefined || value === '') return fallback
  if (!/^[0-9]+$/.test(value)) throw new Error('configuration rejected')
  const parsed = Number(value)
  if (!Number.isSafeInteger(parsed) || parsed < 1 || parsed > maximum) throw new Error('configuration rejected')
  return parsed
}

function nonNegativeInteger(value: string | undefined, fallback: number, maximum: number): number {
  if (value === undefined || value === '') return fallback
  if (!/^[0-9]+$/.test(value)) throw new Error('configuration rejected')
  const parsed = Number(value)
  if (!Number.isSafeInteger(parsed) || parsed > maximum) throw new Error('configuration rejected')
  return parsed
}

function environment(env: NodeJS.ProcessEnv, name: string): string {
  const value = env[name]
  if (!value) throw new Error('configuration rejected')
  return value
}

export async function main(env: NodeJS.ProcessEnv = process.env, args: readonly string[] = process.argv.slice(2), clock?: Clock): Promise<number> {
  const fulfillmentRun = args.includes('--fulfill-pending-snapshot-eligibility')
  if (args.some((argument) => argument !== '--once' && argument !== '--fulfill-pending-snapshot-eligibility') ||
      (fulfillmentRun && args.length !== 1)) throw new Error('configuration rejected')
  if (fulfillmentRun) {
    const source = new PostgresPendingOnboardingSnapshotEligibilitySource(environment(env, 'ONBOARDING_SNAPSHOT_ELIGIBILITY_DATABASE_URL'))
    const fulfillment = new PostgresOnboardingSnapshotEligibilityFulfillmentRpc(environment(env, 'MUD_WRITER_DATABASE_URL'))
    try {
      const result = await fulfillPendingOnboardingSnapshotEligibilityOnce(source, fulfillment, {
        limit: positiveInteger(env.ONBOARDING_SNAPSHOT_ELIGIBILITY_LIMIT, 100, 1_000),
        attempts: positiveInteger(env.ONBOARDING_RECONCILER_RPC_ATTEMPTS, 3, 10),
        retryDelayMs: nonNegativeInteger(env.ONBOARDING_RECONCILER_RETRY_DELAY_MS, 250, 60_000),
        clock,
      })
      process.stdout.write(`${JSON.stringify(result)}\n`)
      return result.rejected > 0 || result.retryExhausted > 0 ? 1 : 0
    } finally {
      await Promise.all([source.close(), fulfillment.close()])
    }
  }
  const runs = args.includes('--once') ? 1 : positiveInteger(env.ONBOARDING_RECONCILER_POLLS, 1, 10_000)
  const pollingClock = clock ?? { now: Date.now, sleep: async (ms: number) => new Promise<void>((done) => setTimeout(done, ms)) }
  const reconciler = new OnboardingReconciler({
    muhanHome: environment(env, 'MUHAN_HOME'),
    postgrestUrl: environment(env, 'SUPABASE_INTERNAL_REST_URL'),
    serviceRoleKey: environment(env, 'SUPABASE_SERVICE_ROLE_KEY'),
    clock: pollingClock,
    rpcAttempts: positiveInteger(env.ONBOARDING_RECONCILER_RPC_ATTEMPTS, 3, 10),
    retryDelayMs: nonNegativeInteger(env.ONBOARDING_RECONCILER_RETRY_DELAY_MS, 250, 60_000),
    rpcRequestTimeoutMs: positiveInteger(env.ONBOARDING_RECONCILER_RPC_TIMEOUT_MS, 10_000, 60_000),
  })
  const totals = await runPolling(reconciler, pollingClock, { runs, intervalMs: nonNegativeInteger(env.ONBOARDING_RECONCILER_POLL_INTERVAL_MS, 1_000, 60_000) })
  // Counts only: receipt contents, paths, hashes, and service-role credentials never reach stdout/stderr.
  process.stdout.write(`${JSON.stringify(totals)}\n`)
  return totals.rejected > 0 || totals.retry_exhausted > 0 ? 1 : 0
}

if (import.meta.url === new URL(process.argv[1]!, 'file:').href) {
  main().then((exitCode) => { process.exitCode = exitCode }).catch(() => {
    process.stderr.write('onboarding reconciler failed safely\n')
    process.exitCode = 1
  })
}

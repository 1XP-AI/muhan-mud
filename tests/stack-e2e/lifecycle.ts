export interface CleanupStep {
  name: string;
  run: () => Promise<void>;
}

/** Run every teardown step, retaining failures for the caller to report. */
export async function runCleanupSteps(steps: readonly CleanupStep[]): Promise<Error[]> {
  const failures: Error[] = [];
  for (const step of steps) {
    try {
      await step.run();
    } catch (error) {
      failures.push(new Error(`stack-e2e ${step.name} cleanup failed`, { cause: error }));
    }
  }
  return failures;
}

export function cleanupFailure(failures: readonly Error[]): Error | undefined {
  if (failures.length === 0) return undefined;
  if (failures.length === 1) return failures[0];
  return new AggregateError(failures, "stack-e2e cleanup failed");
}

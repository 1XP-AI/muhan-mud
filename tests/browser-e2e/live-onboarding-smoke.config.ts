import { defineConfig, devices } from "@playwright/test";

import { readLiveOnboardingSmokeConfig } from "../../web/lib/live-onboarding-smoke";

const liveSmoke = readLiveOnboardingSmokeConfig();

/**
 * This config deliberately has no webServer. If the guard below has not been
 * explicitly satisfied, the spec is skipped before Playwright creates a
 * browser, so default local/CI invocations make no network calls.
 */
export default defineConfig({
  testDir: ".",
  testMatch: "live-onboarding-smoke.spec.ts",
  fullyParallel: false,
  workers: 1,
  timeout: 60_000,
  expect: { timeout: 20_000 },
  forbidOnly: true,
  retries: 0,
  reporter: [["list"]],
  // Never retain a result directory that could contain failure diagnostics
  // from an operator-supplied fixture run.
  preserveOutput: "never",
  outputDir: "output/playwright/live-onboarding-smoke",
  use: {
    // A skipped suite never opens this URL. Keep an invalid placeholder so a
    // missing opt-in cannot accidentally fall back to localhost or a deploy.
    baseURL: liveSmoke.config?.baseUrl ?? "https://live-smoke-disabled.invalid",
    trace: "off",
    screenshot: "off",
    video: "off",
    serviceWorkers: "block",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});

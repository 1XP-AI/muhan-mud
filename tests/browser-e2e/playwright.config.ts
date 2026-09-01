import { defineConfig, devices } from "@playwright/test";

const port = 3123;

export default defineConfig({
  testDir: ".",
  fullyParallel: false,
  workers: 1,
  timeout: 20_000,
  expect: { timeout: 5_000 },
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: [["list"], ["json", { outputFile: "output/playwright/browser-results.json" }]],
  outputDir: "output/playwright/test-results",
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "off",
    serviceWorkers: "block",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
  webServer: {
    command: `pnpm --filter @muhan/web dev --hostname 127.0.0.1 --port ${port}`,
    cwd: process.cwd(),
    url: `http://127.0.0.1:${port}`,
    timeout: 120_000,
    reuseExistingServer: !process.env.CI,
    env: {
      NODE_ENV: "development",
      SUPABASE_PUBLIC_URL: `http://127.0.0.1:${port}`,
      SUPABASE_PUBLISHABLE_KEY: "public-test-key-placeholder",
      MUD_GATEWAY_URL: "ws://gateway.local:9911/ws",
      MUD_ONBOARDING_ENABLED: "true",
    },
  },
});

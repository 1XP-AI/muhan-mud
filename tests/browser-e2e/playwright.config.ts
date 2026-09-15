import { defineConfig, devices } from "@playwright/test";

const port = Number(process.env.MUHAN_BROWSER_PORT ?? 3123);
if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error("Invalid MUHAN_BROWSER_PORT");

export default defineConfig({
  testDir: ".",
  testMatch: "classic-terminal.spec.ts",
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
      MUD_GO_GATEWAY_URL: "ws://gateway.local:8081/ws",
    },
  },
});

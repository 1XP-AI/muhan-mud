import { defineConfig, devices } from "@playwright/test";

const browserPort = Number(process.env.MUHAN_BROWSER_PORT ?? 3125);
const goPort = Number(process.env.MUHAN_GO_PORT ?? 3181);
const databaseURL = process.env.MUHAN_BROWSER_DATABASE_URL;
if (!databaseURL) {
  throw new Error(
    "MUHAN_BROWSER_DATABASE_URL must point at an isolated disposable PostgreSQL database",
  );
}
for (const [name, port] of [["MUHAN_BROWSER_PORT", browserPort], ["MUHAN_GO_PORT", goPort]]) {
  if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error(`Invalid ${name}`);
}
if (browserPort === goPort) throw new Error("MUHAN_BROWSER_PORT and MUHAN_GO_PORT must differ");

export default defineConfig({
  testDir: ".",
  testMatch: "go-process-postgres.spec.ts",
  fullyParallel: false,
  workers: 1,
  timeout: 60_000,
  expect: { timeout: 10_000 },
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: [["list"], ["json", { outputFile: "output/playwright/go-process-postgres-results.json" }]],
  outputDir: "output/playwright/go-process-postgres-test-results",
  use: {
    baseURL: `http://127.0.0.1:${browserPort}`,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "off",
    serviceWorkers: "block",
  },
  projects: [{ name: "chromium-go-postgres", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: "bash tests/browser-e2e/run-go-postgres-browser-e2e.sh",
    cwd: process.cwd(),
    url: `http://127.0.0.1:${browserPort}`,
    timeout: 180_000,
    reuseExistingServer: false,
    env: {
      NODE_ENV: "development",
      MUHAN_BROWSER_PORT: String(browserPort),
      MUHAN_GO_PORT: String(goPort),
      MUHAN_BROWSER_DATABASE_URL: databaseURL,
      MUHAN_BROWSER_WORLD_ID: process.env.MUHAN_BROWSER_WORLD_ID ?? "browser-e2e",
      MUHAN_BROWSER_CHARACTER_NAME: process.env.MUHAN_BROWSER_CHARACTER_NAME ?? "BrowserAlice",
      MUHAN_BROWSER_GAME_PASSWORD: process.env.MUHAN_BROWSER_GAME_PASSWORD ?? "pw1234",
    },
  },
});

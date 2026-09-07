import assert from "node:assert/strict";
import { spawn, type ChildProcess } from "node:child_process";
import { chromium, expect, type Page } from "@playwright/test";

import {
  completeClaimThroughRenderedXterm,
  type ClaimXtermFlowDriver,
} from "./claim-xterm-flow.js";

export interface WebStackFixture {
  accessToken: string;
  email: string;
  gamePassword: string;
  userId: string;
}

export interface WebStackProvisionFixture extends WebStackFixture {
  alignment: string;
  characterClass: string;
  characterName: string;
  gender: string;
  race: string;
  stats: string;
  weapon: string;
}

export interface WebStackClaimFixture extends WebStackFixture {
  characterName: string;
}

export interface WebStackServer {
  baseUrl: string;
  child: ChildProcess;
  postgrestUrl?: string;
}

interface TimerApi {
  clearTimeout(handle: ReturnType<typeof setTimeout> | undefined): void;
  setTimeout(callback: () => void, timeoutMs: number): ReturnType<typeof setTimeout>;
}

interface WebStackReadinessOptions {
  fetch?: typeof globalThis.fetch;
  now?: () => number;
  pollIntervalMs?: number;
  requestTimeoutMs?: number;
  startupTimeoutMs?: number;
  timers?: TimerApi;
}

interface WebStackStopOptions {
  graceTimeoutMs?: number;
  killTimeoutMs?: number;
  signalProcessTree?: (child: ChildProcess, signal: NodeJS.Signals) => void;
  timers?: TimerApi;
}

type WebStackExit = [number | null, NodeJS.Signals | null];

function observedWebStackExit(child: ChildProcess): WebStackExit | undefined {
  // Node sets one of these before emitting `exit`. Checking both prevents a
  // missed event when the process group exits in the tiny gap after kill().
  if (child.exitCode != null || child.signalCode != null) {
    return [child.exitCode, child.signalCode];
  }
  return undefined;
}

function assertSuccessfulWebStackExit(
  [code, signal]: WebStackExit,
  expectedSignal?: NodeJS.Signals,
): void {
  assert.ok(
    code === 0 || (expectedSignal !== undefined && signal === expectedSignal),
    `web server exited unexpectedly (${code ?? signal})`,
  );
}

export async function waitForWebStackExit(
  child: ChildProcess,
  timeoutMs: number,
  timers: TimerApi = globalThis,
): Promise<[number | null, NodeJS.Signals | null]> {
  return new Promise((resolve, reject) => {
    let timeout: ReturnType<typeof setTimeout> | undefined;
    let settled = false;

    const finish = (complete: () => void) => {
      if (settled) return;
      settled = true;
      child.removeListener("exit", onExit);
      if (timeout !== undefined) timers.clearTimeout(timeout);
      complete();
    };
    const onExit = (code: number | null, signal: NodeJS.Signals | null) => {
      finish(() => resolve([code, signal]));
    };

    const alreadyExited = observedWebStackExit(child);
    if (alreadyExited) {
      finish(() => resolve(alreadyExited));
      return;
    }
    child.once("exit", onExit);
    // `exit` can win after the first observation but before the listener is
    // installed. ChildProcess keeps the terminal status for this second check.
    const exitedWhileInstalling = observedWebStackExit(child);
    if (exitedWhileInstalling) {
      finish(() => resolve(exitedWhileInstalling));
      return;
    }
    timeout = timers.setTimeout(
      () => finish(() => reject(new Error(`web server did not exit within ${timeoutMs}ms`))),
      timeoutMs,
    );
  });
}

export function signalWebStackProcessTree(
  child: ChildProcess,
  signal: NodeJS.Signals,
  platform = process.platform,
  kill: typeof process.kill = process.kill,
): void {
  assert.ok(child.pid, "web server process has no PID");
  // pnpm starts Next as a descendant. A detached POSIX child becomes its own
  // process-group leader, so signal the group rather than leaving Next behind.
  if (platform !== "win32") {
    kill(-child.pid, signal);
    return;
  }
  child.kill(signal);
}

function childOutput(child: ChildProcess): { read(): string } {
  let output = "";
  const capture = (data: Buffer) => {
    output = `${output}${data.toString("utf8")}`.slice(-8_000);
  };
  child.stdout?.on("data", capture);
  child.stderr?.on("data", capture);
  return { read: () => output };
}

export function startWebStackServer({
  gatewayUrl,
  port,
  root,
  supabaseUrl,
  supabasePublishableKey,
}: {
  gatewayUrl: string;
  port: number;
  root: string;
  supabaseUrl: string;
  supabasePublishableKey: string;
}): WebStackServer {
  const child = spawn(
    "pnpm",
    ["--dir", `${root}/web`, "exec", "next", "dev", "--hostname", "127.0.0.1", "--port", String(port)],
    {
      cwd: root,
      // Keep pnpm and its Next descendants in a dedicated POSIX process group
      // so the runner can tear down the entire tree deterministically.
      detached: process.platform !== "win32",
      env: {
        ...process.env,
        NODE_ENV: "development",
        SUPABASE_PUBLIC_URL: `http://127.0.0.1:${port}`,
        SUPABASE_PUBLISHABLE_KEY: supabasePublishableKey,
        MUD_GATEWAY_URL: gatewayUrl,
        MUD_ONBOARDING_ENABLED: "true",
      },
      stdio: ["ignore", "pipe", "pipe"],
    },
  );
  const output = childOutput(child);
  child.once("exit", (code, signal) => {
    if (code !== 0 && signal !== "SIGTERM") {
      process.stderr.write(`stack-e2e: web exited early (${code ?? signal}): ${output.read()}\n`);
    }
  });
  return { baseUrl: `http://127.0.0.1:${port}`, child, postgrestUrl: supabaseUrl };
}

async function fetchWebStackReadiness(
  url: string,
  timeoutMs: number,
  fetchRequest: typeof globalThis.fetch,
  timers: TimerApi,
): Promise<Response> {
  const controller = new AbortController();
  let timeout: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      fetchRequest(url, { signal: controller.signal }),
      new Promise<never>((_, reject) => {
        timeout = timers.setTimeout(() => {
          controller.abort();
          reject(new Error(`web server readiness request exceeded ${timeoutMs}ms`));
        }, timeoutMs);
      }),
    ]);
  } finally {
    if (timeout !== undefined) timers.clearTimeout(timeout);
  }
}

export async function waitForWebStackServer(
  server: WebStackServer,
  {
    fetch: fetchRequest = globalThis.fetch,
    now = Date.now,
    pollIntervalMs = 100,
    requestTimeoutMs = 2_000,
    startupTimeoutMs = 60_000,
    timers = globalThis,
  }: WebStackReadinessOptions = {},
): Promise<void> {
  const deadline = now() + startupTimeoutMs;
  let lastError: unknown;
  while (now() < deadline) {
    const exited = observedWebStackExit(server.child);
    if (exited) {
      throw new Error(`web server exited before listening (${exited[0] ?? exited[1]})`);
    }
    try {
      const response = await fetchWebStackReadiness(
        server.baseUrl,
        Math.min(requestTimeoutMs, Math.max(1, deadline - now())),
        fetchRequest,
        timers,
      );
      if (response.ok) return;
      lastError = new Error(`web server returned ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await new Promise<void>((resolve) => timers.setTimeout(() => resolve(), pollIntervalMs));
  }
  throw lastError instanceof Error ? lastError : new Error("web server did not become ready");
}

export async function stopWebStackServer(
  server: WebStackServer,
  {
    graceTimeoutMs = 15_000,
    killTimeoutMs = 5_000,
    signalProcessTree = signalWebStackProcessTree,
    timers = globalThis,
  }: WebStackStopOptions = {},
): Promise<void> {
  const alreadyExited = observedWebStackExit(server.child);
  if (alreadyExited) {
    assertSuccessfulWebStackExit(alreadyExited);
    return;
  }
  try {
    signalProcessTree(server.child, "SIGTERM");
    const [code, signal] = await waitForWebStackExit(server.child, graceTimeoutMs, timers);
    assertSuccessfulWebStackExit([code, signal], "SIGTERM");
  } catch (termError) {
    // A development server that ignores graceful termination still must not
    // outlive this disposable runner. SIGKILL targets the same pnpm/Next tree.
    // The group can exit between the initial exitCode observation and kill().
    // POSIX reports that as ESRCH, but a normal child exit is successful cleanup.
    const exitedAfterTerm = observedWebStackExit(server.child);
    if (exitedAfterTerm) {
      assertSuccessfulWebStackExit(exitedAfterTerm, "SIGTERM");
      return;
    }
    try {
      signalProcessTree(server.child, "SIGKILL");
      const exit = await waitForWebStackExit(server.child, killTimeoutMs, timers);
      // A fallback that observes code 1 (rather than a SIGKILL exit) must
      // remain a failure; otherwise a broken Next process is falsely green.
      assertSuccessfulWebStackExit(exit, "SIGKILL");
    } catch (killError) {
      const exitedAfterKill = observedWebStackExit(server.child);
      if (exitedAfterKill) {
        assertSuccessfulWebStackExit(exitedAfterKill, "SIGKILL");
        return;
      }
      throw new AggregateError(
        [termError, killError],
        "web server process tree could not be terminated",
      );
    }
  }
}

async function installAuthBoundary(page: Page, fixture: WebStackFixture, server: WebStackServer): Promise<void> {
  assert.ok(server.postgrestUrl, "real PostgREST endpoint is required");
  // Mirror the production same-origin /rest/v1 ingress prefix, but forward
  // every request and its bearer token to the real disposable database API.
  // No roster response or database result is mocked here.
  await page.route(`${server.baseUrl}/rest/v1/**`, async (route) => {
    const requested = new URL(route.request().url());
    const upstream = new URL(server.postgrestUrl!);
    upstream.pathname = requested.pathname.slice("/rest/v1".length);
    upstream.search = requested.search;
    const response = await route.fetch({ url: upstream.toString() });
    await route.fulfill({ response });
  });
  const session = {
    access_token: fixture.accessToken,
    token_type: "bearer",
    expires_in: 3600,
    expires_at: Math.floor(Date.now() / 1000) + 3600,
    refresh_token: `refresh-${fixture.userId}`,
    user: {
      id: fixture.userId,
      aud: "authenticated",
      role: "authenticated",
      email: fixture.email,
      app_metadata: { provider: "email", providers: ["email"] },
      user_metadata: {},
    },
  };

  await page.route("**/auth/v1/token**", async (route) => {
    process.stderr.write(`stack-e2e: auth-fixture method=${route.request().method()}\n`);
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(session) });
  });
  await page.route("**/auth/v1/user**", async (route) => {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(session.user) });
  });
}

async function signInToEmptyRoster(page: Page, fixture: WebStackFixture): Promise<void> {
  const statuses: number[] = [];
  const failures: string[] = [];
  const failed = (request: import("@playwright/test").Request) => {
    const code = request.failure()?.errorText.match(/net::[A-Z_]+/)?.[0] ?? "unclassified";
    failures.push(code);
  };
  page.on("requestfailed", failed);
  const observe = (response: import("@playwright/test").Response) => {
    if (new URL(response.url()).pathname === "/rest/v1/game_characters") statuses.push(response.status());
  };
  page.on("response", observe);
  await page.goto("/");
  await expect(page.getByRole("heading", { name: /글자로 열린 세계/ })).toBeVisible();
  await page.getByLabel("웹 계정 이메일").fill(fixture.email);
  await page.getByLabel("비밀번호").fill("web-stack-password");
  await page.getByRole("button", { name: "성문 열기" }).click();
  try {
    await expect(page.getByText("이 계정에 연결된 캐릭터가 없습니다")).toBeVisible();
  } catch (error) {
    process.stderr.write(`stack-e2e: empty-roster responses=${JSON.stringify(statuses)} auth-visible=${await page.getByRole("heading", { name: /글자로 열린 세계/ }).isVisible()} alerts=${await page.getByRole("alert").count()}\n`);
    let alerts = (await page.getByRole("alert").allTextContents()).join(" | ");
    for (const secret of [fixture.accessToken, fixture.email, fixture.gamePassword, "web-stack-password", `refresh-${fixture.userId}`]) {
      if (secret) alerts = alerts.split(secret).join("<REDACTED>");
    }
    process.stderr.write(`stack-e2e: login-alerts=${JSON.stringify(alerts.slice(0, 500))} network-failures=${JSON.stringify(failures)}\n`);
    throw error;
  } finally {
    page.off("response", observe);
    page.off("requestfailed", failed);
  }
}

async function submitOnboardingInput(page: Page, value: string): Promise<void> {
  const command = page.getByLabel("온보딩 입력");
  await expect(command).toBeEnabled();
  await command.fill(value);
  await page.getByRole("button", { name: "보내기" }).click();
  await expect(command).toHaveValue("");
}

class PlaywrightClaimXtermFlowDriver implements ClaimXtermFlowDriver {
  private readonly terminal;

  constructor(private readonly page: Page) {
    this.terminal = page.getByLabel("캐릭터 온보딩 터미널");
  }

  async waitForReadyNamePrompt(): Promise<void> {
    await expect(this.terminal).toBeVisible();
    // `OnboardingTerminal` discards xterm onData before this observed state.
    // The C prompt also proves that the ready handler is relaying game bytes.
    await expect(this.terminal).toHaveAttribute("data-onboarding-ready", "true");
    await expect(this.terminal).toContainText(/당신의 이름은 무엇입니까/);
  }

  async typeThroughRenderedXterm(value: string): Promise<void> {
    // This deliberately targets xterm's browser-owned textarea instead of the
    // responsive fallback form. The password follows the exact desktop
    // keyboard/onData/WebSocket route and is never rendered by this harness.
    const xtermInput = this.terminal.locator("textarea.xterm-helper-textarea");
    await expect(xtermInput).toBeAttached();
    await this.terminal.click();
    await expect(xtermInput).toBeFocused();
    await this.page.keyboard.type(value);
    await this.page.keyboard.press("Enter");
  }

  async waitForCPasswordPrompt(): Promise<void> {
    await expect(this.terminal).toContainText(/암호를 넣어 주십시요/);
  }
}

async function assertRosterThenAdmission(page: Page, characterName: string): Promise<void> {
  await expect(page.locator(".selected-character-bar strong")).toHaveText(characterName);
  await expect(page.getByText("무한대전 세계와 연결됐습니다.")).toBeVisible();

  await page.getByRole("button", { name: "캐릭터 변경" }).click();
  await expect(page.getByRole("heading", { name: "입장할 캐릭터를 고르세요" })).toBeVisible();
  await expect(page.getByText(characterName, { exact: true })).toBeVisible();
  await expect(page.getByText(/ACTIVE/)).toBeVisible();

  const character = page.locator('input[name="mud-character"]');
  await expect(character).toHaveCount(1);
  await character.check();
  await page.getByRole("button", { name: "게임 입장" }).click();
  await expect(page.locator(".selected-character-bar strong")).toHaveText(characterName);
  await expect(page.getByText("무한대전 세계와 연결됐습니다.")).toBeVisible();

  // The ready control alone proves only Gateway authentication. Submit a
  // normal command through MudTerminal and require the C MUD's Korean output.
  const commandBar = page.locator(".command-bar");
  const command = commandBar.getByLabel("명령");
  await expect(command).toBeEnabled();
  await command.fill("건강");
  await commandBar.getByRole("button", { name: "보내기" }).click();
  await expect(page.locator(".terminal-viewport")).toContainText(/체력/);
}

export async function runWebStackAcceptance({
  claim,
  provision,
  server,
}: {
  claim: WebStackClaimFixture;
  provision: WebStackProvisionFixture;
  server: WebStackServer;
}): Promise<void> {
  const browser = await chromium.launch({ headless: true });
  try {
    const provisionContext = await browser.newContext({ baseURL: server.baseUrl });
    const provisionPage = await provisionContext.newPage();
    await installAuthBoundary(provisionPage, provision, server);
    await signInToEmptyRoster(provisionPage, provision);
    await provisionPage.getByRole("button", { name: "새 캐릭터 만들기" }).click();
    await expect(provisionPage.getByRole("heading", { name: "새 캐릭터 만들기" })).toBeVisible();
    await submitOnboardingInput(provisionPage, provision.characterName);
    await submitOnboardingInput(provisionPage, provision.gender);
    await submitOnboardingInput(provisionPage, provision.characterClass);
    await submitOnboardingInput(provisionPage, provision.stats);
    await submitOnboardingInput(provisionPage, provision.weapon);
    await submitOnboardingInput(provisionPage, provision.alignment);
    await submitOnboardingInput(provisionPage, provision.race);
    await provisionPage.getByLabel("게임 비밀번호 입력").fill(provision.gamePassword);
    await provisionPage.getByRole("button", { name: "보내기" }).click();
    await assertRosterThenAdmission(provisionPage, provision.characterName);
    await provisionContext.close();

    const claimContext = await browser.newContext({ baseURL: server.baseUrl });
    const claimPage = await claimContext.newPage();
    await installAuthBoundary(claimPage, claim, server);
    await signInToEmptyRoster(claimPage, claim);
    await claimPage.getByRole("button", { name: "기존 캐릭터 연결" }).click();
    await expect(claimPage.getByRole("heading", { name: "기존 캐릭터 연결" })).toBeVisible();
    await completeClaimThroughRenderedXterm(
      new PlaywrightClaimXtermFlowDriver(claimPage),
      claim.characterName,
      claim.gamePassword,
    );
    await assertRosterThenAdmission(claimPage, claim.characterName);
    await claimContext.close();
  } finally {
    await browser.close();
  }
}

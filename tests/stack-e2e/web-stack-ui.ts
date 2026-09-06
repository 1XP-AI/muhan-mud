import assert from "node:assert/strict";
import { spawn, type ChildProcess } from "node:child_process";
import { chromium, expect, type Page } from "@playwright/test";

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
}

type TimerApi = Pick<typeof globalThis, "clearTimeout" | "setTimeout">;

interface WebStackStopOptions {
  graceTimeoutMs?: number;
  killTimeoutMs?: number;
  signalProcessTree?: (child: ChildProcess, signal: NodeJS.Signals) => void;
  timers?: TimerApi;
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

    child.once("exit", onExit);
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
        SUPABASE_PUBLIC_URL: supabaseUrl,
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
  return { baseUrl: `http://127.0.0.1:${port}`, child };
}

export async function waitForWebStackServer(server: WebStackServer): Promise<void> {
  const deadline = Date.now() + 60_000;
  let lastError: unknown;
  while (Date.now() < deadline) {
    if (server.child.exitCode !== null) {
      throw new Error(`web server exited before listening (${server.child.exitCode})`);
    }
    try {
      const response = await fetch(server.baseUrl);
      if (response.ok) return;
      lastError = new Error(`web server returned ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
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
  if (server.child.exitCode !== null) {
    assert.equal(server.child.exitCode, 0, "web server exited unsuccessfully");
    return;
  }
  try {
    signalProcessTree(server.child, "SIGTERM");
    const [code, signal] = await waitForWebStackExit(server.child, graceTimeoutMs, timers);
    assert.ok(signal === "SIGTERM" || code === 0, `web server exited unexpectedly (${code ?? signal})`);
  } catch (termError) {
    // A development server that ignores graceful termination still must not
    // outlive this disposable runner. SIGKILL targets the same pnpm/Next tree.
    // The group can exit between the initial exitCode observation and kill().
    // POSIX reports that as ESRCH, but a normal child exit is successful cleanup.
    if (server.child.exitCode !== null) {
      assert.equal(server.child.exitCode, 0, "web server exited unsuccessfully");
      return;
    }
    try {
      signalProcessTree(server.child, "SIGKILL");
      await waitForWebStackExit(server.child, killTimeoutMs, timers);
    } catch (killError) {
      if (server.child.exitCode !== null) {
        assert.equal(server.child.exitCode, 0, "web server exited unsuccessfully");
        return;
      }
      throw new AggregateError(
        [termError, killError],
        "web server process tree could not be terminated",
      );
    }
  }
}

async function installAuthBoundary(page: Page, fixture: WebStackFixture): Promise<void> {
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
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(session) });
  });
  await page.route("**/auth/v1/user**", async (route) => {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(session.user) });
  });
}

async function signInToEmptyRoster(page: Page, fixture: WebStackFixture): Promise<void> {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: /글자로 열린 세계/ })).toBeVisible();
  await page.getByLabel("웹 계정 이메일").fill(fixture.email);
  await page.getByLabel("비밀번호").fill("web-stack-password");
  await page.getByRole("button", { name: "성문 열기" }).click();
  await expect(page.getByText("이 계정에 연결된 캐릭터가 없습니다")).toBeVisible();
}

async function submitOnboardingInput(page: Page, value: string): Promise<void> {
  const command = page.getByLabel("온보딩 입력");
  await expect(command).toBeEnabled();
  await command.fill(value);
  await page.getByRole("button", { name: "보내기" }).click();
  await expect(command).toHaveValue("");
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
    await installAuthBoundary(provisionPage, provision);
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
    await installAuthBoundary(claimPage, claim);
    await signInToEmptyRoster(claimPage, claim);
    await claimPage.getByRole("button", { name: "기존 캐릭터 연결" }).click();
    await expect(claimPage.getByRole("heading", { name: "기존 캐릭터 연결" })).toBeVisible();
    await submitOnboardingInput(claimPage, claim.characterName);
    await claimPage.getByLabel("게임 비밀번호 입력").fill(claim.gamePassword);
    await claimPage.getByRole("button", { name: "보내기" }).click();
    await assertRosterThenAdmission(claimPage, claim.characterName);
    await claimContext.close();
  } finally {
    await browser.close();
  }
}

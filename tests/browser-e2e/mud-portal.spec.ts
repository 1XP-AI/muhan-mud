import { expect, test, type Page } from "@playwright/test";

const USER_ID = "11111111-1111-4111-8111-111111111111";
const ACCESS_TOKEN = "test-access-token-placeholder";
const CORRELATION_ID = "22222222-2222-4222-8222-222222222222";

type FakeSocketHandle = {
  close: (code?: number, reason?: string) => void;
  emitMessage: (data: string | ArrayBuffer) => void;
  messages: string[];
  sentBytes: number[][];
  url: string;
};

type MockRosterCharacter = {
  id: string;
  world_id: string;
  legacy_name: string;
  lifecycle: "active";
};

type BrowserBoundaryState = {
  roster: MockRosterCharacter[];
  rosterRequests: number;
};

const browserBoundaryStates = new WeakMap<Page, BrowserBoundaryState>();

declare global {
  interface Window {
    __muhanFakeSockets?: FakeSocketHandle[];
  }
}

async function installBoundaries(page: Page): Promise<void> {
  const state: BrowserBoundaryState = { roster: [], rosterRequests: 0 };
  browserBoundaryStates.set(page, state);

  await page.route("**/auth/v1/token**", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        access_token: ACCESS_TOKEN,
        token_type: "bearer",
        expires_in: 3600,
        expires_at: Math.floor(Date.now() / 1000) + 3600,
        refresh_token: "refresh-token-placeholder",
        user: {
          id: USER_ID,
          aud: "authenticated",
          role: "authenticated",
          email: "player@example.test",
          app_metadata: { provider: "email", providers: ["email"] },
          user_metadata: {},
        },
      }),
    });
  });

  await page.route("**/auth/v1/user**", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        id: USER_ID,
        aud: "authenticated",
        role: "authenticated",
        email: "player@example.test",
        app_metadata: { provider: "email", providers: ["email"] },
        user_metadata: {},
      }),
    });
  });

  await page.route("**/rest/v1/game_characters**", async (route) => {
    state.rosterRequests += 1;
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(state.roster),
    });
  });

  await page.addInitScript(() => {
    const nativeWebSocket = window.WebSocket;
    const sockets: FakeSocketHandle[] = [];
    window.__muhanFakeSockets = sockets;

    class FakeWebSocket extends EventTarget {
      static readonly CONNECTING = 0;
      static readonly OPEN = 1;
      static readonly CLOSING = 2;
      static readonly CLOSED = 3;
      readonly CONNECTING = 0;
      readonly OPEN = 1;
      readonly CLOSING = 2;
      readonly CLOSED = 3;
      readonly url: string;
      readonly protocol: string;
      binaryType = "blob";
      readyState = FakeWebSocket.CONNECTING;
      bufferedAmount = 0;
      extensions = "";
      onopen: ((event: Event) => void) | null = null;
      onmessage: ((event: MessageEvent) => void) | null = null;
      onerror: ((event: Event) => void) | null = null;
      onclose: ((event: CloseEvent) => void) | null = null;
      readonly messages: string[] = [];
      readonly sentBytes: number[][] = [];

      constructor(url: string | URL, protocols?: string | string[]) {
        super();
        this.url = String(url);
        this.protocol = Array.isArray(protocols) ? protocols[0] ?? "" : protocols ?? "";
        const handle: FakeSocketHandle = {
          close: (code = 1000, reason = "") => this.close(code, reason),
          emitMessage: (data) => this.emitMessage(data),
          messages: this.messages,
          sentBytes: this.sentBytes,
          url: this.url,
        };
        sockets.push(handle);
        queueMicrotask(() => {
          if (this.readyState !== FakeWebSocket.CONNECTING) return;
          this.readyState = FakeWebSocket.OPEN;
          const event = new Event("open");
          this.dispatchEvent(event);
          this.onopen?.(event);
          // Supabase Realtime is an external boundary for these tests. Give
          // its client a successful open without speaking the realtime wire
          // protocol, while keeping the Gateway protocol fully observable.
        });
      }

      send(data: string | ArrayBuffer | ArrayBufferView): void {
        if (typeof data === "string") {
          this.messages.push(data);
          if (this.url.includes("/realtime/")) return;
          let frame: unknown;
          try {
            frame = JSON.parse(data);
          } catch {
            return;
          }
          if (!frame || typeof frame !== "object") return;
          const control = frame as { type?: string; mode?: string };
          if (control.type === "onboarding-auth") {
            queueMicrotask(() => this.emitMessage(JSON.stringify({ type: "onboarding-ready" })));
          }
          if (control.type === "auth") {
            queueMicrotask(() => this.emitMessage(JSON.stringify({ type: "ready" })));
          }
          return;
        }
        const bytes = data instanceof ArrayBuffer
          ? new Uint8Array(data)
          : new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
        this.sentBytes.push([...bytes]);
      }

      close(code = 1000, reason = ""): void {
        if (this.readyState >= FakeWebSocket.CLOSING) return;
        this.readyState = FakeWebSocket.CLOSING;
        queueMicrotask(() => {
          this.readyState = FakeWebSocket.CLOSED;
          const event = new CloseEvent("close", { code, reason, wasClean: code === 1000 });
          this.dispatchEvent(event);
          this.onclose?.(event);
        });
      }

      emitMessage(data: string | ArrayBuffer): void {
        const event = new MessageEvent("message", { data });
        this.dispatchEvent(event);
        this.onmessage?.(event);
      }

      ping(): void {}
    }

    window.WebSocket = new Proxy(nativeWebSocket, {
      construct(target, args) {
        const url = String(args[0]);
        return url.includes("gateway.local") || url.includes("/realtime/")
          ? Reflect.construct(FakeWebSocket, args)
          : Reflect.construct(target, args);
      },
    });

  });
}

async function signInToEmptyRoster(page: Page): Promise<void> {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: /글자로 열린 세계/ })).toBeVisible();
  await page.getByLabel("웹 계정 이메일").fill("player@example.test");
  await page.getByLabel("비밀번호").fill("placeholder-password");
  await page.getByRole("button", { name: "성문 열기" }).click();
  await expect(page.getByText("이 계정에 연결된 캐릭터가 없습니다")).toBeVisible();
}

async function fakeSocketCount(page: Page): Promise<number> {
  return page.evaluate(() => window.__muhanFakeSockets?.length ?? 0);
}

async function waitForOnboardingSocket(
  page: Page,
  mode: "provision" | "claim" = "provision",
): Promise<void> {
  await expect.poll(() => fakeSocketCount(page), { timeout: 5_000 }).toBeGreaterThan(0);
  await expect(
    page.getByRole("heading", { name: mode === "provision" ? "새 캐릭터 만들기" : "기존 캐릭터 연결" }),
  ).toBeVisible();
  await expect(page.getByLabel("온보딩 입력")).toBeEnabled();
}

async function emitOnboardingControl(
  page: Page,
  control: Record<string, unknown>,
): Promise<void> {
  await page.evaluate((value) => {
    const sockets = window.__muhanFakeSockets?.filter((entry) => entry.url.includes("/onboarding")) ?? [];
    sockets[sockets.length - 1]?.emitMessage(JSON.stringify(value));
  }, control);
}

function setMockActiveRoster(page: Page, character: MockRosterCharacter): void {
  const state = browserBoundaryStates.get(page);
  if (!state) throw new Error("mocked browser boundaries were not installed");
  state.roster = [character];
}

async function completeOnboardingToActiveRoster(
  page: Page,
  completion: "provisioned" | "claimed",
  character: MockRosterCharacter,
): Promise<void> {
  const state = browserBoundaryStates.get(page);
  if (!state) throw new Error("mocked browser boundaries were not installed");

  const requestsBeforeCompletion = state.rosterRequests;
  setMockActiveRoster(page, character);
  await emitOnboardingControl(page, { type: completion, characterId: character.id });

  await expect.poll(() => state.rosterRequests).toBeGreaterThan(requestsBeforeCompletion);
  await expect(page.getByRole("heading", { name: "입장할 캐릭터를 고르세요" })).toBeVisible();
  await expect(page.getByText(character.legacy_name, { exact: true })).toBeVisible();
}

async function enterActiveCharacterAndAssertGatewayAdmission(
  page: Page,
  character: MockRosterCharacter,
): Promise<void> {
  await page.locator('input[name="mud-character"]').check();
  await page.getByRole("button", { name: "게임 입장" }).click();
  await expect(page.locator(".selected-character-bar strong")).toHaveText(character.legacy_name);
  await expect(page.getByText("무한대전 세계와 연결됐습니다.")).toBeVisible();

  await expect.poll(async () => page.evaluate(() => {
    const sockets = window.__muhanFakeSockets?.filter(
      (entry) => entry.url.includes("gateway.local") && !entry.url.includes("/onboarding"),
    ) ?? [];
    return sockets[sockets.length - 1]?.messages.map((message) => JSON.parse(message)) ?? [];
  })).toContainEqual({
    type: "auth",
    accessToken: ACCESS_TOKEN,
    characterId: character.id,
  });
}

test.beforeEach(async ({ page }) => {
  await installBoundaries(page);
});

test("signed-in empty roster shows provision and claim actions", async ({ page }) => {
  await signInToEmptyRoster(page);

  await expect(page.getByRole("button", { name: "새 캐릭터 만들기" })).toBeVisible();
  await expect(page.getByRole("button", { name: "기존 캐릭터 연결" })).toBeVisible();
  await expect(page.getByText("게임 비밀번호는 웹 계정 비밀번호와 다른 값을 사용하세요.")).toBeVisible();
});

for (const completedFlow of [
  {
    mode: "provision" as const,
    completion: "provisioned" as const,
    character: {
      id: "33333333-3333-4333-8333-333333333333",
      world_id: "muhan-01",
      legacy_name: "ProvisionHero",
      lifecycle: "active" as const,
    },
  },
  {
    mode: "claim" as const,
    completion: "claimed" as const,
    character: {
      id: "44444444-4444-4444-8444-444444444444",
      world_id: "muhan-01",
      legacy_name: "ClaimHero",
      lifecycle: "active" as const,
    },
  },
]) {
  test(`completed ${completedFlow.mode} refreshes to an active roster and permits normal game admission`, async ({ page }) => {
    await signInToEmptyRoster(page);
    await page.getByRole("button", {
      name: completedFlow.mode === "provision" ? "새 캐릭터 만들기" : "기존 캐릭터 연결",
    }).click();
    await waitForOnboardingSocket(page, completedFlow.mode);

    await completeOnboardingToActiveRoster(
      page,
      completedFlow.completion,
      completedFlow.character,
    );
    await enterActiveCharacterAndAssertGatewayAdmission(page, completedFlow.character);
  });
}

test("provision mounts real xterm and sends typed and pasted input as exact onboarding bytes", async ({ page }) => {
  await signInToEmptyRoster(page);
  await page.getByRole("button", { name: "새 캐릭터 만들기" }).click();
  await waitForOnboardingSocket(page);
  await expect(page.locator(".onboarding-viewport .xterm-screen")).toBeVisible();

  const command = page.getByLabel("온보딩 입력");
  await command.focus();
  await page.keyboard.insertText("pasted-placeholder");
  await command.press("Enter");
  await command.focus();
  await page.keyboard.type("typed-placeholder");
  await command.press("Enter");

  await expect.poll(async () => page.evaluate(() => {
    const sockets = window.__muhanFakeSockets?.filter((entry) => entry.url.includes("/onboarding")) ?? [];
    const socket = sockets[sockets.length - 1];
    return socket?.sentBytes ?? [];
  })).toEqual([
    [...new TextEncoder().encode("pasted-placeholder\n")],
    [...new TextEncoder().encode("typed-placeholder\n")],
  ]);
  expect(await page.content()).not.toContain("test-access-token-placeholder");
});

test("mobile viewport keeps the onboarding command input usable", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await signInToEmptyRoster(page);
  await page.getByRole("button", { name: "새 캐릭터 만들기" }).click();
  await waitForOnboardingSocket(page);

  const command = page.getByLabel("온보딩 입력");
  await expect(command).toBeVisible();
  await command.fill("mobile-placeholder");
  await expect(page.getByRole("button", { name: "보내기" })).toBeEnabled();
  await page.getByRole("button", { name: "보내기" }).click();
  await expect.poll(async () => page.evaluate(() => {
    const sockets = window.__muhanFakeSockets?.filter((entry) => entry.url.includes("/onboarding")) ?? [];
    const socket = sockets[sockets.length - 1];
    return socket?.sentBytes[0] ?? [];
  })).toEqual([...new TextEncoder().encode("mobile-placeholder\n")]);
});

test("claim sends the legacy name and password safely, then returns to the roster", async ({ page }) => {
  await signInToEmptyRoster(page);
  await page.getByRole("button", { name: "기존 캐릭터 연결" }).click();
  await waitForOnboardingSocket(page, "claim");

  const authFrames = await page.evaluate(() => {
    const sockets = window.__muhanFakeSockets?.filter((entry) => entry.url.includes("/onboarding")) ?? [];
    const socket = sockets[sockets.length - 1];
    return socket?.messages
      .map((message) => {
        try {
          return JSON.parse(message) as unknown;
        } catch {
          return null;
        }
      })
      .filter((frame): frame is { type?: string; mode?: string; correlationId?: string } =>
        typeof frame === "object" && frame !== null && "type" in frame && (frame as { type?: unknown }).type === "onboarding-auth",
      ) ?? [];
  });
  expect(authFrames).toHaveLength(1);
  expect(authFrames[0]).toMatchObject({
    type: "onboarding-auth",
    mode: "claim",
    accessToken: ACCESS_TOKEN,
  });
  expect(authFrames[0]?.correlationId).toMatch(
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/,
  );
  // The JWT is required only in the auth frame; neither the DOM nor xterm may
  // expose it, and private C-side claim controls must stay server-side.
  expect(await page.content()).not.toContain(ACCESS_TOKEN);
  expect(await page.locator(".onboarding-viewport").textContent()).not.toContain(ACCESS_TOKEN);
  expect(await page.locator(".onboarding-viewport").textContent()).not.toContain("MUD1O CHALLENGE");
  expect(await page.locator(".onboarding-viewport").textContent()).not.toContain("MUD1O ALLOW");

  await expect(page.locator(".onboarding-viewport .xterm-screen")).toBeVisible();
  const name = "ClaimHero";
  const command = page.getByLabel("온보딩 입력");
  await expect(page.getByLabel("게임 비밀번호 입력")).toHaveCount(0);
  await expect(command).toHaveAttribute("type", "text");
  await command.fill(name);
  await page.getByRole("button", { name: "보내기" }).click();
  await expect.poll(async () => page.evaluate(() => {
    const sockets = window.__muhanFakeSockets?.filter((entry) => entry.url.includes("/onboarding")) ?? [];
    return sockets[sockets.length - 1]?.sentBytes ?? [];
  })).toContainEqual([...new TextEncoder().encode(`${name}\n`)]);

  await emitOnboardingControl(page, { type: "echo", enabled: false });
  const password = "claim-secret";
  const passwordInput = page.getByLabel("게임 비밀번호 입력");
  await expect(passwordInput).toHaveAttribute("type", "password");
  await passwordInput.fill(password);
  await expect(page.locator("body")).not.toContainText(password);
  expect(await page.locator("body").textContent()).not.toContain(password);
  expect(await page.locator(".onboarding-viewport").textContent()).not.toContain(password);
  await page.getByRole("button", { name: "보내기" }).click();
  await expect(passwordInput).toHaveValue("");
  expect(await page.content()).not.toContain(password);
  expect(await page.locator(".onboarding-viewport").textContent()).not.toContain(password);
  await expect.poll(async () => page.evaluate(() => {
    const sockets = window.__muhanFakeSockets?.filter((entry) => entry.url.includes("/onboarding")) ?? [];
    return sockets[sockets.length - 1]?.sentBytes ?? [];
  })).toContainEqual([...new TextEncoder().encode(`${password}\n`)]);

  await emitOnboardingControl(page, {
    type: "claimed",
    characterId: "33333333-3333-4333-8333-333333333333",
  });
  await expect(page.getByText("이 계정에 연결된 캐릭터가 없습니다")).toBeVisible();
  await expect(passwordInput).toHaveCount(0);
  const socketCountAfterClaim = await fakeSocketCount(page);

  await page.evaluate(() => {
    const sockets = window.__muhanFakeSockets?.filter((entry) => entry.url.includes("/onboarding")) ?? [];
    sockets[sockets.length - 1]?.close(1000, "normal");
  });
  await page.waitForTimeout(900);
  expect(await fakeSocketCount(page)).toBe(socketCountAfterClaim);
});

test("claim clears the password field and disables input immediately on an error", async ({ page }) => {
  await signInToEmptyRoster(page);
  await page.getByRole("button", { name: "기존 캐릭터 연결" }).click();
  await waitForOnboardingSocket(page, "claim");
  await emitOnboardingControl(page, { type: "echo", enabled: false });

  const passwordInput = page.getByLabel("게임 비밀번호 입력");
  await passwordInput.fill("claim-secret");
  await expect(passwordInput).toHaveValue("claim-secret");
  await emitOnboardingControl(page, { type: "error", reason: "claim denied" });

  await expect(passwordInput).toHaveCount(0);
  await expect(page.getByLabel("온보딩 입력")).toBeDisabled();
  expect(await page.locator("body").textContent()).not.toContain("claim-secret");
});

test("claim clears an unsent password before echo becomes visible again", async ({ page }) => {
  await signInToEmptyRoster(page);
  await page.getByRole("button", { name: "기존 캐릭터 연결" }).click();
  await waitForOnboardingSocket(page, "claim");
  await emitOnboardingControl(page, { type: "echo", enabled: false });

  const passwordInput = page.getByLabel("게임 비밀번호 입력");
  await passwordInput.fill("claim-secret");
  await expect(passwordInput).toHaveValue("claim-secret");

  await emitOnboardingControl(page, { type: "echo", enabled: true });

  await expect(passwordInput).toHaveCount(0);
  const visibleInput = page.getByLabel("온보딩 입력");
  await expect(visibleInput).toHaveAttribute("type", "text");
  await expect(visibleInput).toHaveValue("");
});

test("claim clears the password before a retryable close reconnects", async ({ page }) => {
  await signInToEmptyRoster(page);
  await page.getByRole("button", { name: "기존 캐릭터 연결" }).click();
  await waitForOnboardingSocket(page, "claim");
  await emitOnboardingControl(page, { type: "echo", enabled: false });

  const passwordInput = page.getByLabel("게임 비밀번호 입력");
  await passwordInput.fill("claim-secret");
  await page.evaluate(() => {
    const sockets = window.__muhanFakeSockets?.filter((entry) => entry.url.includes("/onboarding")) ?? [];
    sockets[sockets.length - 1]?.close(1011, "transient");
  });

  await expect(passwordInput).toHaveCount(0);
  await expect(page.getByLabel("온보딩 입력")).toBeDisabled();
  await expect.poll(() => fakeSocketCount(page), { timeout: 3_000 }).toBeGreaterThan(1);
  await page.getByRole("button", { name: "취소하고 캐릭터 선택" }).click();
});

test("normal close does not reconnect, while retryable close follows bounded policy", async ({ page }) => {
  await signInToEmptyRoster(page);
  await page.getByRole("button", { name: "새 캐릭터 만들기" }).click();
  await waitForOnboardingSocket(page);

  const firstSocketCount = await fakeSocketCount(page);
  await page.evaluate(() => {
    const sockets = window.__muhanFakeSockets?.filter((entry) => entry.url.includes("/onboarding")) ?? [];
    sockets[sockets.length - 1]?.close(1000, "normal");
  });
  await expect(page.getByText("이 계정에 연결된 캐릭터가 없습니다")).toBeVisible();
  await page.waitForTimeout(900);
  expect(await fakeSocketCount(page)).toBe(firstSocketCount);

  await page.getByRole("button", { name: "새 캐릭터 만들기" }).click();
  await waitForOnboardingSocket(page);
  const retrySocketCount = await fakeSocketCount(page);
  await page.evaluate(() => {
    const sockets = window.__muhanFakeSockets?.filter((entry) => entry.url.includes("/onboarding")) ?? [];
    sockets[sockets.length - 1]?.close(1011, "transient");
  });
  await expect.poll(() => fakeSocketCount(page), { timeout: 3_000 }).toBeGreaterThan(retrySocketCount);
});

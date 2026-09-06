import { expect, test, type Page } from "@playwright/test";

const USER_ID = "11111111-1111-4111-8111-111111111111";
const ACCESS_TOKEN = "test-access-token-placeholder";

type FakeSocketHandle = { url: string };

declare global {
  interface Window {
    __muhanFeatureOffSockets?: FakeSocketHandle[];
  }
}

async function installFeatureOffBoundaries(page: Page): Promise<void> {
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
    await route.fulfill({ status: 200, contentType: "application/json", body: "[]" });
  });

  await page.addInitScript(() => {
    const nativeWebSocket = window.WebSocket;
    const sockets: FakeSocketHandle[] = [];
    window.__muhanFeatureOffSockets = sockets;

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

      constructor(url: string | URL, protocols?: string | string[]) {
        super();
        this.url = String(url);
        this.protocol = Array.isArray(protocols) ? protocols[0] ?? "" : protocols ?? "";
        sockets.push({ url: this.url });
        queueMicrotask(() => {
          if (this.readyState !== FakeWebSocket.CONNECTING) return;
          this.readyState = FakeWebSocket.OPEN;
          const event = new Event("open");
          this.dispatchEvent(event);
          this.onopen?.(event);
        });
      }

      send(): void {}

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
    }

    window.WebSocket = new Proxy(nativeWebSocket, {
      construct(target, args) {
        const url = String(args[0]);
        return url.includes("gateway.local")
          ? Reflect.construct(FakeWebSocket, args)
          : Reflect.construct(target, args);
      },
    });
  });
}

test.beforeEach(async ({ page }) => {
  await installFeatureOffBoundaries(page);
});

test("feature-off keeps the empty roster unreachable from every gateway socket", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: /글자로 열린 세계/ })).toBeVisible();
  await page.getByLabel("웹 계정 이메일").fill("player@example.test");
  await page.getByLabel("비밀번호").fill("placeholder-password");
  await page.getByRole("button", { name: "성문 열기" }).click();

  await expect(page.getByText("이 계정에 연결된 캐릭터가 없습니다")).toBeVisible();
  await expect(page.getByRole("button", { name: "새 캐릭터 만들기" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "기존 캐릭터 연결" })).toHaveCount(0);
  await expect(page.getByText("이 서버에서는 기존 캐릭터 연결을 웹에서 시작할 수 없습니다.")).toBeVisible();
  await expect.poll(() => page.evaluate(() => window.__muhanFeatureOffSockets ?? [])).toEqual([]);
});

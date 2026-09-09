import { expect, test, type Page } from "@playwright/test";

type FakeGateway = {
  connectionCount: number;
  dropConnection: () => void;
  messages: string[];
  emitView: (text: string, secret?: boolean, closed?: boolean) => void;
  setInputEcho: (enabled: boolean) => void;
};

declare global {
  interface Window {
    __muhanGateway?: FakeGateway;
  }
}

async function installFakeGateway(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const nativeWebSocket = window.WebSocket;
    const messages: string[] = [];
    let socket: FakeSocket | undefined;
    let connectionCount = 0;
    let inputEcho = true;

    class FakeSocket extends EventTarget {
      static readonly CONNECTING = 0;
      static readonly OPEN = 1;
      static readonly CLOSING = 2;
      static readonly CLOSED = 3;
      readonly CONNECTING = 0;
      readonly OPEN = 1;
      readonly CLOSING = 2;
      readonly CLOSED = 3;
      readonly url: string;
      readonly protocol = "";
      readonly binaryType = "blob";
      readonly bufferedAmount = 0;
      readonly extensions = "";
      readyState = FakeSocket.CONNECTING;
      onopen: ((event: Event) => void) | null = null;
      onmessage: ((event: MessageEvent) => void) | null = null;
      onerror: ((event: Event) => void) | null = null;
      onclose: ((event: CloseEvent) => void) | null = null;

      constructor(url: string | URL) {
        super();
        this.url = String(url);
        socket = this;
        connectionCount += 1;
        queueMicrotask(() => {
          this.readyState = FakeSocket.OPEN;
          const event = new Event("open");
          this.dispatchEvent(event);
          this.onopen?.(event);
          this.emitView("이름을 입력하세요: ");
        });
      }

      send(data: string): void {
        messages.push(data);
        const frame = JSON.parse(data) as { type?: string; text?: string };
        if (frame.type === "line") {
          queueMicrotask(() =>
            this.emitView(
              inputEcho
                ? `받은 입력: ${frame.text ?? ""}\r\n`
                : "입력이 처리되었습니다.\r\n",
            ),
          );
        }
      }

      close(code = 1000, reason = ""): void {
        if (this.readyState >= FakeSocket.CLOSING) return;
        this.readyState = FakeSocket.CLOSING;
        const event = new CloseEvent("close", { code, reason, wasClean: code === 1000 });
        this.dispatchEvent(event);
        this.onclose?.(event);
        this.readyState = FakeSocket.CLOSED;
      }

      emitView(text: string, secret = false, closed = false): void {
        const event = new MessageEvent("message", { data: JSON.stringify({ type: "view", text, secret, closed }) });
        this.dispatchEvent(event);
        this.onmessage?.(event);
      }
    }

    window.__muhanGateway = {
      connectionCount,
      dropConnection: () => socket?.close(1006, "network drop"),
      messages,
      emitView: (text, secret = false, closed = false) => socket?.emitView(text, secret, closed),
      setInputEcho: (enabled) => {
        inputEcho = enabled;
      },
    };
    Object.defineProperty(window.__muhanGateway, "connectionCount", {
      get: () => connectionCount,
    });
    window.WebSocket = new Proxy(nativeWebSocket, {
      construct(target, args) {
        return String(args[0]).includes("gateway.local")
          ? Reflect.construct(FakeSocket, args)
          : Reflect.construct(target, args);
      },
    });
  });
}

async function commitKoreanComposition(page: Page, text: string): Promise<void> {
  const input = page.locator("textarea.xterm-helper-textarea");
  await input.evaluate((element, value) => {
    element.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true, data: "" }));
    element.value = value;
    element.dispatchEvent(new CompositionEvent("compositionupdate", { bubbles: true, data: value }));
    element.dispatchEvent(new InputEvent("input", {
      bubbles: true,
      data: value,
      inputType: "insertCompositionText",
      isComposing: true,
    }));
    element.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true, data: value }));
  }, text);
  // xterm waits one task after compositionend so Chromium can commit the
  // textarea value before it forwards the Korean code points to onData.
  await page.waitForTimeout(10);
}

test("xterm opens the original line protocol and keeps terminal focus", async ({ page }) => {
  await installFakeGateway(page);
  await page.goto("/");

  const input = page.getByRole("textbox", { name: "Terminal input" });
  await expect(input).toBeFocused();
  await expect(page.locator(".xterm-screen")).toContainText("무한대전 · 터미널 접속");
  await expect(page.locator(".xterm-screen")).toContainText("이름을 입력하세요:");

  await page.keyboard.type("Alice");
  await page.keyboard.press("Enter");
  await expect(page.locator(".xterm-screen")).toContainText("받은 입력: Alice");
  await expect.poll(() => page.evaluate(() => window.__muhanGateway?.messages ?? [])).toEqual([
    JSON.stringify({ type: "line", text: "Alice" }),
  ]);

  await page.evaluate(() => {
    const input = document.querySelector("textarea.xterm-helper-textarea") as HTMLTextAreaElement | null;
    input?.blur();
    window.dispatchEvent(new Event("focus"));
  });
  await expect(input).toBeFocused();
  await expect(page.locator("main input[type=email], main input[type=password], main button")).toHaveCount(0);
});

test("secret prompts suppress password echo in the terminal and DOM", async ({ page }) => {
  await installFakeGateway(page);
  await page.goto("/");

  const input = page.getByRole("textbox", { name: "Terminal input" });
  const password = "terminal-secret-42";
  await expect(page.locator(".xterm-screen")).toContainText("이름을 입력하세요:");

  await page.evaluate(() => {
    window.__muhanGateway?.setInputEcho(false);
    window.__muhanGateway?.emitView("비밀번호를 입력하세요: ", true);
  });
  await expect(page.locator(".xterm-screen")).toContainText("비밀번호를 입력하세요:");

  await page.keyboard.type(password);
  await expect(page.locator(".xterm-screen")).not.toContainText(password);
  await expect(page.locator("body")).not.toContainText(password);

  await page.keyboard.press("Enter");
  await expect.poll(() => page.evaluate(() => window.__muhanGateway?.messages ?? [])).toEqual([
    JSON.stringify({ type: "line", text: password }),
  ]);
  await expect(page.locator(".xterm-screen")).toContainText("입력이 처리되었습니다.");
  await expect(page.locator(".xterm-screen")).not.toContainText(password);
  expect(await page.content()).not.toContain(password);
  await expect(input).toBeFocused();
});

test("xterm preserves Korean composition and restores focus across mobile resize and reconnect", async ({ page }) => {
  await installFakeGateway(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");

  const input = page.getByRole("textbox", { name: "Terminal input" });
  await expect(input).toBeFocused();
  const mobileTerminal = await page.locator(".terminal").boundingBox();
  expect(mobileTerminal?.height ?? 0).toBeGreaterThan(0);
  expect(mobileTerminal?.height ?? 0).toBeLessThanOrEqual(844);

  await commitKoreanComposition(page, "한글");
  await page.evaluate(() => window.dispatchEvent(new Event("resize")));
  await page.keyboard.press("Enter");
  await expect(page.locator(".xterm-screen")).toContainText("받은 입력: 한글");
  await expect.poll(() => page.evaluate(() => window.__muhanGateway?.messages ?? [])).toEqual([
    JSON.stringify({ type: "line", text: "한글" }),
  ]);

  await input.evaluate((element) => element.blur());
  await page.evaluate(() => window.dispatchEvent(new Event("resize")));
  await expect(input).toBeFocused();

  await page.keyboard.type("draft");
  await page.evaluate(() => window.__muhanGateway?.dropConnection());
  await expect.poll(() => page.evaluate(() => window.__muhanGateway?.connectionCount ?? 0), {
    timeout: 5_000,
  }).toBe(2);
  await expect(page.locator(".xterm-screen")).toContainText("이름을 입력하세요:");
  await expect(input).toBeFocused();
  await expect(input).toHaveValue("");
  await expect.poll(() => page.evaluate(() => window.__muhanGateway?.messages ?? [])).toEqual([
    JSON.stringify({ type: "line", text: "한글" }),
  ]);
});

test("xterm bounds reconnects across repeated transient drops", async ({ page }) => {
  await installFakeGateway(page);
  await page.goto("/");

  await expect(page.locator(".xterm-screen")).toContainText("이름을 입력하세요:");
  await expect.poll(() => page.evaluate(() => window.__muhanGateway?.connectionCount ?? 0)).toBe(1);

  for (const expectedConnectionCount of [2, 3, 4]) {
    await page.evaluate(() => window.__muhanGateway?.dropConnection());
    await expect.poll(
      () => page.evaluate(() => window.__muhanGateway?.connectionCount ?? 0),
      { timeout: 5_000 },
    ).toBe(expectedConnectionCount);
  }

  await page.evaluate(() => window.__muhanGateway?.dropConnection());
  await expect(page.locator(".xterm-screen")).toContainText(
    "접속이 끝났습니다. 다시 접속하려면 페이지를 새로고침하십시오.",
  );
  await page.waitForTimeout(1_000);
  await expect.poll(() => page.evaluate(() => window.__muhanGateway?.connectionCount ?? 0)).toBe(4);
});

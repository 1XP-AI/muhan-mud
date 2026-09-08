import { expect, test, type Page } from "@playwright/test";

type FakeGateway = {
  messages: string[];
  emitView: (text: string, secret?: boolean, closed?: boolean) => void;
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
          queueMicrotask(() => this.emitView(`받은 입력: ${frame.text ?? ""}\r\n`));
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
      messages,
      emitView: (text, secret = false, closed = false) => socket?.emitView(text, secret, closed),
    };
    window.WebSocket = new Proxy(nativeWebSocket, {
      construct(target, args) {
        return String(args[0]).includes("gateway.local")
          ? Reflect.construct(FakeSocket, args)
          : Reflect.construct(target, args);
      },
    });
  });
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

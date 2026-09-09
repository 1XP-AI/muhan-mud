"use client";

import { useEffect, useRef } from "react";

import { TerminalLine } from "@/lib/terminal-line";
import {
  canRestoreTerminalFocus,
  shouldDeferTerminalResize,
} from "@/lib/terminal-focus";
import { validateGatewayUrl } from "@/lib/gateway-url";

import styles from "./classic-terminal.module.css";

const MAX_RECONNECT_ATTEMPTS = 3;
const MAX_RECONNECT_DELAY_MS = 30_000;

function reconnectDelay(attempt: number): number {
  return Math.min(
    MAX_RECONNECT_DELAY_MS,
    750 * 2 ** Math.max(0, attempt - 1),
  );
}

function shouldReconnect(code: number, attempt: number): boolean {
  if (attempt >= MAX_RECONNECT_ATTEMPTS) return false;
  return [1001, 1006, 1011, 1012, 1013].includes(code);
}

interface TerminalView {
  type: "view";
  text: string;
  secret: boolean;
  closed: boolean;
}

function isTerminalView(value: unknown): value is TerminalView {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return false;
  }
  const view = value as Record<string, unknown>;
  return (
    view.type === "view" &&
    typeof view.text === "string" &&
    typeof view.secret === "boolean" &&
    typeof view.closed === "boolean"
  );
}

export function ClassicTerminal({ url }: { url: string | null }) {
  const host = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let disposed = false;
    let cleanup = () => {};

    async function start() {
      const [{ Terminal }, { FitAddon }] = await Promise.all([
        import("@xterm/xterm"),
        import("@xterm/addon-fit"),
      ]);
      const element = host.current;
      if (disposed || !element) return;

      const term = new Terminal({
        allowProposedApi: false,
        convertEol: true,
        cursorBlink: true,
        cursorStyle: "bar",
        fontFamily:
          '"D2Coding", "Noto Sans Mono CJK KR", ui-monospace, monospace',
        fontSize: 16,
        lineHeight: 1.25,
        minimumContrastRatio: 7,
        rightClickSelectsWord: true,
        screenReaderMode: true,
        scrollback: 2_000,
        theme: {
          background: "#000080",
          foreground: "#e0e0e0",
          cursor: "#ffffff",
          cursorAccent: "#000080",
          selectionBackground: "#00ffff55",
        },
      });
      const fit = new FitAddon();
      term.loadAddon(fit);
      term.open(element);

      const line = new TerminalLine();
      const textarea = term.textarea;
      let socket: WebSocket | undefined;
      let reconnectTimer: number | undefined;
      let reconnectAttempt = 0;
      let resizeFrame: number | undefined;
      let resizePending = false;
      let composing = false;
      let permanentlyClosed = false;

      const clearPendingInput = () => {
        line.clear();
        if (textarea) textarea.value = "";
      };

      const hasSelection = () =>
        term.hasSelection() || Boolean(window.getSelection()?.toString());

      const focusTerminal = () => {
        const activeElement = document.activeElement;
        if (
          !canRestoreTerminalFocus({
            disposed,
            composing,
            hasSelection: hasSelection(),
            documentFocused: document.hasFocus(),
            activeElementOutsideTerminal:
              activeElement !== null &&
              activeElement !== document.body &&
              !element.contains(activeElement),
          })
        ) {
          return;
        }
        term.focus();
      };

      let focusFrame: number | undefined;
      const queueFocus = () => {
        if (disposed || focusFrame !== undefined) return;
        focusFrame = window.requestAnimationFrame(() => {
          focusFrame = undefined;
          focusTerminal();
        });
      };

      const syncMobileViewport = () => {
        const viewportHeight = window.visualViewport?.height ?? window.innerHeight;
        if (window.matchMedia("(max-width: 640px)").matches) {
          if (Number.isFinite(viewportHeight) && viewportHeight > 0) {
            element.style.height = `${Math.round(viewportHeight)}px`;
          }
        } else {
          element.style.removeProperty("height");
        }
      };

      const applyResize = () => {
        resizeFrame = undefined;
        if (disposed) return;
        syncMobileViewport();
        if (shouldDeferTerminalResize(composing)) {
          resizePending = true;
          return;
        }
        fit.fit();
        queueFocus();
      };

      const resize = () => {
        if (disposed) return;
        // Keep the terminal above an on-screen keyboard even while the IME is
        // composing; defer only xterm's row/column recalculation until the
        // composition has committed.
        syncMobileViewport();
        if (shouldDeferTerminalResize(composing)) {
          resizePending = true;
          return;
        }
        if (resizeFrame !== undefined) return;
        resizeFrame = window.requestAnimationFrame(applyResize);
      };

      const compositionStart = () => {
        composing = true;
      };
      const compositionEnd = () => {
        composing = false;
        if (resizePending) {
          resizePending = false;
          resize();
        } else {
          queueFocus();
        }
      };
      const pointerUp = () => {
        // Do not collapse an xterm selection or a browser text selection just
        // because the pointer was released over the terminal.
        if (!hasSelection()) queueFocus();
      };

      textarea?.addEventListener("compositionstart", compositionStart);
      textarea?.addEventListener("compositionend", compositionEnd);
      element.addEventListener("pointerup", pointerUp);
      window.addEventListener("focus", queueFocus);
      window.addEventListener("resize", resize);
      window.visualViewport?.addEventListener("resize", resize);
      window.visualViewport?.addEventListener("scroll", resize);
      const observer = new ResizeObserver(resize);
      observer.observe(element);

      term.attachCustomKeyEventHandler((event) => event.key !== "Tab");
      const data = term.onData((value) => {
        const currentSocket = socket;
        if (
          disposed ||
          !currentSocket ||
          currentSocket.readyState !== WebSocket.OPEN
        ) {
          return;
        }

        // TerminalLine.clear() resets the secret flag. Capture it before
        // processing Enter so passwords never reach xterm's scrollback.
        const secret = line.hidden;
        const before = line.display;
        const submissions = line.input(value);
        if (submissions.length > 0) {
          const submitted = submissions[0] ?? "";
          term.write(`\x1b8\x1b[J${secret ? "" : submitted}\r\n`);
          try {
            currentSocket.send(
              JSON.stringify({ type: "line", text: submitted }),
            );
          } catch {
            // The close event owns retry policy. Do not replay this line on a
            // replacement connection after a send race.
          }
          term.scrollToBottom();
          queueFocus();
        } else if (line.display !== before) {
          term.write(`\x1b8\x1b[J${line.display}`);
          queueFocus();
        }
      });

      term.writeln("무한대전 · 터미널 접속\r\n");
      syncMobileViewport();
      fit.fit();
      queueFocus();

      let address: URL | undefined;
      const gateway = validateGatewayUrl(url, location.protocol);
      if (gateway.kind === "missing") {
        term.writeln("게임 서버 주소가 설정되지 않았습니다.");
      } else if (gateway.kind === "valid") {
        address = gateway.url;
      } else {
        term.writeln("게임 서버 주소를 확인하십시오.");
      }

      const scheduleReconnect = () => {
        if (disposed || permanentlyClosed || reconnectTimer !== undefined) {
          return;
        }
        reconnectAttempt += 1;
        const delay = reconnectDelay(reconnectAttempt);
        term.writeln(
          `\r\n접속이 잠시 끊겼습니다. ${Math.ceil(delay / 1000)}초 뒤 다시 연결합니다.`,
        );
        reconnectTimer = window.setTimeout(() => {
          reconnectTimer = undefined;
          connect();
        }, delay);
        queueFocus();
      };

      function connect() {
        if (disposed || permanentlyClosed || !address) return;
        clearPendingInput();
        queueFocus();

        let currentSocket: WebSocket;
        try {
          currentSocket = new WebSocket(address);
        } catch {
          term.writeln("\r\n게임 서버에 연결하지 못했습니다.");
          return;
        }
        socket = currentSocket;

        currentSocket.addEventListener("open", () => {
          if (disposed || permanentlyClosed || socket !== currentSocket) {
            currentSocket.close(1000, "component disposed");
            return;
          }
          queueFocus();
        });

        currentSocket.addEventListener("message", (event) => {
          if (disposed || socket !== currentSocket) return;
          try {
            const value: unknown = JSON.parse(event.data);
            if (!isTerminalView(value)) throw new Error("invalid view");
            const view = value;
            line.clear();
            if (view.closed) permanentlyClosed = true;
            term.write(`${view.text}\x1b7`, () => {
              if (disposed || view.closed) return;
              line.prompt(view.secret);
              queueFocus();
            });
          } catch {
            permanentlyClosed = true;
            line.clear();
            term.writeln("\r\n게임 서버 응답 형식이 올바르지 않습니다.");
            currentSocket.close(1008, "invalid view");
          }
        });

        currentSocket.addEventListener("error", () => {
          if (!disposed && socket === currentSocket) {
            term.writeln("\r\n게임 서버와 통신하지 못했습니다.");
          }
        });

        currentSocket.addEventListener("close", (event) => {
          // A stale socket may close after a replacement connection exists.
          if (socket !== currentSocket) return;
          socket = undefined;
          clearPendingInput();
          if (disposed || permanentlyClosed) return;
          if (shouldReconnect(event.code, reconnectAttempt)) {
            scheduleReconnect();
            return;
          }
          term.writeln(
            "\r\n접속이 끝났습니다. 다시 접속하려면 페이지를 새로고침하십시오.",
          );
          queueFocus();
        });
      }

      cleanup = () => {
        if (reconnectTimer !== undefined) {
          window.clearTimeout(reconnectTimer);
          reconnectTimer = undefined;
        }
        if (resizeFrame !== undefined) {
          window.cancelAnimationFrame(resizeFrame);
          resizeFrame = undefined;
        }
        if (focusFrame !== undefined) {
          window.cancelAnimationFrame(focusFrame);
          focusFrame = undefined;
        }
        data.dispose();
        observer.disconnect();
        textarea?.removeEventListener("compositionstart", compositionStart);
        textarea?.removeEventListener("compositionend", compositionEnd);
        element.removeEventListener("pointerup", pointerUp);
        window.removeEventListener("focus", queueFocus);
        window.removeEventListener("resize", resize);
        window.visualViewport?.removeEventListener("resize", resize);
        window.visualViewport?.removeEventListener("scroll", resize);
        clearPendingInput();
        const activeSocket = socket;
        socket = undefined;
        activeSocket?.close(1000, "component disposed");
        fit.dispose();
        term.dispose();
      };

      if (address) connect();
    }

    void start();
    return () => {
      disposed = true;
      cleanup();
    };
  }, [url]);

  return (
    <main className={styles.desktop}>
      <div
        aria-label="무한대전 게임 터미널"
        className={styles.terminal}
        ref={host}
      />
    </main>
  );
}

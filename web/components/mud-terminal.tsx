"use client";

import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import {
  FormEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";

import {
  createGatewayAuthFrame,
  shouldReconnectGatewayClose,
} from "@/lib/gateway-contract";
import {
  canRestoreTerminalFocus,
  canSubmitMobileLine,
  getMobileViewportHeight,
  shouldDeferTerminalResize,
} from "@/lib/terminal-focus";

export type GatewayConnectionState =
  | "idle"
  | "connecting"
  | "authenticating"
  | "ready"
  | "provisioned"
  | "retrying"
  | "closed"
  | "error";

export interface GatewayStatus {
  state: GatewayConnectionState;
  detail: string;
  attempt: number;
}

interface MudTerminalProps {
  accessToken: string;
  characterId: string;
  gatewayUrl: string;
  onStatus: (status: GatewayStatus) => void;
  onTerminated?: () => void;
}

interface GatewayControl {
  type?: string;
  enabled?: boolean;
  status?: string;
  message?: string;
  reason?: string;
  code?: string;
}

const textEncoder = new TextEncoder();
const MAX_RECONNECT_DELAY_MS = 30_000;

function reconnectDelay(attempt: number): number {
  const exponential = Math.min(
    MAX_RECONNECT_DELAY_MS,
    750 * 2 ** Math.max(0, attempt - 1),
  );
  const jitter = Math.round(exponential * 0.15 * Math.random());
  return exponential + jitter;
}

function echoInput(terminal: Terminal, data: string): void {
  for (const character of data) {
    if (character === "\r" || character === "\n") {
      terminal.write("\r\n");
    } else if (character === "\b" || character === "\u007f") {
      terminal.write("\b \b");
    } else if (character === "\u001b") {
      // Arrow/function-key escape sequences are transport input, not local text.
      return;
    } else if (character.codePointAt(0)! >= 0x20) {
      terminal.write(character);
    }
  }
}

export function MudTerminal({
  accessToken,
  characterId,
  gatewayUrl,
  onStatus,
  onTerminated,
}: MudTerminalProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const sendInputRef = useRef<(data: string) => boolean>(() => false);
  const queueFocusRef = useRef<() => void>(() => {});
  const localEchoRef = useRef(true);
  const readyRef = useRef(false);
  const composingRef = useRef(false);
  const mobileComposingRef = useRef(false);
  const [mobileLine, setMobileLine] = useState("");
  const [ready, setReady] = useState(false);
  const [localEcho, setLocalEcho] = useState(true);

  const updateEcho = useCallback((enabled: boolean) => {
    localEchoRef.current = enabled;
    setLocalEcho(enabled);
  }, []);

  useEffect(() => {
    if (!containerRef.current) {
      return;
    }

    let disposed = false;

    const terminal = new Terminal({
      allowProposedApi: false,
      convertEol: false,
      cursorBlink: true,
      cursorStyle: "bar",
      fontFamily:
        '"SFMono-Regular", "Cascadia Code", "D2Coding", "Noto Sans Mono CJK KR", ui-monospace, monospace',
      fontSize: 15,
      letterSpacing: 0,
      lineHeight: 1.28,
      minimumContrastRatio: 7,
      rightClickSelectsWord: true,
      screenReaderMode: true,
      scrollback: 5_000,
      theme: {
        background: "#05090d",
        black: "#05090d",
        blue: "#6e9fbd",
        brightBlack: "#5d625d",
        brightBlue: "#8bb9d4",
        brightCyan: "#8bcfc0",
        brightGreen: "#92c7a6",
        brightMagenta: "#c4a2c8",
        brightRed: "#ed8270",
        brightWhite: "#f3efe1",
        brightYellow: "#ebc978",
        cursor: "#d7ae61",
        cursorAccent: "#05090d",
        cyan: "#75bda7",
        foreground: "#d8d4c4",
        green: "#75b58d",
        magenta: "#ac8eb0",
        red: "#d96b59",
        selectionBackground: "#d7ae6140",
        white: "#d8d4c4",
        yellow: "#d7ae61",
      },
    });
    const fitAddon = new FitAddon();

    terminal.loadAddon(fitAddon);
    terminal.open(containerRef.current);

    let resizeFrame: number | undefined;
    let resizePending = false;
    let focusFrame: number | undefined;

    const hasSelection = () =>
      terminal.hasSelection() || Boolean(window.getSelection()?.toString());

    const focusTerminal = () => {
      const activeElement = document.activeElement;
      if (
        !canRestoreTerminalFocus({
          disposed,
          composing: composingRef.current,
          hasSelection: hasSelection(),
          documentFocused: document.hasFocus(),
          activeElementOutsideTerminal:
            activeElement !== null &&
            activeElement !== document.body &&
            !containerRef.current?.contains(activeElement),
        })
      ) {
        return;
      }
      terminal.focus();
    };

    const queueFocus = () => {
      if (disposed || focusFrame !== undefined) return;
      focusFrame = window.requestAnimationFrame(() => {
        focusFrame = undefined;
        focusTerminal();
      });
    };
    queueFocusRef.current = queueFocus;

    const syncMobileViewport = () => {
      const viewportHeight = window.visualViewport?.height ?? window.innerHeight;
      const layout = containerRef.current?.parentElement;
      if (!layout) return;
      if (window.matchMedia("(max-width: 640px)").matches) {
        const height = getMobileViewportHeight(
          viewportHeight,
          layout.getBoundingClientRect().top,
        );
        if (height !== null) {
          layout.style.height = `${height}px`;
        }
      } else {
        layout.style.removeProperty("height");
      }
    };

    const applyResize = () => {
      resizeFrame = undefined;
      if (disposed) return;
      syncMobileViewport();
      if (shouldDeferTerminalResize(composingRef.current)) {
        resizePending = true;
        return;
      }
      fitAddon.fit();
      queueFocus();
    };

    const resize = () => {
      if (disposed) return;
      // Keep the visible area in sync with a mobile keyboard while deferring
      // xterm's row/column recalculation until a composition has committed.
      syncMobileViewport();
      if (shouldDeferTerminalResize(composingRef.current)) {
        resizePending = true;
        return;
      }
      if (resizeFrame !== undefined) return;
      resizeFrame = window.requestAnimationFrame(applyResize);
    };

    const compositionStart = () => {
      composingRef.current = true;
    };
    const compositionEnd = () => {
      composingRef.current = false;
      if (resizePending) {
        resizePending = false;
        resize();
      } else {
        queueFocus();
      }
    };
    const pointerUp = () => {
      if (!hasSelection()) queueFocus();
    };

    terminal.textarea?.addEventListener("compositionstart", compositionStart);
    terminal.textarea?.addEventListener("compositionend", compositionEnd);
    containerRef.current.addEventListener("pointerup", pointerUp);
    window.addEventListener("focus", queueFocus);
    window.addEventListener("resize", resize);
    window.visualViewport?.addEventListener("resize", resize);
    window.visualViewport?.addEventListener("scroll", resize);
    const resizeObserver = new ResizeObserver(resize);
    resizeObserver.observe(containerRef.current);

    terminal.attachCustomKeyEventHandler((event) => event.key !== "Tab");
    syncMobileViewport();
    fitAddon.fit();
    queueFocus();

    const dataDisposable = terminal.onData((data) => {
      sendInputRef.current(data);
    });
    terminalRef.current = terminal;
    fitAddonRef.current = fitAddon;

    return () => {
      disposed = true;
      if (resizeFrame !== undefined) {
        window.cancelAnimationFrame(resizeFrame);
        resizeFrame = undefined;
      }
      if (focusFrame !== undefined) {
        window.cancelAnimationFrame(focusFrame);
        focusFrame = undefined;
      }
      queueFocusRef.current = () => {};
      resizeObserver.disconnect();
      terminal.textarea?.removeEventListener("compositionstart", compositionStart);
      terminal.textarea?.removeEventListener("compositionend", compositionEnd);
      containerRef.current?.removeEventListener("pointerup", pointerUp);
      window.removeEventListener("focus", queueFocus);
      window.removeEventListener("resize", resize);
      window.visualViewport?.removeEventListener("resize", resize);
      window.visualViewport?.removeEventListener("scroll", resize);
      dataDisposable.dispose();
      fitAddon.dispose();
      terminal.dispose();
      terminalRef.current = null;
      fitAddonRef.current = null;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let attempt = 0;

    const publishStatus = (
      state: GatewayConnectionState,
      detail: string,
    ) => {
      if (!cancelled) {
        onStatus({ state, detail, attempt });
      }
    };

    const setNotReady = () => {
      readyRef.current = false;
      setReady(false);
      mobileComposingRef.current = false;
      // Retry/termination must not carry command text or a password forward.
      setMobileLine("");
      updateEcho(true);
    };

    const sendInput = (data: string): boolean => {
      const socket = socketRef.current;
      const terminal = terminalRef.current;

      if (!socket || socket.readyState !== WebSocket.OPEN || !readyRef.current) {
        return false;
      }

      socket.send(textEncoder.encode(data));
      if (localEchoRef.current && terminal) {
        echoInput(terminal, data);
      }
      return true;
    };
    sendInputRef.current = sendInput;

    const scheduleReconnect = (detail: string) => {
      attempt += 1;
      const delay = reconnectDelay(attempt);
      publishStatus(
        "retrying",
        `${detail} ${Math.ceil(delay / 1000)}초 뒤 다시 연결합니다.`,
      );
      reconnectTimer = setTimeout(connect, delay);
    };

    const handleControl = (
      control: GatewayControl,
      socket: WebSocket,
      markProtocolMalformed: () => void,
    ) => {
      switch (control.type) {
        case "ready":
          attempt = 0;
          readyRef.current = true;
          setReady(true);
          queueFocusRef.current();
          publishStatus("ready", "무한대전 세계와 연결됐습니다.");
          break;
        case "echo":
          updateEcho(control.enabled !== false);
          break;
        case "pong":
          break;
        case "error":
          // Error text is informational. The following close frame owns the
          // retry/termination decision and may be a transient 1011/12/13.
          publishStatus(
            "error",
            control.message ??
              control.reason ??
              control.code ??
              "게이트웨이가 연결을 거절했습니다.",
          );
          break;
        case "closed":
          publishStatus(
            "closed",
            control.message ?? control.reason ?? "게이트웨이 연결이 닫혔습니다.",
          );
          break;
        default:
          markProtocolMalformed();
          publishStatus("error", "게이트웨이 응답 형식이 올바르지 않습니다.");
          socket.close(1008, "invalid gateway control frame");
      }
    };

    function connect() {
      if (cancelled || !characterId) {
        return;
      }

      setNotReady();
      publishStatus(
        attempt > 0 ? "retrying" : "connecting",
        attempt > 0 ? "통로를 다시 찾고 있습니다." : "암호화 통로를 여는 중입니다.",
      );

      let socket: WebSocket;
      try {
        socket = new WebSocket(gatewayUrl, "muhan.v1");
      } catch {
        publishStatus("error", "게이트웨이 주소가 올바르지 않습니다.");
        return;
      }

      socket.binaryType = "arraybuffer";
      socketRef.current = socket;
      let protocolMalformed = false;
      const markProtocolMalformed = () => {
        protocolMalformed = true;
      };

      socket.addEventListener("open", () => {
        if (cancelled) {
          socket.close(1000, "component disposed");
          return;
        }
        publishStatus("authenticating", "입장권을 확인하는 중입니다.");
        socket.send(JSON.stringify(createGatewayAuthFrame(accessToken, characterId)));
      });

      socket.addEventListener("message", (event) => {
        if (typeof event.data === "string") {
          try {
            handleControl(
              JSON.parse(event.data) as GatewayControl,
              socket,
              markProtocolMalformed,
            );
          } catch {
            markProtocolMalformed();
            publishStatus("error", "알 수 없는 게이트웨이 응답을 받았습니다.");
            socket.close(1008, "malformed gateway control frame");
          }
          return;
        }

        if (event.data instanceof ArrayBuffer) {
          if (!readyRef.current) {
            markProtocolMalformed();
            publishStatus("error", "캐릭터 입장 확인 전 데이터가 도착했습니다.");
            socket.close(1008, "data before auth acknowledgement");
            return;
          }
          terminalRef.current?.write(new Uint8Array(event.data));
          return;
        }

        markProtocolMalformed();
        publishStatus("error", "게이트웨이 프레임 형식이 올바르지 않습니다.");
        socket.close(1008, "malformed gateway frame");
      });

      socket.addEventListener("error", () => {
        if (!cancelled) {
          publishStatus("error", "게이트웨이 통신 오류가 발생했습니다.");
        }
      });

      socket.addEventListener("close", (event) => {
        // An older socket can close after a reconnect has replaced it.
        if (socketRef.current !== socket) return;
        socketRef.current = null;
        setNotReady();

        if (cancelled) {
          return;
        }

        const reason = event.reason || `연결이 닫혔습니다 (${event.code}).`;
        if (!shouldReconnectGatewayClose(event.code, attempt, protocolMalformed)) {
          publishStatus("closed", reason);
          onTerminated?.();
          return;
        }
        scheduleReconnect(reason);
      });
    }

    connect();

    return () => {
      cancelled = true;
      if (reconnectTimer) {
        clearTimeout(reconnectTimer);
      }
      sendInputRef.current = () => false;
      const socket = socketRef.current;
      socketRef.current = null;
      if (socket && socket.readyState < WebSocket.CLOSING) {
        socket.close(1000, "session changed");
      }
    };
  }, [accessToken, characterId, gatewayUrl, onStatus, onTerminated, updateEcho]);

  const submitMobileLine = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (
      !canSubmitMobileLine({
        value: mobileLine,
        ready,
        composing: mobileComposingRef.current,
      }) ||
      !sendInputRef.current(`${mobileLine}\n`)
    ) {
      return;
    }
    setMobileLine("");
    terminalRef.current?.focus();
  };

  return (
    <div className="terminal-stack">
      <div
        aria-label="무한대전 터미널"
        className="terminal-viewport"
        onPointerDown={() => terminalRef.current?.focus()}
        ref={containerRef}
      />
      <form className="command-bar" onSubmit={submitMobileLine}>
        <label htmlFor="mud-command">
          {localEcho ? "명령" : "비밀번호"}
        </label>
        <input
          aria-label={localEcho ? "명령 입력" : "게임 비밀번호 입력"}
          autoCapitalize="none"
          autoComplete="off"
          autoCorrect="off"
          disabled={!ready}
          enterKeyHint="send"
          id="mud-command"
          inputMode="text"
          onChange={(event) => setMobileLine(event.target.value)}
          onCompositionEnd={() => {
            mobileComposingRef.current = false;
          }}
          onCompositionStart={() => {
            mobileComposingRef.current = true;
          }}
          onKeyDown={(event) => {
            if (
              event.key === "Enter" &&
              (mobileComposingRef.current || event.nativeEvent.isComposing)
            ) {
              event.preventDefault();
            }
          }}
          placeholder={ready ? "명령을 입력하세요" : "세계 연결을 기다리는 중"}
          spellCheck={false}
          type={localEcho ? "text" : "password"}
          value={mobileLine}
        />
        <button disabled={!ready || !mobileLine} type="submit">
          보내기
        </button>
      </form>
    </div>
  );
}

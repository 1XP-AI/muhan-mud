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

export type GatewayConnectionState =
  | "idle"
  | "connecting"
  | "authenticating"
  | "ready"
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
}: MudTerminalProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const sendInputRef = useRef<(data: string) => boolean>(() => false);
  const localEchoRef = useRef(true);
  const readyRef = useRef(false);
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
    fitAddon.fit();
    terminal.focus();

    const dataDisposable = terminal.onData((data) => {
      sendInputRef.current(data);
    });
    const resizeObserver = new ResizeObserver(() => {
      window.requestAnimationFrame(() => fitAddon.fit());
    });
    resizeObserver.observe(containerRef.current);

    terminalRef.current = terminal;
    fitAddonRef.current = fitAddon;

    return () => {
      resizeObserver.disconnect();
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
    let terminalFailure: string | null = null;

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

    const handleControl = (control: GatewayControl) => {
      switch (control.type) {
        case "ready":
          attempt = 0;
          readyRef.current = true;
          setReady(true);
          publishStatus("ready", "무한대전 세계와 연결됐습니다.");
          break;
        case "echo":
          updateEcho(control.enabled !== false);
          break;
        case "pong":
          break;
        case "error":
          terminalFailure =
            control.message ??
            control.reason ??
            control.code ??
            "게이트웨이가 연결을 거절했습니다.";
          publishStatus("error", terminalFailure);
          break;
        case "closed":
          publishStatus(
            "closed",
            control.message ?? control.reason ?? "게이트웨이 연결이 닫혔습니다.",
          );
          break;
        default:
          if (!readyRef.current) {
            terminalFailure = "입장 확인 형식이 올바르지 않습니다.";
            publishStatus("error", terminalFailure);
            socketRef.current?.close(1008, "invalid admission acknowledgement");
          }
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
            handleControl(JSON.parse(event.data) as GatewayControl);
          } catch {
            publishStatus("error", "알 수 없는 게이트웨이 응답을 받았습니다.");
          }
          return;
        }

        if (event.data instanceof ArrayBuffer) {
          if (!readyRef.current) {
            terminalFailure = "캐릭터 입장 확인 전 데이터가 도착했습니다.";
            publishStatus("error", terminalFailure);
            socket.close(1008, "data before auth acknowledgement");
            return;
          }
          terminalRef.current?.write(new Uint8Array(event.data));
        }
      });

      socket.addEventListener("error", () => {
        if (!cancelled) {
          publishStatus("error", "게이트웨이 통신 오류가 발생했습니다.");
        }
      });

      socket.addEventListener("close", (event) => {
        if (socketRef.current === socket) {
          socketRef.current = null;
        }
        setNotReady();

        if (cancelled) {
          return;
        }

        if (terminalFailure) {
          publishStatus("error", terminalFailure);
          return;
        }

        const reason = event.reason || `연결이 닫혔습니다 (${event.code}).`;
        if (!shouldReconnectGatewayClose(event.code)) {
          publishStatus("closed", reason);
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
  }, [accessToken, characterId, gatewayUrl, onStatus, updateEcho]);

  const submitMobileLine = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!mobileLine || !sendInputRef.current(`${mobileLine}\n`)) {
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
          autoCapitalize="none"
          autoComplete="off"
          disabled={!ready}
          id="mud-command"
          onChange={(event) => setMobileLine(event.target.value)}
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

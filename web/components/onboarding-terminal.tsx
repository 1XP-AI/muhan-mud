"use client";

import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import { FormEvent, useCallback, useEffect, useRef, useState } from "react";

import type { GatewayStatus } from "@/components/mud-terminal";
import {
  canRestoreTerminalFocus,
  getMobileViewportHeight,
  shouldDeferTerminalResize,
} from "@/lib/terminal-focus";
import {
  createOnboardingAuthFrame,
  createOnboardingSocketContract,
  createOnboardingSocketUrl,
  decideOnboardingClose,
  decideOnboardingControl,
  recoveryFromOnboardingClose,
  type OnboardingRecovery,
  type OnboardingLifecyclePhase,
  type OnboardingMode,
} from "@/lib/onboarding-contract";
import { LEGACY_GAME_PASSWORD_DISCLOSURE } from "@/lib/onboarding-disclosure";

interface OnboardingTerminalProps {
  accessToken: string;
  gatewayUrl: string;
  mode: OnboardingMode;
  correlationId: string;
  onStatus: (status: GatewayStatus) => void;
  onCancel: () => void;
  onProvisioned: (characterId: string) => void;
  onTerminated: (recovery?: OnboardingRecovery) => void;
  onClaimed: (characterId: string) => void;
}

const textEncoder = new TextEncoder();
const MAX_RECONNECT_DELAY_MS = 30_000;

function reconnectDelay(attempt: number): number {
  return Math.min(MAX_RECONNECT_DELAY_MS, 750 * 2 ** Math.max(0, attempt - 1));
}

export function OnboardingTerminal({
  accessToken,
  gatewayUrl,
  mode,
  correlationId,
  onStatus,
  onCancel,
  onProvisioned,
  onTerminated,
  onClaimed,
}: OnboardingTerminalProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const sendInputRef = useRef<(data: string) => boolean>(() => false);
  const queueFocusRef = useRef<() => void>(() => {});
  const readyRef = useRef(false);
  const settledRef = useRef(false);
  const phaseRef = useRef<OnboardingLifecyclePhase>("connecting");
  const composingRef = useRef(false);
  const terminalComposingRef = useRef(false);
  const echoRef = useRef(true);
  const [mobileLine, setMobileLine] = useState("");
  const [ready, setReady] = useState(false);
  const [echo, setEcho] = useState(true);

  const updateEcho = useCallback((enabled: boolean) => {
    if (echoRef.current !== enabled) {
      // A password must never survive the transition back to visible input;
      // clearing on both directions also prevents stale normal input from
      // being submitted as a password after Telnet echo changes.
      setMobileLine("");
    }
    echoRef.current = enabled;
    setEcho(enabled);
  }, []);

  useEffect(() => {
    if (!containerRef.current) return;

    let disposed = false;

    const terminal = new Terminal({
      allowProposedApi: false,
      convertEol: false,
      cursorBlink: true,
      cursorStyle: "bar",
      fontFamily:
        '"SFMono-Regular", "Cascadia Code", "D2Coding", "Noto Sans Mono CJK KR", ui-monospace, monospace',
      fontSize: 15,
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
    const isComposing = () =>
      composingRef.current || terminalComposingRef.current;

    const hasSelection = () =>
      terminal.hasSelection() || Boolean(window.getSelection()?.toString());

    const focusTerminal = () => {
      const activeElement = document.activeElement;
      if (
        !canRestoreTerminalFocus({
          disposed,
          composing: isComposing(),
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
      if (shouldDeferTerminalResize(isComposing())) {
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
      if (shouldDeferTerminalResize(isComposing())) {
        resizePending = true;
        return;
      }
      if (resizeFrame !== undefined) return;
      resizeFrame = window.requestAnimationFrame(applyResize);
    };

    const compositionStart = () => {
      terminalComposingRef.current = true;
    };
    const compositionEnd = () => {
      terminalComposingRef.current = false;
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
      // Before onboarding-ready, xterm input is deliberately discarded.
      sendInputRef.current(data);
    });
    terminalRef.current = terminal;

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
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let attempt = 0;
    let failed = false;
    let pendingRecovery: OnboardingRecovery | undefined;

    const publishStatus = (state: GatewayStatus["state"], detail: string) => {
      if (!cancelled) onStatus({ state, detail, attempt });
    };

    const clearInputState = () => {
      readyRef.current = false;
      setReady(false);
      composingRef.current = false;
      setMobileLine("");
      updateEcho(true);
    };

    const setNotReady = () => {
      clearInputState();
      phaseRef.current = "connecting";
    };

    const sendInput = (data: string): boolean => {
      const socket = socketRef.current;
      if (
        !readyRef.current ||
        !socket ||
        socket.readyState !== WebSocket.OPEN
      ) {
        return false;
      }
      // Onboarding always relies on C output for echo; this prevents password
      // keystrokes from being rendered locally in xterm.
      socket.send(textEncoder.encode(data));
      return true;
    };
    sendInputRef.current = sendInput;

    const scheduleReconnect = () => {
      attempt += 1;
      const delay = reconnectDelay(attempt);
      publishStatus(
        "retrying",
        `연결이 잠시 끊겼습니다. ${Math.ceil(delay / 1000)}초 뒤 다시 연결합니다.`,
      );
      reconnectTimer = setTimeout(connect, delay);
    };

    const handleControl = (value: unknown) => {
      if (cancelled || settledRef.current) return;
      const decision = decideOnboardingControl(mode, value, phaseRef.current);
      switch (decision.kind) {
        case "ready":
          phaseRef.current = "ready";
          readyRef.current = true;
          setReady(true);
          queueFocusRef.current();
          publishStatus("ready", "온보딩 통로가 준비됐습니다.");
          break;
        case "echo": {
          updateEcho(decision.enabled);
          break;
        }
        case "provisioned": {
          // Provisioning has committed its C-side wizard transaction. Return
          // to the refreshed roster for the normal Gateway admission socket.
          phaseRef.current = "provisioned";
          settledRef.current = true;
          readyRef.current = true;
          setReady(true);
          setMobileLine("");
          updateEcho(true);
          publishStatus("provisioned", "새 캐릭터가 활성화됐습니다.");
          onProvisioned(decision.characterId);
          break;
        }
        case "error":
          // Gateway reasons and C output are not safe display material. Keep
          // only the allowlisted recovery category until the close decides
          // whether this flow retries or returns to the roster.
          clearInputState();
          pendingRecovery = decision.recovery;
          publishStatus("error", decision.recovery.detail);
          break;
        case "claimed": {
          phaseRef.current = "ready";
          settledRef.current = true;
          clearInputState();
          onClaimed(decision.characterId);
          break;
        }
        case "terminated":
          settledRef.current = true;
          readyRef.current = false;
          setReady(false);
          setMobileLine("");
          updateEcho(true);
          onTerminated();
          break;
        case "failure":
          phaseRef.current = "failed";
          failed = true;
          clearInputState();
          pendingRecovery = recoveryFromOnboardingClose(1008);
          publishStatus("error", pendingRecovery.detail);
          socketRef.current?.close(1008, "onboarding failed");
          break;
        case "ignore":
          break;
      }
    };

    function connect() {
      if (cancelled || settledRef.current) return;
      setNotReady();
      const contract = createOnboardingSocketContract("empty", mode);
      if (!contract) {
        pendingRecovery = recoveryFromOnboardingClose(1008);
        publishStatus("error", pendingRecovery.detail);
        return;
      }
      publishStatus(
        attempt > 0 ? "retrying" : "connecting",
        attempt > 0 ? "온보딩 통로를 다시 찾고 있습니다." : "온보딩 통로를 여는 중입니다.",
      );

      let socket: WebSocket;
      try {
        socket = new WebSocket(createOnboardingSocketUrl(gatewayUrl), contract.subprotocol);
      } catch {
        failed = true;
        phaseRef.current = "failed";
        pendingRecovery = recoveryFromOnboardingClose(1008);
        publishStatus("error", pendingRecovery.detail);
        settledRef.current = true;
        onTerminated(pendingRecovery);
        return;
      }
      socket.binaryType = "arraybuffer";
      socketRef.current = socket;

      socket.addEventListener("open", () => {
        if (cancelled || settledRef.current) {
          socket.close(1000, "onboarding cancelled");
          return;
        }
        publishStatus("authenticating", "온보딩 권한을 확인하는 중입니다.");
        socket.send(JSON.stringify(createOnboardingAuthFrame(accessToken, mode, correlationId)));
      });

      socket.addEventListener("message", (event) => {
        if (typeof event.data === "string") {
          try {
            handleControl(JSON.parse(event.data));
          } catch {
            failed = true;
            clearInputState();
            pendingRecovery = recoveryFromOnboardingClose(1008);
            publishStatus("error", pendingRecovery.detail);
            socket.close(1008, "invalid onboarding response");
          }
          return;
        }
        if (event.data instanceof ArrayBuffer) {
          if (!readyRef.current) {
            failed = true;
            clearInputState();
            pendingRecovery = recoveryFromOnboardingClose(1008);
            publishStatus("error", pendingRecovery.detail);
            socket.close(1008, "onboarding data before ready");
            return;
          }
          terminalRef.current?.write(new Uint8Array(event.data));
          return;
        }

        failed = true;
        clearInputState();
        pendingRecovery = recoveryFromOnboardingClose(1008);
        publishStatus("error", pendingRecovery.detail);
        socket.close(1008, "malformed onboarding frame");
      });

      socket.addEventListener("error", () => {
        if (!cancelled) {
          clearInputState();
          pendingRecovery = recoveryFromOnboardingClose(1011);
          publishStatus("error", pendingRecovery.detail);
        }
      });

      socket.addEventListener("close", (event) => {
        // An older socket can close after a reconnect has already replaced it.
        // It must not reset readiness or schedule a second reconnect.
        if (socketRef.current !== socket) return;
        socketRef.current = null;
        const closedPhase = phaseRef.current;
        setNotReady();
        if (cancelled || settledRef.current) return;
        if (decideOnboardingClose(closedPhase, event.code, attempt, failed) === "terminate") {
          settledRef.current = true;
          onTerminated(pendingRecovery ?? recoveryFromOnboardingClose(event.code));
          return;
        }
        scheduleReconnect();
      });
    }

    connect();
    return () => {
      cancelled = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      sendInputRef.current = () => false;
      const socket = socketRef.current;
      socketRef.current = null;
      if (socket && socket.readyState < WebSocket.CLOSING) {
        socket.close(1000, "onboarding cancelled");
      }
    };
  }, [accessToken, correlationId, gatewayUrl, mode, onClaimed, onProvisioned, onStatus, onTerminated, updateEcho]);

  const cancelOnboarding = () => {
    // Clear visible input and password mode before the parent unmounts us.
    setMobileLine("");
    updateEcho(true);
    onCancel();
  };

  const submitMobileLine = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    // The legacy character-creation confirmation explicitly requires a blank
    // [enter]. Readiness and IME composition are the only submission gates:
    // an empty line remains valid terminal input.
    if (composingRef.current) return;
    if (sendInputRef.current(`${mobileLine}\n`)) {
      setMobileLine("");
      terminalRef.current?.focus();
    }
  };

  return (
    <div className="onboarding-terminal">
      <div className="onboarding-heading">
        <div>
          <p className="eyebrow">{mode === "provision" ? "NEW CHARACTER" : "EXISTING MUD VERIFICATION"}</p>
          <h2>{mode === "provision" ? "새 캐릭터 만들기" : "기존 캐릭터 연결 확인"}</h2>
        </div>
        <button className="secondary-action" onClick={cancelOnboarding} type="button">
          취소하고 캐릭터 선택
        </button>
      </div>
      <p className="onboarding-hint" aria-live="polite">
        {mode === "claim"
          ? "기존 MUD 확인 절차는 이 xterm 안에서 끝까지 진행해야 합니다. 확인이 끝나고 활성 목록에 나타난 캐릭터만 플레이할 수 있습니다."
          : "온보딩 절차를 터미널에서 진행합니다. 입력 안내가 나타날 때까지 잠시 기다려 주세요."}
      </p>
      {mode === "provision" ? (
        <p className="onboarding-hint">{LEGACY_GAME_PASSWORD_DISCLOSURE}</p>
      ) : null}
      <div
        aria-label="캐릭터 온보딩 터미널"
        className="terminal-viewport onboarding-viewport"
        data-onboarding-ready={ready ? "true" : "false"}
        onPointerDown={() => terminalRef.current?.focus()}
        ref={containerRef}
      />
      <form className="command-bar" onSubmit={submitMobileLine}>
        <label htmlFor="onboarding-command">{echo ? "입력" : "게임 비밀번호"}</label>
        <input
          aria-label={echo ? "온보딩 입력" : "게임 비밀번호 입력"}
          autoCapitalize="none"
          autoComplete="off"
          autoCorrect="off"
          disabled={!ready}
          enterKeyHint="send"
          id="onboarding-command"
          inputMode="text"
          onChange={(event) => setMobileLine(event.target.value)}
          onCompositionEnd={() => {
            composingRef.current = false;
          }}
          onCompositionStart={() => {
            composingRef.current = true;
          }}
          onKeyDown={(event) => {
            if (
              event.key === "Enter" &&
              (composingRef.current || event.nativeEvent.isComposing)
            ) {
              event.preventDefault();
            }
          }}
          placeholder={ready ? "터미널 입력을 보내세요" : "온보딩 통로를 기다리는 중"}
          spellCheck={false}
          type={echo ? "text" : "password"}
          value={mobileLine}
        />
        <button disabled={!ready} type="submit">
          보내기
        </button>
      </form>
    </div>
  );
}

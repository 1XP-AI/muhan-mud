export interface GatewayAuthFrame {
  type: "auth";
  accessToken: string;
  characterId: string;
}

export const MAX_GATEWAY_RECONNECT_ATTEMPTS = 3;

export type GatewayLifecyclePhase =
  | "connecting"
  | "authenticating"
  | "ready"
  | "retrying"
  | "closed";

export interface GatewayLifecycleState {
  phase: GatewayLifecyclePhase;
  attempt: number;
  failed: boolean;
  error: string | null;
}

export type GatewayLifecycleEvent =
  | { type: "error-frame"; detail: string }
  | { type: "close"; code: number; protocolMalformed?: boolean };

export type GatewayLifecycleAction = "show-error" | "retry" | "terminate";

export interface GatewayLifecycleResult {
  action: GatewayLifecycleAction;
  state: GatewayLifecycleState;
}

export function createGatewayLifecycleState(): GatewayLifecycleState {
  return { phase: "connecting", attempt: 0, failed: false, error: null };
}

export function createGatewayAuthFrame(
  accessToken: string,
  characterId: string,
): GatewayAuthFrame {
  return { type: "auth", accessToken, characterId };
}

export function shouldOpenGatewaySocket(
  rosterStatus: "loading" | "error" | "empty" | "ready",
  characterId: string | null,
  ownedCharacters: readonly { id: string; lifecycle: string }[],
): boolean {
  return (
    rosterStatus === "ready" &&
    characterId !== null &&
    ownedCharacters.some(
      (character) => character.id === characterId && character.lifecycle === "active",
    )
  );
}

export function shouldReconnectGatewayClose(
  code: number,
  attempt = 0,
  protocolMalformed = false,
): boolean {
  if (protocolMalformed || attempt >= MAX_GATEWAY_RECONNECT_ATTEMPTS) return false;
  return !(
    code === 1000 ||
    code === 1002 ||
    code === 1003 ||
    code === 1007 ||
    code === 1008 ||
    code === 1009 ||
    code === 1010 ||
    code === 4001 ||
    (code >= 4400 && code < 4500)
  );
}

export function decideGatewayClose(
  code: number,
  attempt: number,
  protocolMalformed = false,
): "retry" | "terminate" {
  return shouldReconnectGatewayClose(code, attempt, protocolMalformed)
    ? "retry"
    : "terminate";
}

/** Keep a text error visible while deferring retry/termination to the close frame. */
export function reduceGatewayLifecycle(
  state: GatewayLifecycleState,
  event: GatewayLifecycleEvent,
): GatewayLifecycleResult {
  if (event.type === "error-frame") {
    return {
      action: "show-error",
      state: { ...state, error: event.detail, failed: false },
    };
  }

  const action = decideGatewayClose(
    event.code,
    state.attempt,
    event.protocolMalformed,
  );
  return {
    action,
    state: {
      ...state,
      phase: action === "retry" ? "retrying" : "closed",
      attempt: action === "retry" ? state.attempt + 1 : state.attempt,
      error: state.error,
      failed: false,
    },
  };
}

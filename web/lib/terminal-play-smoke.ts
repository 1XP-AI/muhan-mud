import {
  createGatewayLifecycleState,
  reduceGatewayLifecycle,
  shouldReconnectGatewayClose,
  type GatewayLifecycleResult,
  type GatewayLifecycleState,
} from "./gateway-contract.ts";
import {
  canRestoreTerminalFocus,
  canSubmitMobileLine,
  shouldDeferTerminalSubmission,
  shouldDeferTerminalResize,
} from "./terminal-focus.ts";
import { validateGatewayUrl, type GatewayUrlValidation } from "./gateway-url.ts";
import { TerminalLine } from "./terminal-line.ts";

/**
 * Contract-only line sent by the terminal. The real browser sends this JSON
 * object over WebSocket after a complete line has been entered.
 */
export interface TerminalLineFrame {
  type: "line";
  text: string;
}

/**
 * Contract-only view received from the Go gateway. Every view carries both
 * flags so a secret prompt and a terminal close cannot be inferred from text.
 */
export interface TerminalViewFrame {
  type: "view";
  text: string;
  secret: boolean;
  closed: boolean;
}

const LINE_FRAME_KEYS = ["text", "type"] as const;
const VIEW_FRAME_KEYS = ["closed", "secret", "text", "type"] as const;
const MAX_LINE_BYTES = 512;
const MAX_PASSWORD_BYTES = 14;
const MIN_PASSWORD_BYTES = 3;
const textEncoder = new TextEncoder();

/** The deterministic inputs used by the in-memory signup/relogin scenario. */
export interface TerminalPlayScenario {
  gatewayUrl: string;
  pageProtocol: string;
  name: string;
  password: string;
  creationLines: readonly string[];
  firstCommand: string;
}

export const DEFAULT_TERMINAL_PLAY_SCENARIO: TerminalPlayScenario = Object.freeze({
  gatewayUrl: "ws://mud.test/game",
  pageProtocol: "http:",
  name: "Smokehero",
  password: "smoke-password",
  creationLines: Object.freeze(["남", "4", "12 10 12 10 10", "1", "선", "7"]),
  firstCommand: "봐",
});

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function hasExactKeys(value: Record<string, unknown>, keys: readonly string[]): boolean {
  const actual = Object.keys(value).sort();
  const expected = [...keys].sort();
  return actual.length === expected.length && actual.every((key, index) => key === expected[index]);
}

/** WebSocket's UTF-8 encoder rejects lone UTF-16 surrogates; mirror that here. */
function isUnicodeScalarString(value: string): boolean {
  for (let index = 0; index < value.length; index += 1) {
    const codeUnit = value.charCodeAt(index);
    if (codeUnit >= 0xd800 && codeUnit <= 0xdbff) {
      const next = value.charCodeAt(index + 1);
      if (next < 0xdc00 || next > 0xdfff) return false;
      index += 1;
    } else if (codeUnit >= 0xdc00 && codeUnit <= 0xdfff) {
      return false;
    }
  }
  return true;
}

function isLineText(value: unknown): value is string {
  return (
    typeof value === "string" &&
    isUnicodeScalarString(value) &&
    textEncoder.encode(value).byteLength <= MAX_LINE_BYTES &&
    !/[\r\n\u0000]/u.test(value)
  );
}

/** Validate the exact client line object without contacting a socket. */
export function isTerminalLineFrame(value: unknown): value is TerminalLineFrame {
  if (!isRecord(value) || !hasExactKeys(value, LINE_FRAME_KEYS)) return false;
  return value.type === "line" && isLineText(value.text);
}

/** Parse a client line frame; malformed or unsafe input is rejected. */
export function parseTerminalLineFrame(value: unknown): TerminalLineFrame | null {
  return isTerminalLineFrame(value) ? { type: "line", text: value.text } : null;
}

/** Create a canonical line frame, throwing before a malformed frame is sent. */
export function createTerminalLineFrame(text: string): TerminalLineFrame {
  const frame: unknown = { type: "line", text };
  if (!isTerminalLineFrame(frame)) throw new RangeError("invalid terminal line");
  return frame;
}

/** Validate the exact server view object used by the terminal-only page. */
export function assertTerminalView(value: unknown): value is TerminalViewFrame {
  if (!isRecord(value) || !hasExactKeys(value, VIEW_FRAME_KEYS)) return false;
  return (
    value.type === "view" &&
    typeof value.text === "string" &&
    isUnicodeScalarString(value.text) &&
    typeof value.secret === "boolean" &&
    typeof value.closed === "boolean"
  );
}

/** Alias with parser wording for callers that do not use assertion-style names. */
export function parseTerminalView(value: unknown): TerminalViewFrame | null {
  return assertTerminalView(value) ? { ...value } : null;
}

/** Inspect the runtime coordinate without opening a browser or WebSocket. */
export function inspectTerminalGateway(
  value: string | null | undefined,
  pageProtocol: string,
):
  | { kind: "ready"; canOpen: true; url: string }
  | { kind: "missing"; canOpen: false }
  | { kind: "invalid"; canOpen: false }
  | { kind: "mixed-content"; canOpen: false } {
  const result: GatewayUrlValidation = validateGatewayUrl(value, pageProtocol);
  if (result.kind === "valid") {
    return { kind: "ready", canOpen: true, url: result.url.toString() };
  }
  return { kind: result.kind, canOpen: false };
}

function canonicalName(value: string): string | null {
  const normalized = value.normalize("NFC");
  if (
    normalized.length === 0 ||
    normalized === "." ||
    normalized === ".." ||
    normalized.length > 14 ||
    /[\u0000-\u001f\u007f\\/:]/u.test(normalized)
  ) {
    return null;
  }
  let canonical = normalized.replace(/[A-Z]/g, (letter) => letter.toLowerCase());
  if (/^[a-z]/u.test(canonical)) {
    canonical = canonical[0]!.toUpperCase() + canonical.slice(1);
  }
  return canonical;
}

function isAcceptedPassword(value: string): boolean {
  const length = textEncoder.encode(value).byteLength;
  return (
    isUnicodeScalarString(value) &&
    length >= MIN_PASSWORD_BYTES &&
    length <= MAX_PASSWORD_BYTES &&
    !/[\r\n\u0000]/u.test(value)
  );
}

interface MemoryAccount {
  readonly name: string;
  readonly password: string;
  readonly characterId: string;
  readonly creationLines: readonly string[];
}

export interface TerminalClientTrace {
  readonly type: "line";
  readonly text: string;
  readonly secret: boolean;
}

export type TerminalPlayPhase =
  | "disconnected"
  | "name"
  | "confirm"
  | "enter"
  | "creation"
  | "new-password"
  | "password"
  | "world"
  | "closed";

export interface TerminalPlaySmokeHarnessOptions {
  gatewayUrl?: string | null;
  pageProtocol?: string;
  scenario?: Partial<TerminalPlayScenario>;
}

/**
 * A deterministic, browser/DB-free WebSocket contract harness.
 *
 * It models only the terminal conversation boundary. Account and world state
 * live in an in-memory map, while all externally visible frames are validated
 * against the production wire shape. It is intentionally not an E2E client.
 */
export class TerminalPlaySmokeHarness {
  readonly scenario: TerminalPlayScenario;
  readonly gateway: ReturnType<typeof inspectTerminalGateway>;
  readonly serverViews: TerminalViewFrame[] = [];
  readonly clientTrace: TerminalClientTrace[] = [];
  readonly worldCommands: string[] = [];
  private readonly accounts = new Map<string, MemoryAccount>();

  connected = false;
  phase: TerminalPlayPhase = "disconnected";
  pendingInput = "";
  closeReason: string | null = null;
  clientLineCount = 0;
  reconnectAttempts = 0;
  createdCharacterId: string | null = null;
  restoredCharacterId: string | null = null;

  private readonly lifecycle: GatewayLifecycleState;
  private currentName = "";
  private creationIndex = 0;
  private passwordAttempts = 0;
  private reconnectPending = false;
  private protocolMalformed = false;
  private readonly creationValues: string[] = [];

  constructor(options: TerminalPlaySmokeHarnessOptions = {}) {
    const supplied = options.scenario ?? {};
    this.scenario = {
      ...DEFAULT_TERMINAL_PLAY_SCENARIO,
      ...supplied,
      creationLines: supplied.creationLines ?? DEFAULT_TERMINAL_PLAY_SCENARIO.creationLines,
    };
    const gatewayUrl = options.gatewayUrl === undefined ? this.scenario.gatewayUrl : options.gatewayUrl;
    const pageProtocol = options.pageProtocol ?? this.scenario.pageProtocol;
    this.gateway = inspectTerminalGateway(gatewayUrl, pageProtocol);
    this.lifecycle = createGatewayLifecycleState();
  }

  /** Open a fresh login conversation, or return null when the gateway is absent/invalid. */
  connect(): TerminalViewFrame | null {
    if (this.gateway.kind !== "ready" || this.phase === "closed") return null;
    this.connected = true;
    this.phase = "name";
    this.closeReason = null;
    this.protocolMalformed = false;
    this.lifecycle.phase = "ready";
    this.pendingInput = "";
    return this.emitView("당신의 이름은 무엇입니까? ", false, false);
  }

  /** Reopen only after a transient close accepted by the bounded retry policy. */
  reconnect(): TerminalViewFrame | null {
    if (!this.reconnectPending || this.phase === "closed") return null;
    this.reconnectPending = false;
    this.reconnectAttempts = this.lifecycle.attempt;
    this.currentName = "";
    this.creationIndex = 0;
    this.creationValues.length = 0;
    this.passwordAttempts = 0;
    this.pendingInput = "";
    this.connected = true;
    this.phase = "name";
    this.closeReason = null;
    this.lifecycle.phase = "ready";
    return this.emitView("당신의 이름은 무엇입니까? ", false, false);
  }

  /**
   * Submit one complete line. The harness never queues or replays a line after
   * a close, matching the terminal's one-prompt-at-a-time behavior.
   */
  sendLine(text: string): TerminalViewFrame | null {
    if (!this.connected || this.phase === "closed") return null;
    let frame: TerminalLineFrame;
    try {
      frame = createTerminalLineFrame(text);
    } catch {
      this.connected = false;
      this.phase = "closed";
      this.closeReason = "invalid-line";
      this.protocolMalformed = true;
      return null;
    }

    // A test trace is still a log: redact the fixture password even if a
    // malformed scenario submits it at the wrong prompt.
    const secret =
      this.phase === "password" ||
      this.phase === "new-password" ||
      text === this.scenario.password;
    this.clientLineCount += 1;
    this.clientTrace.push({ type: "line", text: secret ? "<secret>" : frame.text, secret });
    this.pendingInput = "";
    return this.processLine(frame.text);
  }

  /** Accept one untrusted incoming frame and fail closed on malformed data. */
  acceptServerFrame(value: unknown): TerminalViewFrame | null {
    const frame = parseTerminalView(value);
    if (frame === null) {
      this.connected = false;
      this.phase = "closed";
      this.closeReason = "invalid-message";
      this.protocolMalformed = true;
      this.reconnectPending = false;
      return null;
    }
    this.serverViews.push(frame);
    if (frame.closed) {
      this.connected = false;
      this.phase = "closed";
      this.closeReason = "server-closed";
      this.reconnectPending = false;
    }
    return { ...frame };
  }

  /** Apply the same close/retry decision used by the terminal contract. */
  close(code: number, protocolMalformed = false): GatewayLifecycleResult {
    const result = reduceGatewayLifecycle(this.lifecycle, {
      type: "close",
      code,
      protocolMalformed: protocolMalformed || this.protocolMalformed,
    });
    this.connected = false;
    this.pendingInput = "";
    this.reconnectPending = result.action === "retry";
    this.reconnectAttempts = result.state.attempt;
    this.lifecycle.phase = result.state.phase;
    this.lifecycle.attempt = result.state.attempt;
    this.lifecycle.failed = result.state.failed;
    this.lifecycle.error = result.state.error;
    if (result.action === "terminate") {
      this.phase = "closed";
      this.closeReason = this.protocolMalformed ? "invalid-message" : "terminated";
    } else {
      this.phase = "disconnected";
      this.closeReason = "transient-close";
    }
    return result;
  }

  get secretTraceContainsPassword(): boolean {
    return this.clientTrace.some((entry) => entry.text === this.scenario.password);
  }

  private processLine(line: string): TerminalViewFrame {
    switch (this.phase) {
      case "name":
        return this.submitName(line);
      case "confirm":
        if (line === "예" || line.toLowerCase() === "y") {
          this.phase = "enter";
          return this.emitView("[엔터]를 누르십시오.", false, false);
        }
        this.currentName = "";
        this.phase = "name";
        return this.emitView("당신의 이름은 무엇입니까? ", false, false);
      case "enter":
        if (line !== "") return this.emitView("[엔터]를 누르십시오.", false, false);
        this.phase = "creation";
        this.creationIndex = 0;
        this.creationValues.length = 0;
        return this.emitCreationPrompt();
      case "creation":
        return this.submitCreation(line);
      case "new-password":
        return this.submitNewPassword(line);
      case "password":
        return this.submitPassword(line);
      case "world":
        this.worldCommands.push(line);
        return this.emitView(line === this.scenario.firstCommand ? "광장\r\n" : "명령을 처리했습니다.\r\n", false, false);
      case "disconnected":
      case "closed":
        return this.emitView("접속을 끊습니다.\r\n", false, true);
    }
  }

  private submitName(line: string): TerminalViewFrame {
    const name = canonicalName(line);
    if (name === null) return this.emitView("이름을 다시 입력하십시오.\r\n당신의 이름은 무엇입니까? ", false, false);
    this.currentName = name;
    if (this.accounts.has(name)) {
      this.phase = "password";
      this.passwordAttempts = 0;
      return this.emitView("암호를 넣어 주십시오: ", true, false);
    }
    this.phase = "confirm";
    return this.emitView("이 이름으로 새 캐릭터를 만드시겠습니까(예/아니오)? ", false, false);
  }

  private emitCreationPrompt(): TerminalViewFrame {
    const prompts = [
      "당신은 남자입니까, 여자입니까(남자/여자)? ",
      "직업을 고르세요: ",
      ": ",
      ": ",
      "성향을 고르십시오(선함/악함): ",
      "종족을 고르십시오: ",
    ];
    return this.emitView(prompts[this.creationIndex] ?? "", false, false);
  }

  private submitCreation(line: string): TerminalViewFrame {
    if (line !== this.scenario.creationLines[this.creationIndex]) {
      return this.emitView("입력이 잘못되었습니다.\r\n" + this.currentCreationPrompt(), false, false);
    }
    this.creationValues.push(line);
    this.creationIndex += 1;
    if (this.creationIndex >= this.scenario.creationLines.length) {
      this.phase = "new-password";
      return this.emitView("새 암호를 넣으십시오(3~14바이트): ", true, false);
    }
    return this.emitCreationPrompt();
  }

  private currentCreationPrompt(): string {
    const prompts = [
      "당신은 남자입니까, 여자입니까(남자/여자)? ",
      "직업을 고르세요: ",
      ": ",
      ": ",
      "성향을 고르십시오(선함/악함): ",
      "종족을 고르십시오: ",
    ];
    return prompts[this.creationIndex] ?? "";
  }

  private submitNewPassword(line: string): TerminalViewFrame {
    if (!isAcceptedPassword(line)) {
      return this.emitView("새 암호를 다시 넣으십시오(3~14바이트): ", true, false);
    }
    const characterId = `character-${this.accounts.size + 1}`;
    this.accounts.set(this.currentName, {
      name: this.currentName,
      password: line,
      characterId,
      creationLines: [...this.creationValues],
    });
    this.createdCharacterId = characterId;
    this.phase = "world";
    return this.emitView("광장\r\n", false, false);
  }

  private submitPassword(line: string): TerminalViewFrame {
    const account = this.accounts.get(this.currentName);
    if (!account || account.password !== line) {
      this.passwordAttempts += 1;
      if (this.passwordAttempts >= 3 || line.length === 0) {
        this.connected = false;
        this.phase = "closed";
        return this.emitView("접속을 끊습니다.\r\n", false, true);
      }
      return this.emitView("암호가 틀립니다. 다시 입력하십시오: ", true, false);
    }
    this.restoredCharacterId = account.characterId;
    this.phase = "world";
    return this.emitView("광장\r\n", false, false);
  }

  private emitView(text: string, secret: boolean, closed: boolean): TerminalViewFrame {
    const frame: TerminalViewFrame = { type: "view", text, secret, closed };
    if (!assertTerminalView(frame)) throw new Error("harness emitted invalid view");
    this.serverViews.push(frame);
    if (closed) {
      this.connected = false;
      this.phase = "closed";
    }
    return { ...frame };
  }
}

export interface TerminalPlayFocusImeInvariantReport {
  readonly initialFocus: boolean;
  readonly selectionPreserved: boolean;
  readonly compositionDefersResize: boolean;
  readonly compositionEnterDefersSubmission: boolean;
  readonly compositionBlocksSubmit: boolean;
  readonly committedMobileLineSubmits: boolean;
  readonly koreanCodepointDeletesAsOne: boolean;
  readonly enterSubmitsOnce: boolean;
}

/** Exercise the pure focus/IME/input guards used by the xterm components. */
export function terminalPlayFocusImeInvariants(): TerminalPlayFocusImeInvariantReport {
  const line = new TerminalLine();
  line.prompt(false);
  line.input("가나\x7f");
  const koreanCodepointDeletesAsOne = line.display === "가";

  line.clear();
  line.prompt(false);
  const firstSubmit = line.input("봐\r");
  const secondSubmit = line.input("\r");

  const composingLine = new TerminalLine();
  composingLine.prompt(false);
  composingLine.input("한");
  const earlyEnter = shouldDeferTerminalSubmission("\r", true)
    ? []
    : composingLine.input("\r");
  const committedEnter = shouldDeferTerminalSubmission("\r", false)
    ? []
    : composingLine.input("\r");

  return {
    initialFocus: canRestoreTerminalFocus({
      disposed: false,
      composing: false,
      hasSelection: false,
      documentFocused: true,
      activeElementOutsideTerminal: false,
    }),
    selectionPreserved: !canRestoreTerminalFocus({
      disposed: false,
      composing: false,
      hasSelection: true,
      documentFocused: true,
      activeElementOutsideTerminal: false,
    }),
    compositionDefersResize: shouldDeferTerminalResize(true),
    compositionEnterDefersSubmission:
      earlyEnter.length === 0 &&
      committedEnter.length === 1 &&
      committedEnter[0] === "한",
    compositionBlocksSubmit: !canSubmitMobileLine({
      value: "봐",
      ready: true,
      composing: true,
    }),
    committedMobileLineSubmits: canSubmitMobileLine({
      value: "봐",
      ready: true,
      composing: false,
    }),
    koreanCodepointDeletesAsOne,
    enterSubmitsOnce: firstSubmit.length === 1 && firstSubmit[0] === "봐" && secondSubmit.length === 0,
  };
}

export interface TerminalPlaySmokeReport {
  readonly passed: boolean;
  readonly gateway: ReturnType<typeof inspectTerminalGateway>;
  readonly createdCharacterId: string | null;
  readonly restoredCharacterId: string | null;
  readonly worldCommands: readonly string[];
  readonly reconnect: {
    readonly action: "retry" | "terminate";
    readonly attempts: number;
    readonly reopened: boolean;
    readonly replayedPendingInput: boolean;
  };
  readonly secretTraceContainsPassword: boolean;
  readonly clientTrace: readonly TerminalClientTrace[];
  readonly serverViews: readonly TerminalViewFrame[];
  readonly focusIme: TerminalPlayFocusImeInvariantReport;
  readonly failures: readonly string[];
}

export interface RunTerminalPlaySmokeOptions extends TerminalPlaySmokeHarnessOptions {}

/**
 * Run the complete deterministic contract scenario without browser or DB IO.
 * A failed report describes a missing/invalid boundary; it never claims full
 * game functionality or live deployment coverage.
 */
export function runTerminalPlaySmoke(
  options: RunTerminalPlaySmokeOptions = {},
): TerminalPlaySmokeReport {
  const harness = new TerminalPlaySmokeHarness(options);
  const failures: string[] = [];
  const initial = harness.connect();

  if (initial === null) {
    failures.push("gateway-not-opened");
    return makeReport(harness, "terminate", 0, false, false, failures);
  }

  const scenario = harness.scenario;
  const signupLines = [scenario.name, "예", "", ...scenario.creationLines, scenario.password];
  for (const line of signupLines) {
    const view = harness.sendLine(line);
    if (view === null || !assertTerminalView(view)) {
      failures.push("signup-frame-invalid");
      break;
    }
  }

  const enteredWorld = harness.phase === "world" && harness.createdCharacterId !== null;
  if (!enteredWorld) failures.push("signup-did-not-enter-world");
  const firstCommand = harness.sendLine(scenario.firstCommand);
  if (
    firstCommand === null ||
    firstCommand.closed ||
    firstCommand.secret ||
    !firstCommand.text.includes("광장")
  ) {
    failures.push("first-command-not-reached");
  }

  harness.pendingInput = scenario.firstCommand;
  const close = harness.close(1011);
  const pendingInputBeforeReconnect = harness.pendingInput;
  const reopened = harness.reconnect() !== null;
  const replayedPendingInput = pendingInputBeforeReconnect.length > 0 && harness.pendingInput.length > 0;
  if (close.action !== "retry") failures.push("transient-close-not-retried");
  if (!reopened) failures.push("reconnect-not-opened");
  if (replayedPendingInput) failures.push("pending-input-replayed");

  if (reopened) {
    const name = harness.sendLine(scenario.name.toUpperCase());
    const password = harness.sendLine(scenario.password);
    const command = harness.sendLine(scenario.firstCommand);
    if (name === null || !name.secret || password === null || password.secret || command === null || command.closed) {
      failures.push("relogin-contract-failed");
    }
    if (harness.restoredCharacterId !== harness.createdCharacterId) {
      failures.push("character-identity-not-restored");
    }
  }

  const focusIme = terminalPlayFocusImeInvariants();
  for (const value of Object.values(focusIme)) {
    if (!value) failures.push("focus-ime-invariant-failed");
  }
  if (harness.secretTraceContainsPassword) failures.push("secret-trace-leaked-password");
  if (harness.serverViews.some((view) => view.text.includes(scenario.password))) {
    failures.push("server-view-leaked-password");
  }

  return makeReport(
    harness,
    close.action === "retry" ? "retry" : "terminate",
    close.state.attempt,
    reopened,
    replayedPendingInput,
    failures,
  );
}

function makeReport(
  harness: TerminalPlaySmokeHarness,
  action: "retry" | "terminate",
  attempts: number,
  reopened: boolean,
  replayedPendingInput: boolean,
  failures: readonly string[],
): TerminalPlaySmokeReport {
  return {
    passed: failures.length === 0,
    gateway: harness.gateway,
    createdCharacterId: harness.createdCharacterId,
    restoredCharacterId: harness.restoredCharacterId,
    worldCommands: [...harness.worldCommands],
    reconnect: { action, attempts, reopened, replayedPendingInput },
    secretTraceContainsPassword: harness.secretTraceContainsPassword,
    clientTrace: harness.clientTrace.map((entry) => ({ ...entry })),
    serverViews: harness.serverViews.map((view) => ({ ...view })),
    focusIme: terminalPlayFocusImeInvariants(),
    failures: [...failures],
  };
}

/** Exported for tests/readers that want the reconnect rule without a harness. */
export function isTerminalPlayReconnectAllowed(code: number, attempt = 0): boolean {
  return shouldReconnectGatewayClose(code, attempt);
}

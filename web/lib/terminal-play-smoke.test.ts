import assert from "node:assert/strict";
import test from "node:test";

import {
  DEFAULT_TERMINAL_PLAY_SCENARIO,
  TerminalPlaySmokeHarness,
  assertTerminalView,
  createTerminalLineFrame,
  inspectTerminalGateway,
  isTerminalLineFrame,
  parseTerminalView,
  runTerminalPlaySmoke,
  terminalPlayFocusImeInvariants,
} from "./terminal-play-smoke.ts";

test("deterministic terminal smoke completes signup, world entry, command, and relogin", () => {
  const report = runTerminalPlaySmoke();

  assert.equal(report.passed, true, report.failures.join("; "));
  assert.equal(report.createdCharacterId, "character-1");
  assert.equal(report.restoredCharacterId, report.createdCharacterId);
  assert.deepEqual(report.worldCommands, ["봐", "봐"]);
  assert.equal(report.reconnect.action, "retry");
  assert.equal(report.reconnect.reopened, true);
  assert.equal(report.reconnect.replayedPendingInput, false);
  assert.equal(report.reconnect.attempts, 1);
  assert.equal(report.secretTraceContainsPassword, false);
  assert.ok(report.serverViews.every((view) => assertTerminalView(view)));
});

test("smoke uses the exact line/view wire shape and keeps secret views closed-state explicit", () => {
  assert.deepEqual(createTerminalLineFrame("봐"), { type: "line", text: "봐" });
  assert.deepEqual(
    createTerminalLineFrame(DEFAULT_TERMINAL_PLAY_SCENARIO.password),
    { type: "line", text: DEFAULT_TERMINAL_PLAY_SCENARIO.password },
  );
  assert.equal(
    assertTerminalView({
      type: "view",
      text: "새 암호를 넣으십시오: ",
      secret: true,
      closed: false,
    }),
    true,
  );
  assert.equal(
    assertTerminalView({
      type: "view",
      text: "캐릭터가 저장되었습니다.",
      secret: false,
      closed: true,
    }),
    true,
  );
  assert.equal(assertTerminalView({ type: "event", text: "ignored", secret: false, closed: false }), false);
  assert.equal(assertTerminalView({ type: "view", text: "missing flags" }), false);
  assert.equal(
    assertTerminalView({ type: "view", text: "extra", secret: false, closed: false, ticket: "secret" }),
    false,
  );
  assert.equal(isTerminalLineFrame({ type: "line", text: "a\ncommand" }), false);
  assert.equal(isTerminalLineFrame({ type: "line", text: "x".repeat(513) }), false);
  assert.equal(parseTerminalView({ type: "view", text: "광장", secret: false, closed: false })?.closed, false);
});

test("missing or mixed-content gateway is reported before a socket can be opened", () => {
  assert.deepEqual(inspectTerminalGateway(undefined, "http:"), {
    kind: "missing",
    canOpen: false,
  });
  assert.deepEqual(inspectTerminalGateway("ws://mud.test/game", "https:"), {
    kind: "mixed-content",
    canOpen: false,
  });

  const harness = new TerminalPlaySmokeHarness({ gatewayUrl: null });
  assert.equal(harness.connect(), null);
  assert.equal(harness.clientLineCount, 0);
  assert.equal(harness.serverViews.length, 0);
});

test("invalid server messages fail closed and do not enter reconnect flow", () => {
  const harness = new TerminalPlaySmokeHarness();
  assert.ok(harness.connect());

  assert.equal(
    harness.acceptServerFrame({
      type: "view",
      text: "bad",
      secret: "yes",
      closed: false,
    }),
    null,
  );
  assert.equal(harness.connected, false);
  assert.equal(harness.closeReason, "invalid-message");
  assert.equal(harness.close(1011).action, "terminate");
});

test("transient reconnects are bounded and a pending line is never replayed", () => {
  const harness = new TerminalPlaySmokeHarness();
  assert.ok(harness.connect());
  harness.pendingInput = "봐";

  assert.equal(harness.close(1011).action, "retry");
  assert.equal(harness.reconnect()?.text.includes("이름"), true);
  assert.equal(harness.pendingInput, "");

  assert.equal(harness.close(1011).action, "retry");
  assert.ok(harness.reconnect());
  assert.equal(harness.close(1011).action, "retry");
  assert.ok(harness.reconnect());
  assert.equal(harness.close(1011).action, "terminate");
  assert.equal(harness.reconnect(), null);
  assert.equal(harness.reconnectAttempts, 3);
});

test("focus, IME, Korean deletion, and mobile submission invariants are represented", () => {
  assert.deepEqual(terminalPlayFocusImeInvariants(), {
    initialFocus: true,
    selectionPreserved: true,
    compositionDefersResize: true,
    compositionEnterDefersSubmission: true,
    compositionBlocksSubmit: true,
    committedMobileLineSubmits: true,
    koreanCodepointDeletesAsOne: true,
    enterSubmitsOnce: true,
  });
});

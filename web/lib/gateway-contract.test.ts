import test from "node:test";
import assert from "node:assert/strict";

import {
  createGatewayAuthFrame,
  createGatewayLifecycleState,
  decideGatewayClose,
  reduceGatewayLifecycle,
  shouldOpenGatewaySocket,
  shouldReconnectGatewayClose,
} from "./gateway-contract.ts";

const characterId = "00000000-0000-4000-8000-000000000001";

test("auth frame has the exact Gateway contract", () => {
  assert.deepEqual(createGatewayAuthFrame("access-token", characterId), {
    type: "auth",
    accessToken: "access-token",
    characterId,
  });
});

test("socket opening requires an explicitly selected owned character", () => {
  assert.equal(shouldOpenGatewaySocket("loading", characterId, [characterId]), false);
  assert.equal(shouldOpenGatewaySocket("ready", null, [characterId]), false);
  assert.equal(shouldOpenGatewaySocket("ready", characterId, []), false);
  assert.equal(shouldOpenGatewaySocket("ready", characterId, [characterId]), true);
});

test("normal Gateway close is not retried by the terminal", () => {
  assert.equal(shouldReconnectGatewayClose(1000), false);
  assert.equal(shouldReconnectGatewayClose(1008), false);
  assert.equal(shouldReconnectGatewayClose(1002), false);
  assert.equal(shouldReconnectGatewayClose(1001), true);
  assert.equal(shouldReconnectGatewayClose(1006), true);
  assert.equal(shouldReconnectGatewayClose(4001), false);
  assert.equal(shouldReconnectGatewayClose(4403), false);
  assert.equal(shouldReconnectGatewayClose(1011), true);
  assert.equal(shouldReconnectGatewayClose(1012), true);
  assert.equal(shouldReconnectGatewayClose(1013), true);
  assert.equal(shouldReconnectGatewayClose(1011, 3), false);
  assert.equal(decideGatewayClose(1008, 0, true), "terminate");
  assert.equal(decideGatewayClose(1011, 0, true), "terminate");
});

test("Gateway error text is display-only; the subsequent close owns retry policy", () => {
  const initial = createGatewayLifecycleState();
  const error = reduceGatewayLifecycle(initial, {
    type: "error-frame",
    detail: "MUD connection failed",
  });

  assert.equal(error.action, "show-error");
  assert.equal(error.state.phase, initial.phase);
  assert.equal(error.state.failed, false);
  assert.equal(error.state.error, "MUD connection failed");

  const retry1011 = reduceGatewayLifecycle(error.state, {
    type: "close",
    code: 1011,
  });
  assert.equal(retry1011.action, "retry");

  const retry1006 = reduceGatewayLifecycle(error.state, {
    type: "close",
    code: 1006,
  });
  assert.equal(retry1006.action, "retry");

  const retry1013 = reduceGatewayLifecycle(error.state, {
    type: "close",
    code: 1013,
  });
  assert.equal(retry1013.action, "retry");

  const terminate1008 = reduceGatewayLifecycle(error.state, {
    type: "close",
    code: 1008,
  });
  assert.equal(terminate1008.action, "terminate");
});

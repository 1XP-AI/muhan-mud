import test from "node:test";
import assert from "node:assert/strict";

import {
  createGatewayAuthFrame,
  shouldOpenGatewaySocket,
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

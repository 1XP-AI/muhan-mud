import assert from "node:assert/strict";
import test from "node:test";

import { resolveGatewayUrl, validateGatewayUrl } from "./gateway-url.ts";

test("the Go runtime coordinate wins and the explicit gateway variable is a fallback", () => {
  assert.equal(
    resolveGatewayUrl({
      MUD_GO_GATEWAY_URL: "wss://go.example/ws",
      MUD_GATEWAY_URL: "wss://fallback.example/ws",
    }),
    "wss://go.example/ws",
  );
  assert.equal(
    resolveGatewayUrl({ MUD_GATEWAY_URL: "ws://localhost:8080/ws" }),
    "ws://localhost:8080/ws",
  );
  assert.equal(
    resolveGatewayUrl({ MUD_GO_GATEWAY_URL: "  ", MUD_GATEWAY_URL: " ws://fallback.example/ws " }),
    "ws://fallback.example/ws",
  );
});

test("a missing coordinate is reported without opening a socket", () => {
  assert.deepEqual(validateGatewayUrl(undefined, "http:"), {
    kind: "missing",
    url: null,
  });
  assert.deepEqual(validateGatewayUrl("   ", "http:"), {
    kind: "missing",
    url: null,
  });
});

test("non-WebSocket and malformed coordinates are invalid", () => {
  assert.deepEqual(validateGatewayUrl("https://gateway.example/ws", "http:"), {
    kind: "invalid",
    url: null,
  });
  assert.deepEqual(validateGatewayUrl("not a URL", "http:"), {
    kind: "invalid",
    url: null,
  });
});

test("ws on an HTTPS page is rejected as mixed content", () => {
  assert.deepEqual(validateGatewayUrl("ws://gateway.example/ws", "https:"), {
    kind: "mixed-content",
    url: null,
  });
});

test("ws and wss coordinates are valid on their allowed page protocols", () => {
  const ws = validateGatewayUrl("ws://localhost:8080/ws", "http:");
  assert.equal(ws.kind, "valid");
  if (ws.kind === "valid") assert.equal(ws.url.toString(), "ws://localhost:8080/ws");

  const wss = validateGatewayUrl("wss://gateway.example/ws", "https:");
  assert.equal(wss.kind, "valid");
  if (wss.kind === "valid") assert.equal(wss.url.toString(), "wss://gateway.example/ws");
});

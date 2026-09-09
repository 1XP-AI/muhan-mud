import assert from "node:assert/strict";
import test from "node:test";

import {
  canRestoreTerminalFocus,
  shouldDeferTerminalResize,
} from "./terminal-focus.ts";

test("terminal focus returns only when it will not interrupt IME or selection", () => {
  assert.equal(
    canRestoreTerminalFocus({
      disposed: false,
      composing: false,
      hasSelection: false,
      documentFocused: true,
      activeElementOutsideTerminal: false,
    }),
    true,
  );
  assert.equal(
    canRestoreTerminalFocus({
      disposed: false,
      composing: true,
      hasSelection: false,
      documentFocused: true,
      activeElementOutsideTerminal: false,
    }),
    false,
  );
  assert.equal(
    canRestoreTerminalFocus({
      disposed: false,
      composing: false,
      hasSelection: true,
      documentFocused: true,
      activeElementOutsideTerminal: false,
    }),
    false,
  );
  assert.equal(
    canRestoreTerminalFocus({
      disposed: false,
      composing: false,
      hasSelection: false,
      documentFocused: false,
      activeElementOutsideTerminal: false,
    }),
    false,
  );
  assert.equal(
    canRestoreTerminalFocus({
      disposed: false,
      composing: false,
      hasSelection: false,
      documentFocused: true,
      activeElementOutsideTerminal: true,
    }),
    false,
  );
});

test("terminal resize waits for compositionend before recalculating rows", () => {
  assert.equal(shouldDeferTerminalResize(true), true);
  assert.equal(shouldDeferTerminalResize(false), false);
});

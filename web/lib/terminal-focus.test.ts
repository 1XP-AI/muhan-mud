import assert from "node:assert/strict";
import test from "node:test";

import {
  canSubmitMobileLine,
  canRestoreTerminalFocus,
  getMobileViewportHeight,
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

test("mobile command submission waits for readiness and IME completion", () => {
  assert.equal(
    canSubmitMobileLine({ value: "look", ready: true, composing: false }),
    true,
  );
  assert.equal(
    canSubmitMobileLine({ value: "look", ready: false, composing: false }),
    false,
  );
  assert.equal(
    canSubmitMobileLine({ value: "look", ready: true, composing: true }),
    false,
  );
  assert.equal(
    canSubmitMobileLine({ value: "", ready: true, composing: false }),
    false,
  );
  assert.equal(
    canSubmitMobileLine({ value: " ", ready: true, composing: false }),
    true,
  );
});

test("mobile viewport height tracks the visual viewport above the keyboard", () => {
  assert.equal(getMobileViewportHeight(844), 844);
  assert.equal(getMobileViewportHeight(420, 72), 348);
  assert.equal(getMobileViewportHeight(420, -10), 420);
  assert.equal(getMobileViewportHeight(420, Number.NaN), 420);
  assert.equal(getMobileViewportHeight(0), null);
  assert.equal(getMobileViewportHeight(Number.POSITIVE_INFINITY), null);
});

export interface TerminalFocusState {
  disposed: boolean;
  composing: boolean;
  hasSelection: boolean;
  documentFocused: boolean;
  activeElementOutsideTerminal: boolean;
}

/** Do not steal focus while IME composition, selection, or tab switching is active. */
export function canRestoreTerminalFocus({
  disposed,
  composing,
  hasSelection,
  documentFocused,
  activeElementOutsideTerminal,
}: TerminalFocusState): boolean {
  return (
    !disposed &&
    !composing &&
    !hasSelection &&
    documentFocused &&
    !activeElementOutsideTerminal
  );
}

/** xterm must not recalculate rows/columns in the middle of an IME composition. */
export function shouldDeferTerminalResize(composing: boolean): boolean {
  return composing;
}

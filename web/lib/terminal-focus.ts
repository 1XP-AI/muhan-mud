export interface TerminalFocusState {
  disposed: boolean;
  composing: boolean;
  hasSelection: boolean;
  documentFocused: boolean;
  activeElementOutsideTerminal: boolean;
}

export interface MobileLineSubmissionState {
  value: string;
  ready: boolean;
  composing: boolean;
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

/**
 * xterm can emit an Enter data event before the browser's compositionend
 * event. Defer that line break so a composed Korean syllable is not submitted
 * twice (or before its final text reaches the line buffer).
 */
export function shouldDeferTerminalSubmission(
  data: string,
  composing: boolean,
): boolean {
  return composing && /[\r\n]/u.test(data);
}

/** Do not submit a mobile command until the socket is ready and IME is idle. */
export function canSubmitMobileLine({
  value,
  ready,
  composing,
}: MobileLineSubmissionState): boolean {
  return ready && !composing && value.length > 0;
}

/** Return a safe CSS height for the visible mobile viewport. */
export function getMobileViewportHeight(
  viewportHeight: number,
  layoutTop = 0,
): number | null {
  if (!Number.isFinite(viewportHeight) || viewportHeight <= 0) {
    return null;
  }

  const safeTop = Number.isFinite(layoutTop) ? Math.max(0, layoutTop) : 0;
  return Math.max(0, Math.round(viewportHeight - safeTop));
}

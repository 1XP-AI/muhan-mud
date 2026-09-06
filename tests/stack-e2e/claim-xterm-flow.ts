/**
 * The browser acceptance lane must not type into xterm until the onboarding
 * component has installed its real input handler.  Keeping this small flow
 * separate makes the ordering executable without starting the disposable
 * Docker stack.
 */
export interface ClaimXtermFlowDriver {
  waitForReadyNamePrompt(): Promise<void>;
  typeThroughRenderedXterm(value: string): Promise<void>;
  waitForCPasswordPrompt(): Promise<void>;
}

export async function completeClaimThroughRenderedXterm(
  driver: ClaimXtermFlowDriver,
  characterName: string,
  legacyPassword: string,
): Promise<void> {
  await driver.waitForReadyNamePrompt();
  await driver.typeThroughRenderedXterm(characterName);
  // C must issue this prompt only after the Gateway has accepted the name and
  // privately completed CHALLENGE -> ALLOW.  Never type a password before it.
  await driver.waitForCPasswordPrompt();
  await driver.typeThroughRenderedXterm(legacyPassword);
}

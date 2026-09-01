export interface GatewayAuthFrame {
  type: "auth";
  accessToken: string;
  characterId: string;
}

export function createGatewayAuthFrame(
  accessToken: string,
  characterId: string,
): GatewayAuthFrame {
  return { type: "auth", accessToken, characterId };
}

export function shouldOpenGatewaySocket(
  rosterStatus: "loading" | "error" | "empty" | "ready",
  characterId: string | null,
  ownedCharacterIds: readonly string[],
): boolean {
  return (
    rosterStatus === "ready" &&
    characterId !== null &&
    ownedCharacterIds.includes(characterId)
  );
}

export function shouldReconnectGatewayClose(code: number): boolean {
  return !(
    code === 1000 ||
    code === 1008 ||
    code === 4001 ||
    (code >= 4400 && code < 4500)
  );
}

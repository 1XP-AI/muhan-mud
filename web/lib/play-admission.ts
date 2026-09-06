export type OnboardingCompletion = "claimed" | "provisioned";
export type PlayAdmissionRosterStatus = "loading" | "error" | "empty" | "ready";

export interface PlayAdmissionCharacter {
  id: string;
  lifecycle: "active";
}

/**
 * A browser-local target for the normal game socket after onboarding closes.
 * It does not assert ownership itself: the roster query and Gateway lease are
 * the two independent owner-active boundaries.
 */
export interface PlayAdmissionHandoff {
  ownerUserId: string;
  characterId: string;
  completion: OnboardingCompletion;
}

const strictLowerUuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

function isStrictLowerUuid(value: unknown): value is string {
  return typeof value === "string" && strictLowerUuid.test(value);
}

/**
 * An exact completion may be replayed after a dropped control frame. Once a
 * target is recorded, never replace it from a later or conflicting frame.
 */
export function completeOnboardingHandoff(
  ownerUserId: string,
  characterId: string,
  completion: OnboardingCompletion,
  existing: PlayAdmissionHandoff | null = null,
): PlayAdmissionHandoff | null {
  if (existing) return existing;
  if (
    !isStrictLowerUuid(ownerUserId) ||
    !isStrictLowerUuid(characterId) ||
    (completion !== "claimed" && completion !== "provisioned")
  ) {
    return null;
  }
  return { ownerUserId, characterId, completion };
}

/**
 * The terminal is allowed to open only after the refreshed, owner-filtered
 * roster contains the completed active character. The Gateway then acquires
 * its own owner-active lease before issuing the C admission ticket.
 */
export function resolvePlayAdmission(
  viewerUserId: string,
  rosterStatus: PlayAdmissionRosterStatus,
  characters: readonly PlayAdmissionCharacter[],
  handoff: PlayAdmissionHandoff | null,
): PlayAdmissionHandoff | null {
  if (
    !handoff ||
    rosterStatus !== "ready" ||
    viewerUserId !== handoff.ownerUserId ||
    !characters.some(
      (character) =>
        character.id === handoff.characterId && character.lifecycle === "active",
    )
  ) {
    return null;
  }
  return handoff;
}

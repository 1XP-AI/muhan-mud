import assert from "node:assert/strict";
import test from "node:test";

import {
  completeOnboardingHandoff,
  resolvePlayAdmission,
} from "./play-admission.ts";

const accountId = "00000000-0000-4000-8000-000000000001";
const characterId = "00000000-0000-4000-8000-000000000002";
const otherCharacterId = "00000000-0000-4000-8000-000000000003";

const activeRoster = [{
  id: characterId,
  world_id: "muhan",
  legacy_name: "Contracthero",
  lifecycle: "active" as const,
}];

test("an account with no owned active roster entry cannot start normal game admission", () => {
  const handoff = completeOnboardingHandoff(accountId, characterId, "claimed");

  assert.equal(resolvePlayAdmission(accountId, "empty", [], handoff), null);
  assert.equal(resolvePlayAdmission(accountId, "ready", [], handoff), null);
  assert.equal(resolvePlayAdmission(accountId, "ready", [
    { ...activeRoster[0], id: otherCharacterId },
  ], handoff), null);
});

test("both finalized legacy claim and web-first provision wait for the owned active roster then use normal Gateway auth", () => {
  for (const completion of ["claimed", "provisioned"] as const) {
    const handoff = completeOnboardingHandoff(accountId, characterId, completion);

    assert.equal(resolvePlayAdmission(accountId, "loading", activeRoster, handoff), null);
    assert.deepEqual(resolvePlayAdmission(accountId, "ready", activeRoster, handoff), {
      characterId,
      ownerUserId: accountId,
      completion,
    });
  }
});

test("a duplicate completion cannot retarget an established association, and a conflicting account cannot use it", () => {
  const initial = completeOnboardingHandoff(accountId, characterId, "claimed");
  const retry = completeOnboardingHandoff(accountId, characterId, "claimed", initial);
  const conflict = completeOnboardingHandoff(accountId, otherCharacterId, "provisioned", initial);

  assert.deepEqual(retry, initial);
  assert.deepEqual(conflict, initial);
  assert.equal(resolvePlayAdmission(otherCharacterId, "ready", activeRoster, initial), null);
});

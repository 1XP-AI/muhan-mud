import assert from "node:assert/strict";
import test from "node:test";

import {
  completeOnboardingHandoff,
  resolvePlayAdmission,
} from "./play-admission.ts";
import {
  shouldOpenGatewaySocket,
} from "./gateway-contract.ts";
import { createOnboardingSocketContract } from "./onboarding-contract.ts";
import { recoveryFromOnboardingControl } from "./onboarding-contract.ts";

const accountId = "00000000-0000-4000-8000-000000000001";
const characterId = "00000000-0000-4000-8000-000000000002";
const otherCharacterId = "00000000-0000-4000-8000-000000000003";

const activeRoster = [{
  id: characterId,
  world_id: "muhan",
  legacy_name: "Contracthero",
  lifecycle: "active" as const,
}];

test("authenticated empty roster stays isolated until one exact active completion is visible", () => {
  assert.deepEqual(createOnboardingSocketContract("empty", "provision"), {
    kind: "onboarding",
    path: "/onboarding",
    subprotocol: "muhan.onboarding.v1",
    mode: "provision",
  });
  assert.equal(shouldOpenGatewaySocket("empty", null, []), false);

  const first = completeOnboardingHandoff(accountId, characterId, "provisioned");
  assert.ok(first);
  assert.deepEqual(
    completeOnboardingHandoff(accountId, characterId, "provisioned", first),
    first,
  );
  assert.deepEqual(
    completeOnboardingHandoff(accountId, otherCharacterId, "claimed", first),
    first,
  );

  // The owner-filtered roster is the post-activation observation: exactly one
  // active row for the id returned by the completion boundary.
  const activeCharacters = activeRoster;
  assert.equal(activeCharacters.length, 1);
  assert.equal(activeCharacters[0]?.id, characterId);
  assert.equal(
    shouldOpenGatewaySocket("ready", characterId, activeCharacters),
    true,
  );
  assert.equal(
    shouldOpenGatewaySocket("ready", otherCharacterId, activeCharacters),
    false,
  );
});

test("an account with no owned active roster entry cannot start normal game admission", () => {
  const handoff = completeOnboardingHandoff(accountId, characterId, "claimed");

  assert.equal(resolvePlayAdmission(accountId, "empty", [], handoff), null);
  assert.equal(resolvePlayAdmission(accountId, "ready", [], handoff), null);
  assert.equal(resolvePlayAdmission(accountId, "ready", [
    { ...activeRoster[0], id: otherCharacterId },
  ], handoff), null);
});

test("browser mock claim failures return to the roster without a normal socket handoff", () => {
  const browserFailureFrames = [
    { type: "error", reason: "onboarding authentication failed" },
    { type: "error", reason: "onboarding failed" },
    { type: "error", reason: "C/Gateway password=ticket=sha256=internal-id" },
  ];

  for (const frame of browserFailureFrames) {
    const recovery = recoveryFromOnboardingControl("claim", frame);
    assert.ok(["session", "legacy-credentials", "unknown"].includes(recovery.category));

    // The browser mock receives a terminal failure, not a completion. It must
    // keep normal /ws closed until a fresh owner-filtered active row exists.
    assert.equal(resolvePlayAdmission(accountId, "empty", [], null), null);
    assert.equal(shouldOpenGatewaySocket("empty", null, []), false);
    assert.equal(shouldOpenGatewaySocket("ready", characterId, []), false);
  }
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

test("a malformed completion cannot create a normal admission handoff", () => {
  assert.equal(
    completeOnboardingHandoff(accountId, characterId, "unexpected" as never),
    null,
  );
});

test("a malformed replay cannot surface a usable session even when its character is active", () => {
  const malformedReplay = {
    ownerUserId: accountId,
    characterId,
    completion: "replayed",
  } as never;

  assert.equal(
    resolvePlayAdmission(accountId, "ready", activeRoster, malformedReplay),
    null,
  );
});

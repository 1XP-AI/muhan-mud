import assert from "node:assert/strict";
import test from "node:test";

import { decideClaimEntry } from "./claim-transparency.ts";

test("a signed-in empty roster without onboarding cannot start a claim", () => {
  assert.deepEqual(decideClaimEntry("empty", false), {
    kind: "unavailable",
    title: "웹 로그인만으로는 MUD 캐릭터를 소유하지 않습니다",
    detail: "이 서버에서는 기존 캐릭터 연결을 웹에서 시작할 수 없습니다.",
  });
});

test("only an enabled empty roster exposes the existing isolated claim start", () => {
  assert.deepEqual(decideClaimEntry("empty", true), {
    kind: "claim-start",
    title: "기존 캐릭터 연결을 시작할 수 있습니다",
    detail: "기존 캐릭터는 xterm 안에서 원래 MUD 확인을 마친 뒤에만 플레이할 수 있습니다.",
    actionLabel: "기존 캐릭터 연결 시작",
  });

  for (const status of ["loading", "error", "ready"] as const) {
    assert.equal(decideClaimEntry(status, true).kind, "hidden");
  }
});

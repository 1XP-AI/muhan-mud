export type ClaimEntryRosterStatus = "loading" | "error" | "empty" | "ready";

export type ClaimEntryDecision =
  | { kind: "hidden" }
  | {
      kind: "unavailable";
      title: "웹 로그인만으로는 MUD 캐릭터를 소유하지 않습니다";
      detail: "이 서버에서는 기존 캐릭터 연결을 웹에서 시작할 수 없습니다.";
    }
  | {
      kind: "claim-start";
      title: "기존 캐릭터 연결을 시작할 수 있습니다";
      detail: "기존 캐릭터는 xterm 안에서 원래 MUD 확인을 마친 뒤에만 플레이할 수 있습니다.";
      actionLabel: "기존 캐릭터 연결 시작";
    };

/**
 * Keep the web claim boundary explicit: a session is never character
 * ownership, and the only web-started claim path is the enabled onboarding
 * terminal for an empty roster.
 */
export function decideClaimEntry(
  rosterStatus: ClaimEntryRosterStatus,
  onboardingEnabled: boolean,
): ClaimEntryDecision {
  if (rosterStatus !== "empty") return { kind: "hidden" };

  if (!onboardingEnabled) {
    return {
      kind: "unavailable",
      title: "웹 로그인만으로는 MUD 캐릭터를 소유하지 않습니다",
      detail: "이 서버에서는 기존 캐릭터 연결을 웹에서 시작할 수 없습니다.",
    };
  }

  return {
    kind: "claim-start",
    title: "기존 캐릭터 연결을 시작할 수 있습니다",
    detail: "기존 캐릭터는 xterm 안에서 원래 MUD 확인을 마친 뒤에만 플레이할 수 있습니다.",
    actionLabel: "기존 캐릭터 연결 시작",
  };
}

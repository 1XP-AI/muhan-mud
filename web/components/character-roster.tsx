"use client";

import type {
  CharacterRosterStatus,
  OwnedCharacter,
} from "@/lib/character-roster";
import type { OnboardingMode } from "@/lib/onboarding-contract";

interface CharacterRosterProps {
  status: CharacterRosterStatus;
  characters: OwnedCharacter[];
  selectedId: string | null;
  error: string | null;
  onSelect: (id: string) => void;
  onRetry: () => void;
  onEnter: () => void;
  onboardingEnabled?: boolean;
  onStartOnboarding?: (mode: OnboardingMode) => void;
}

export function CharacterRoster({
  status,
  characters,
  selectedId,
  error,
  onSelect,
  onRetry,
  onEnter,
  onboardingEnabled = false,
  onStartOnboarding,
}: CharacterRosterProps) {
  if (status === "loading") {
    return (
      <div className="roster-state" role="status">
        캐릭터 목록을 불러오는 중…
      </div>
    );
  }

  if (status === "error") {
    return (
      <div className="roster-state roster-error" role="alert">
        <p>{error ?? "캐릭터 목록을 확인하지 못했습니다."}</p>
        <button className="secondary-action" onClick={onRetry} type="button">
          다시 불러오기
        </button>
      </div>
    );
  }

  if (status === "empty") {
    return (
      <div className="roster-state roster-empty">
        <p className="roster-empty-title">이 계정에 연결된 캐릭터가 없습니다</p>
        {onboardingEnabled && onStartOnboarding ? (
          <>
            <p>
              새 캐릭터를 만들거나 기존 텔넷 캐릭터를 이 계정에 연결할 수 있습니다.
              진행 과정은 안전한 터미널에서 계속됩니다.
            </p>
            <div className="onboarding-actions" aria-label="캐릭터 온보딩 선택">
              <button
                className="primary-action"
                onClick={() => onStartOnboarding("provision")}
                type="button"
              >
                새 캐릭터 만들기
              </button>
              <button
                className="secondary-action onboarding-claim-action"
                onClick={() => onStartOnboarding("claim")}
                type="button"
              >
                기존 캐릭터 연결
              </button>
            </div>
            <p className="onboarding-note">
              게임 비밀번호는 웹 계정 비밀번호와 다른 값을 사용하세요.
            </p>
          </>
        ) : (
          <p>
            운영자가 캐릭터를 연결하거나 claim 단계를 열어야 이곳에 표시됩니다.
            지금은 게임 데이터를 변경할 수 없습니다.
          </p>
        )}
      </div>
    );
  }

  return (
    <div className="character-roster">
      <div className="roster-heading">
        <div>
          <p className="eyebrow">CHARACTER ACCESS LOG</p>
          <h2>입장할 캐릭터를 고르세요</h2>
        </div>
        <span>{characters.length} RECORD{characters.length === 1 ? "" : "S"}</span>
      </div>
      <fieldset className="character-list">
        <legend className="sr-only">내 캐릭터 목록</legend>
        {characters.map((character, index) => (
          <label
            className="character-row"
            data-selected={selectedId === character.id}
            key={character.id}
          >
            <input
              checked={selectedId === character.id}
              name="mud-character"
              onChange={() => onSelect(character.id)}
              type="radio"
              value={character.id}
            />
            <span className="character-slot" aria-hidden="true">
              {String(index + 1).padStart(2, "0")}
            </span>
            <span className="character-row-main">
              <strong>{character.legacy_name}</strong>
              <small>
                {character.world_id} · ACTIVE · {character.id.slice(0, 8)}
              </small>
            </span>
            <span className="character-mark" aria-hidden="true" />
          </label>
        ))}
      </fieldset>
      <button
        className="primary-action roster-enter"
        disabled={!selectedId}
        onClick={onEnter}
        type="button"
      >
        게임 입장
      </button>
    </div>
  );
}

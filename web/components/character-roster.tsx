"use client";

import type {
  CharacterRosterStatus,
  OwnedCharacter,
} from "@/lib/character-roster";

interface CharacterRosterProps {
  status: CharacterRosterStatus;
  characters: OwnedCharacter[];
  selectedId: string | null;
  error: string | null;
  onSelect: (id: string) => void;
  onRetry: () => void;
  onEnter: () => void;
}

export function CharacterRoster({
  status,
  characters,
  selectedId,
  error,
  onSelect,
  onRetry,
  onEnter,
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
      <div className="roster-state">
        <p className="roster-empty-title">이 계정에 연결된 캐릭터가 없습니다</p>
        <p>
          운영자가 캐릭터를 연결하거나 claim 단계를 열어야 이곳에 표시됩니다.
          지금은 게임 데이터를 변경할 수 없습니다.
        </p>
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

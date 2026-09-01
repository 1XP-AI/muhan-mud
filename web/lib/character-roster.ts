"use client";

import type { SupabaseClient } from "@supabase/supabase-js";
import { useCallback, useEffect, useState } from "react";

export type CharacterRosterStatus = "loading" | "error" | "empty" | "ready";

export interface OwnedCharacter {
  id: string;
  world_id: string;
  legacy_name: string;
  lifecycle: "active";
}

export interface CharacterRosterState {
  status: CharacterRosterStatus;
  characters: OwnedCharacter[];
  selectedId: string | null;
  error: string | null;
  ownerUserId: string | null;
}

const strictLowerUuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

export function isStrictLowerUuid(value: unknown): value is string {
  return typeof value === "string" && strictLowerUuid.test(value);
}

function isSafeText(value: unknown, maxLength: number): value is string {
  return (
    typeof value === "string" &&
    value.length >= 1 &&
    value.length <= maxLength &&
    !/[\u0000-\u001f\u007f]/u.test(value)
  );
}

function isSafeLegacyName(value: unknown): value is string {
  if (
    typeof value !== "string" ||
    value === "." ||
    value === ".." ||
    [...value].length < 1 ||
    [...value].length > 12 ||
    new TextEncoder().encode(value).byteLength > 14 ||
    /[\u0000-\u001f\u007f/\\:]/u.test(value)
  ) {
    return false;
  }
  let canonical = value.replace(/[A-Z]/g, (letter) => letter.toLowerCase());
  if (/^[a-z]/.test(canonical)) {
    canonical = canonical[0]!.toUpperCase() + canonical.slice(1);
  }
  return canonical === value;
}

function parseCharacter(row: unknown): OwnedCharacter | null {
  if (typeof row !== "object" || row === null || Array.isArray(row)) {
    return null;
  }

  const candidate = row as Record<string, unknown>;
  if (
    !isStrictLowerUuid(candidate.id) ||
    !isSafeText(candidate.world_id, 64) ||
    !isSafeLegacyName(candidate.legacy_name) ||
    candidate.lifecycle !== "active"
  ) {
    return null;
  }

  return {
    id: candidate.id,
    world_id: candidate.world_id,
    legacy_name: candidate.legacy_name,
    lifecycle: "active",
  };
}

export function parseCharacterRows(rows: unknown): OwnedCharacter[] {
  if (!Array.isArray(rows)) {
    return [];
  }

  const byId = new Map<string, OwnedCharacter>();
  for (const row of rows) {
    const character = parseCharacter(row);
    if (character && !byId.has(character.id)) {
      byId.set(character.id, character);
    }
  }

  return [...byId.values()].sort(
    (left, right) =>
      left.legacy_name.localeCompare(right.legacy_name, "ko") ||
      left.id.localeCompare(right.id),
  );
}

export function selectOwnedCharacter(
  characters: readonly OwnedCharacter[],
  id: string,
): string | null {
  return characters.some((character) => character.id === id) ? id : null;
}

function storedSelectionKey(userId: string): string {
  return `muhan.selected-character.${userId}`;
}

function readStoredSelection(userId: string): string | null {
  if (typeof window === "undefined") {
    return null;
  }
  try {
    const value = window.localStorage.getItem(storedSelectionKey(userId));
    return value && isStrictLowerUuid(value) ? value : null;
  } catch {
    return null;
  }
}

const initialState: CharacterRosterState = {
  status: "loading",
  characters: [],
  selectedId: null,
  error: null,
  ownerUserId: null,
};

export function useCharacterRoster(
  supabase: SupabaseClient | null,
  userId: string | null,
): CharacterRosterState & {
  selectCharacter: (id: string) => void;
  retry: () => void;
} {
  const [state, setState] = useState<CharacterRosterState>(initialState);
  const [reloadToken, setReloadToken] = useState(0);

  useEffect(() => {
    let cancelled = false;
    let selectedForRequest: string | null = null;

    setState((previous) => {
      selectedForRequest =
        previous.ownerUserId === userId &&
        previous.selectedId && previous.characters.some(
          (character) => character.id === previous.selectedId,
        )
          ? previous.selectedId
          : null;
      return {
        status: "loading",
        characters: [],
        selectedId: selectedForRequest,
        error: null,
        ownerUserId: userId,
      };
    });

    if (!supabase || !userId) {
      setState({ ...initialState, status: "empty", ownerUserId: null });
      return () => {
        cancelled = true;
      };
    }

    if (!isStrictLowerUuid(userId)) {
      setState({
        status: "error",
        characters: [],
        selectedId: null,
        error: "계정 식별자를 확인하지 못했습니다.",
        ownerUserId: userId,
      });
      return () => {
        cancelled = true;
      };
    }

    void supabase
      .from("game_characters")
      .select("id, world_id, legacy_name, lifecycle")
      .eq("owner_user_id", userId)
      .eq("lifecycle", "active")
      .then(({ data, error }) => {
        if (cancelled) {
          return;
        }
        if (error) {
          setState({
            status: "error",
            characters: [],
            selectedId: null,
            error: "캐릭터 목록을 불러오지 못했습니다.",
            ownerUserId: userId,
          });
          return;
        }

        const characters = parseCharacterRows(data);
        const selectedId = selectOwnedCharacter(
          characters,
          selectedForRequest ?? readStoredSelection(userId) ?? "",
        );
        if (selectedId && typeof window !== "undefined") {
          try {
            window.localStorage.setItem(storedSelectionKey(userId), selectedId);
          } catch {
            // Private browsing/storage-disabled contexts still work in memory.
          }
        }
        setState({
          status: characters.length > 0 ? "ready" : "empty",
          characters,
          selectedId,
          error: null,
          ownerUserId: userId,
        });
      });

    return () => {
      cancelled = true;
    };
  }, [reloadToken, supabase, userId]);

  const selectCharacter = useCallback((id: string) => {
    setState((previous) => {
      const selectedId = selectOwnedCharacter(previous.characters, id);
      if (selectedId && userId && typeof window !== "undefined") {
        try {
          window.localStorage.setItem(storedSelectionKey(userId), selectedId);
        } catch {
          // Private browsing/storage-disabled contexts still work in memory.
        }
      }
      return { ...previous, selectedId };
    });
  }, [userId]);

  const retry = useCallback(() => {
    setReloadToken((token) => token + 1);
  }, []);

  const visibleState = state.ownerUserId === userId
    ? state
    : {
        ...initialState,
        status: userId ? "loading" as const : "empty" as const,
        ownerUserId: userId,
      };

  return { ...visibleState, retry, selectCharacter };
}

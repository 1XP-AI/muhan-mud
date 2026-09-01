import test from "node:test";
import assert from "node:assert/strict";

import {
  isStrictLowerUuid,
  parseCharacterRows,
  selectOwnedCharacter,
} from "./character-roster.ts";

const firstId = "00000000-0000-4000-8000-000000000001";
const secondId = "00000000-0000-4000-8000-000000000002";

test("filters malformed and inactive rows without exposing them", () => {
  const rows = [
    {
      id: firstId,
      world_id: "muhan",
      legacy_name: "검객",
      lifecycle: "active",
    },
    {
      id: "00000000-0000-4000-8000-00000000000A",
      world_id: "muhan",
      legacy_name: "대문자UUID",
      lifecycle: "active",
    },
    {
      id: secondId,
      world_id: "muhan",
      legacy_name: "휴면",
      lifecycle: "suspended",
    },
    {
      id: secondId,
      world_id: "muhan",
      legacy_name: "나쁜/이름",
      lifecycle: "active",
    },
    {
      id: secondId,
      world_id: "muhan",
      legacy_name: "너무긴이름입니다",
      lifecycle: "active",
    },
  ];

  assert.deepEqual(parseCharacterRows(rows), [
    {
      id: firstId,
      world_id: "muhan",
      legacy_name: "검객",
      lifecycle: "active",
    },
  ]);
});

test("keeps deterministic order and removes duplicate IDs", () => {
  const rows = [
    {
      id: secondId,
      world_id: "muhan",
      legacy_name: "Beta",
      lifecycle: "active",
    },
    {
      id: firstId,
      world_id: "muhan",
      legacy_name: "Alpha",
      lifecycle: "active",
    },
    {
      id: secondId,
      world_id: "other",
      legacy_name: "ignored duplicate",
      lifecycle: "active",
    },
  ];

  assert.deepEqual(
    parseCharacterRows(rows).map(({ id, legacy_name }) => ({ id, legacy_name })),
    [
      { id: firstId, legacy_name: "Alpha" },
      { id: secondId, legacy_name: "Beta" },
    ],
  );
});

test("filters reserved and noncanonical legacy names before selection", () => {
  assert.deepEqual(parseCharacterRows([
    { id: firstId, world_id: "muhan", legacy_name: ".", lifecycle: "active" },
    { id: secondId, world_id: "muhan", legacy_name: "hERO", lifecycle: "active" },
  ]), []);
});

test("selection is restricted to the current account roster", () => {
  const characters = parseCharacterRows([
    {
      id: firstId,
      world_id: "muhan",
      legacy_name: "검객",
      lifecycle: "active",
    },
  ]);

  assert.equal(selectOwnedCharacter(characters, firstId), firstId);
  assert.equal(selectOwnedCharacter(characters, secondId), null);
  assert.equal(isStrictLowerUuid(firstId), true);
  assert.equal(
    isStrictLowerUuid("abcdefab-cdef-4abc-8def-abcdefabcdef"),
    true,
  );
  assert.equal(
    isStrictLowerUuid("ABCDEFAB-CDEF-4ABC-8DEF-ABCDEFABCDEF"),
    false,
  );
});

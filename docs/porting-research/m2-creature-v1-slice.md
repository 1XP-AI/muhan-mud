# M2 CreatureV1 core flat sub-slice

상태: clone-only test codec (2026-09-02)

이 문서는 `m2-creature-object-codec-map.md`의 전체 CreatureV1 목표를 대체하지
않는다. 이번 구현은 live MUD, `write_crt/read_crt`, player save path에 연결하지
않는 **core flat sub-slice**다. 증명 대상도 native legacy file byte 동일성이
아니라, 아래 logical field mapping과 새 CDTO V1 canonical byte의 C/Rust 동일성이다.

## 고정 37-field schema

CDTO kind `CREATURE`의 field 1-37은 `name`, `description`, `key[3]`, level/type/
class/race/numwander, alignment, five stats, HP/MP max/current, armor/thaco,
experience/gold, three dice/special, proficiency[5], realm[4], spells, flags,
quests, questnum, carry[10], rom_num, daily[10] 순서다. Scalar는 explicit BE
i8/u8/i16/i64이고, proficiency/realm/carry/daily는 각 원소를 BE로 pack한다.
`daily`는 native padding을 절대 복사하지 않는 `(u8 max,u8 cur,i64 last_used)`
10개다.

`name[80]`, `description[80]`, `key[*][20]`는 fixed bytes이되 최초 NUL 이후가
모두 zero여야 한다. 이 padding rule과 `hpcur <= hpmax`, `mpcur <= mpmax`,
`daily.cur <= daily.max`은 C와 Rust decoder 모두 reject-only로 확인한다. 따라서
decode→encode가 입력 byte를 정규화해 바꾸지 않는다.

## 명시적 제외와 이유

- `password[15]`: credential material. Encoder가 참조하지 않으며 decoder는
  모두 zero로 scrub한다.
- `talk[80]`, `first_tlk`: talk state는 이 slice 범위 밖이며 nonzero talk 또는
  talk link는 exporter가 fail-closed 한다.
- `fd`, ready slots, following/follower/enemy/talk links, `first_obj`,
  `parent_rom`: session/transport, attachment, pointer 또는 recursive graph다.
  Decoder는 `fd=-1`, 모든 pointer zero로 초기화한다; ready/inventory attachment는
  exporter가 fail-closed 한다.
- `lasttime[45]`: time-sensitive gameplay history는 이 core flat sub-slice에서
  제외한다. 전체 CreatureV1 follow-on field **38**은 explicit `lasttime` subcodec,
  field **39**는 bounded ordered inventory/ObjectV1 graph로 예약한다. 둘 다 raw
  struct/pointer dump가 아닌 별도 TDD codec과 graph cap이 필요하다.

Legacy `creature`의 struct padding, native endianness/long width, and pointers are
ABI artifacts, not fields in this contract. The differential oracle uses only
synthetic vectors and never prints password or player-file content.

## Safety proof

`tests/unit/creature_v1_test.c` covers stale output reset on failed C encode,
pointer/inventory/talk rejection, password-shape independence, full password/talk
scrub, padding/current-max canonicality rejection, and poison native `daily`
padding equality. `tests/harness/cdto_v1_oracle.c` plus Rust differential tests
require C export → Rust decode/reencode → C decode/reencode byte equality.

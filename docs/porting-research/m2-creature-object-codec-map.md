# M2 Creature/Object codec map

상태: legacy ABI 조사 및 clone-only slice 설계 (2026-09-02)

이 문서는 `src/files1.c`, `src/files3.c`, `src/mstruct.h`를 실행 가능한
legacy oracle로 대조한 결과다. CDTO 구현은 이 문서의 소비자이며 여기서는
production codec을 변경하지 않는다.

## 핵심 결론

`write_crt/read_crt`와 `write_obj/read_obj`는 field-by-field 포맷이 아니다.
각각 `sizeof(creature)`/`sizeof(object)`만큼 native struct bytes를 먼저
쓰고, native `int` child count와 재귀 object bytes를 이어 붙인다.
따라서 raw 파일은 C compiler/OS/architecture/endianness/padding/pointer
주소에 묶인 ABI artifact다. Rust가 raw blob을 직접 해석하거나 C struct의
`repr(C)` mirror를 persistence DTO로 삼으면 안 된다.

첫 slice의 “lossless”는 raw pointer/padding까지의 byte 동일성이 아니라
gameplay-visible logical field와 ordered inventory graph의 의미 보존이다.
clone-only 경로는 disposable C heap graph에서만 동작하고 live player/file,
socket, DB writer를 건드리지 않는다.

## Raw disk codec

### ObjectV1 source order

`typedef struct object`는 [`src/mstruct.h:130-155`](../../src/mstruct.h#L130-L155)의
순서 그대로 raw prefix에 들어간다.

| 순번 | C field | native type/크기 성격 | CDTO logical mapping | clone 처리 |
| ---: | --- | --- | --- | --- |
| 1 | `name[80]` | fixed char bytes | text/bytes, NUL 뒤 canonical trim | 보존 |
| 2 | `description[80]` | fixed char bytes | text/bytes | 보존 |
| 3 | `key[3][20]` | 3 fixed strings | bytes/text list | 보존 |
| 4 | `use_output[80]` | fixed char bytes | text/bytes | 보존 |
| 5 | `value` | `long` | signed i64 after explicit sign extension | 보존 |
| 6 | `weight` | `short` | signed i16 | 보존 |
| 7 | `type` | `char` | signed/unsigned policy를 명시한 i8/u8 | 보존 |
| 8 | `adjustment` | `char` | i8/u8 | 보존 |
| 9-10 | `shotsmax`, `shotscur` | 2×`short` | i16, i16 | 보존하되 reader가 `shotscur > shotsmax`를 clamp |
| 11-13 | `ndice`, `sdice`, `pdice` | 3×`short` | i16 tuple | 보존 |
| 14-15 | `armor`, `wearflag` | 2×`char` | i8/u8 | 보존 |
| 16-17 | `magicpower`, `magicrealm` | 2×`char` | i8/u8 | 보존 |
| 18 | `special` | `short` | i16 | 보존 |
| 19 | `flags[8]` | fixed bytes | bytes(8), bit positions 고정 | 보존 |
| 20 | `questnum` | `char` | i8/u8 | 보존 |
| 21 | `first_obj` | `otag *` | 없음 | raw 주소 폐기, child list로 재구성 |
| 22 | `parent_obj` | `object *` | 없음 | 폐기, importer가 parent 설정 |
| 23 | `parent_rom` | `room *` | 없음 | 폐기 |
| 24 | `parent_crt` | `creature *` | 없음 | 폐기 |

Disk `write_obj(fd,obj,perm_only)` ([`files1.c:76-109`](../../src/files1.c#L76-L109))는
위 raw prefix 뒤에 native `int cnt`를 쓰고 `first_obj` linked list를 재귀
순서로 쓴다. `perm_only == 0`이면 모든 child, 아니면 `OPERMT` child만
선택한다. `count_obj`와 실제 loop가 관찰한 list가 달라지면 `-1`이다.

`read_obj` ([`files1.c:398-454`](../../src/files1.c#L398-L454))는 raw prefix를
읽은 직후 `first_obj`, 세 parent pointer를 0으로 덮고, 각 child를 allocate해
`parent_obj`를 재구성한다. `shotscur > shotsmax`는 max로 clamp한다. count가
음수거나 `MAX_NESTED_OBJECTS=4096`보다 크면 error다. short raw/count/read
및 nested parser error는 `-1`로 반환한다. allocation failure는 legacy
`merror(..., FATAL)` 경로가 있어 clone importer가 그대로 재사용해서는 안 된다.

### CreatureV1 source order

`typedef struct creature`는 [`src/mstruct.h:182-242`](../../src/mstruct.h#L182-L242)의
순서다. `write_crt`는 이 전체 struct를 쓴 뒤 inventory count와 object tree를
붙인다.

| 순번 | C field | native type/크기 성격 | CDTO logical mapping | clone 처리 |
| ---: | --- | --- | --- | --- |
| 1-5 | `name`, `description`, `talk`, `password`, `key[3][20]` | fixed char/string bytes | text/bytes; password는 private sensitive bytes | semantic clone은 보존하되 log/DB shadow에서 redaction |
| 6 | `fd` | `short` socket fd | 없음 | runtime-only 폐기, importer는 -1 |
| 7-11 | `level`, `type`, `class`, `race`, `numwander` | `unsigned char` + 4×`char` | fixed-width scalar | 보존 |
| 12 | `alignment` | `short` | i16 | 보존 |
| 13-17 | `strength`, `dexterity`, `constitution`, `intelligence`, `piety` | 5×`char` | fixed-width scalar | 보존 |
| 18-21 | `hpmax`, `hpcur`, `mpmax`, `mpcur` | 4×`short` | i16 | reader가 current > max를 clamp |
| 22-23 | `armor`, `thaco` | 2×`char` | i8/u8 | 보존 |
| 24-25 | `experience`, `gold` | 2×`long` | signed i64 with source-width metadata | 보존 |
| 26-29 | `ndice`, `sdice`, `pdice`, `special` | 4×`short` | i16 | 보존 |
| 30 | `proficiency[5]` | 5×`long` | ordered signed i64 list | 보존 |
| 31 | `realm[4]` | 4×`long` | ordered signed i64 list | 보존 |
| 32-34 | `spells[16]`, `flags[8]`, `quests[16]` | fixed bytes | bytes with bit positions fixed | 보존 |
| 35 | `questnum` | `char` | i8/u8 | 보존 |
| 36 | `carry[10]` | 10×`short` | ordered i16 list | 보존 |
| 37 | `rom_num` | `short` | i16 | 보존 as source location number |
| 38 | `ready[MAXWEAR]` (`MAXWEAR=20`) | 20×`object *` | 없음 | raw addresses 폐기; save path first un-readies them into inventory |
| 39 | `daily[10]` | `{char max,char cur,long ltime}`×10 | ordered fixed subrecords; ltime→i64 | 보존 |
| 40 | `lasttime[45]` | `{long interval,long ltime,short misc}`×45 | ordered fixed subrecords | 보존 |
| 41 | `following` | `creature *` | 없음 | 폐기 |
| 42 | `first_fol` | `ctag *` | 없음 | follower graph는 이 codec에 없음 |
| 43 | `first_obj` | `otag *` | `inventory` child list 하나로 표현 | raw 주소 폐기, ordered children으로 재구성 |
| 44 | `first_enm` | `etag *` | 없음 | enemy runtime graph 폐기 |
| 45 | `first_tlk` | `ttag *` | 없음 | talk runtime graph 폐기 |
| 46 | `parent_rom` | `room *` | 없음 | importer가 별도 location 입력으로 설정 |

`write_crt(fd,crt,perm_only)` ([`files1.c:152-184`](../../src/files1.c#L152-L184))는
raw prefix 다음 native `int count_inv()`와 child object를 쓴다. player
save는 `perm_only=0`이므로 inventory 전체가 대상이다. 일반 `savegame`은
[`src/command8.c:717-805`](../../src/command8.c#L717-L805)에서 duplicate를
만들고 ready item을 inventory로 옮긴 뒤 `save_ply`를 호출한다. 따라서
`ready[]`는 player file의 독립 logical field가 아니며 첫 slice에도 넣지
않는다.

`read_crt` ([`files1.c:465-527`](../../src/files1.c#L465-L527))는
`first_obj`, `first_fol`, `first_enm`, `parent_rom`, `following`, `ready[20]`를
scrub하고 inventory child의 `parent_crt`를 설정한다. **`first_tlk`는 scrub하지
않는 bug/ABI hazard**다. raw pointer를 CDTO로 복사하지 말고 importer가 항상
0으로 초기화해야 한다. count는 `MAX_NESTED_OBJECTS=4096`까지이며 current
HP/MP를 max로 clamp한다.

## Memory codec은 disk codec과 동일하지 않다

`src/files3.c:26-248`의 `write_*_to_mem/read_*_from_mem`은 같은 raw
`sizeof(struct)` + native `int` + recursion 모양이지만 다음 차이가 있다.

- memory writer의 `perm_only != 0` child 조건은 `OPERMT || OPERM2`다. 그러나
  `count_obj`/`count_inv`는 `OPERMT`만 세므로 OPERM2가 있는 perm-only graph는
  count와 실제 write가 어긋날 수 있다.
- memory reader는 input length를 받지 않는다. count/child recursion에 byte
  boundary가 없고 negative/huge count guard가 disk reader처럼 완전하지 않다.
  외부 CDTO/네트워크 decoder로 재사용하면 안 된다.
- `COMPRESS` build의 `file_player_store`가 fixed 50 KiB/100 KiB buffer에
  `write_crt_to_mem/read_crt_from_mem`을 호출한다. build flag와 zlib path는
  별도 ABI/size risk로 fixture에 기록한다.
- memory writer/reader도 pointer-bearing raw prefix를 복사하므로 CDTO
  exporter가 직접 호출할 수 있는 canonical serializer가 아니다.

## Nested graph와 최대치

| 그래프 | legacy disk cap | 순서/소유권 | CDTO 처리 |
| --- | ---: | --- | --- |
| object children | 4,096 per reader recursion budget | linked-list 순서, 각 child 1 parent | ordered `children` list; cycle/multiple parent reject |
| creature inventory | `count_inv`는 최대 200으로 clamp (`files1.c:119-140`), reader raw count는 4,096까지 허용 | `first_obj` 순서; ready는 save 전에 inventory로 풀림 | `inventory` ordered list; exporter가 count discrepancy 기록 |
| room monsters | 4,096 | room list; nested creature each has own inventory | 첫 slice 제외 |
| room objects | 8,192 | room list; each nested object | 첫 slice 제외 |
| room exits | 200 | exit list | 첫 slice 제외 |
| room descriptions | 1 MiB each | length-prefixed `short_desc`, `long_desc`, `obj_desc` | 첫 slice 제외 |

`write_obj`/`write_crt` 자체는 write-side count를 별도 cap하지 않는다. source
graph를 export하기 전에 CDTO exporter가 depth, node count, aggregate byte
budget를 적용해야 한다. recursion depth도 4,096 node cap과 별도의 stack-safe
limit를 둔다. OOM/FATAL은 DTO rejection으로 바꿔야 하며 process exit를
정상 경로로 허용하지 않는다.

## ABI 의존성과 비결정 요소

- `long`의 폭(현재 macOS LP64 probe: 8), `short`/`int`의 폭, signed `char`,
  struct alignment/padding이 raw prefix와 배열 크기에 영향을 준다.
- pointer 폭/주소, endian, compiler packing, build flags(`COMPRESS`, charset,
  platform macros)가 raw bytes를 바꾼다. 현재 CDTO ABI fingerprint는
  `sizeof(long)`, pointer/object/creature/room/iobuf size와 일부 pointer
  `offsetof`를 field IDs 1-22로 기록하지만, logical field order는 별도
  source contract test가 감시해야 한다.
- native `int` child count는 endian/width 의존이다. CDTO에서는 모든 count와
  scalar를 명시적 BE fixed-width로 encode한다.
- struct padding/uninitialized tail bytes와 pointer bytes는 nondeterministic다.
  fixed char array는 first NUL 뒤 canonical zero/trim 정책을 적용하며 raw
  padding은 비교 대상이 아니다.
- linked-list order는 현재 heap graph의 insertion/order에 따라 달라질 수
  있지만 serializer가 관찰한 order를 보존한다. canonical DTO는 exporter가
  source list order를 명시적으로 기록하고, unordered collection sort를
  임의로 적용하지 않는다.
- `savegame`의 ready→inventory 변환과 `add_obj_crt` ordering, `count_inv`
  clamp가 snapshot 결과에 영향을 준다. clone fixture는 raw file만이 아니라
  pre-save logical graph와 unready policy도 기록한다.
- `time(0)`, `rand`, socket fd, room/session links는 이 codec field가 아니며
  첫 slice digest에서 제외한다. time/RNG trace는 후속 command differential
  slice에서 별도 adapter로 기록한다.

## CDTO clone-only 최소 slice

### 경계

첫 slice는 다음 pipeline만 허용한다.

```text
disposable player raw file
        │ C ABI load_ply/read_crt
        ▼
validated native creature + ordered object graph
        │ C exporter (pointer scrub, bounds, canonical scalars)
        ▼
CDTO kind=CREATURE + nested ObjectV1 bytes
        │ Rust validate/canonicalize/decode/encode
        ▼
CDTO exact digest
        │ C importer clone (temporary heap only)
        ▼
export again → same logical digest
```

`CreatureV1`은 위 표의 모든 gameplay-visible scalar/fixed array, `daily[10]`,
`lasttime[45]`, `carry[10]`, `rom_num`, ordered inventory를 포함한다.
`ObjectV1`은 scalar/fixed array와 ordered recursive children을 포함한다.
legacy password는 private clone fixture에서만 opaque sensitive bytes로
보존하고, golden/log/shadow/DB에는 redacted한다. importer는 password를
shared state로 복사하지 않는다.

제외하는 것은 `fd`, `ready[*]`, `following`, follower/enemy/talk linked
lists, `parent_rom`, 모든 `otag/ctag/ttag` 주소, struct padding과 room graph다.
이 제외가 의미를 잃게 만드는 command는 첫 slice 밖이다. 특히 equipped
state는 savegame duplicate의 unready normalization 결과로만 다룬다.

### 권장 logical field order

CDTO field IDs는 source declaration order를 그대로 복제하지 않아도 되지만,
한 번 정하면 오름차순/중복 금지로 고정한다. 첫 구현 제안은 다음과 같다.

- `ObjectV1`: 1 name, 2 description, 3 key[3], 4 use_output, 5 value,
  6 weight, 7 type, 8 adjustment, 9 shotsmax, 10 shotscur, 11-13 dice,
  14 armor, 15 wearflag, 16 magicpower, 17 magicrealm, 18 special,
  19 flags, 20 questnum, 21 children.
- `CreatureV1`: 1 name, 2 description, 3 talk, 4 legacy_password(private),
  5 key[3], 6-10 identity scalars, 11 alignment, 12-16 stats, 17-20 hp/mp,
  21-22 armor/thaco, 23-24 experience/gold, 25-28 dice/special,
  29 proficiency, 30 realm, 31 spells, 32 flags, 33 quests, 34 questnum,
  35 carry, 36 rom_num, 37 daily, 38 lasttime, 39 inventory.

Nested `children`/`inventory`는 CDTO field header 하나의 canonical bytes
payload로 감싼다. 내부 list count, depth, child field encoding은 Terra의
codec implementation에서 별도 versioned subcodec로 확정하되 raw C bytes를
그대로 복사하지 않는다.

## RED → GREEN test design

### Required fixtures

1. empty creature/inventory: all pointers non-zero poison values before export;
   output에는 pointer/fd가 없고 import 후 parent pointers만 재구성한다.
2. scalar boundary: signed long/short min/max, UTF-8 and embedded NUL fixed
   arrays, all flags/spells/quests/daily/lasttime slots.
3. nested object: deterministic child order, depth 1/2/limit, duplicate names,
   container shots clamp.
4. malformed legacy: short struct, short count, negative/4097 child count,
   truncated nested child, invalid `shotscur`, pointer-looking bytes, and
   `first_tlk` non-zero to prove importer scrub.
5. save-normalized player: ready slots populated, duplicate/unready result
   compared with `savegame` semantics; ready pointers never appear in DTO.
6. ABI variants: current LP64 fingerprint plus a fixture metadata record for
   32-bit/Windows historical files; a mismatched fingerprint is rejected or
   routed through an ABI-specific C exporter, never guessed by Rust.

### Assertions

RED tests must first fail if a field is reordered/added, a pointer field enters
the DTO, a child is sorted instead of source order, or a malformed count is
accepted. GREEN requires:

- `C load → export → decode → encode → decode` semantic equality and canonical
  byte idempotence;
- `C export → Rust validate/canonicalize → C import clone → C export` equal
  digest, with original native graph/file unchanged;
- empty/nested inventory and all scalar arrays round-trip;
- `first_tlk`, `ready`, parent links, fd and every raw pointer are absent from
  CDTO and safely initialized only by importer policy;
- malformed/truncated/oversize/depth/cycle input returns a structured error,
  frees temporary allocations, and never invokes global process exit;
- C and Rust reject duplicate/out-of-order CDTO fields identically and agree on
  fixed-width endian encoding.

The first static contract test is
[`tests/unit/creature_object_layout_contract_test.py`](../../tests/unit/creature_object_layout_contract_test.py).
It extracts the declaration order from `mstruct.h`, compares it to the frozen
expected lists above, and asserts that the raw serializers still use whole
`sizeof(object/creature)` prefixes. This makes a source field reorder/addition
an immediate RED before Terra implements a codec against stale assumptions.

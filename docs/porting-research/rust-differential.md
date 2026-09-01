# Terra C → Rust 차등 포팅 설계

## 결정

전체 재작성은 하지 않는다. C 프로세스가 저장소, 소켓, `select` 루프와 실제 게임 상태의 유일한 권위자(oracle)로 남고, Rust는 **버전이 있는 canonical DTO와 순수 전이 함수**를 통해 한 모듈씩 검증한 뒤에만 경계 안으로 들어간다. 이 문서의 DTO는 C 구조체의 `repr(C)` 사본도, raw 파일의 새 이름도 아니다. 포인터, 컴파일러 padding, `long` 폭, 주소값을 버린 논리적 데이터 계약이다.

첫 구현은 라이브 경로를 바꾸지 않는 오프라인 player/inventory 왕복 검사다. 이후에도 C `select`/Telnet 루프는 마지막까지 유지하고, Rust에는 C 포인터를 건네지 않는다.

## 조사한 현재 oracle

| 영역 | 관찰 | 이식 제약 |
| --- | --- | --- |
| 저장 ABI | `object`, `creature`, `room`, `exit_`를 `sizeof`만큼 직접 읽고 쓴다. `write_obj`/`write_crt`는 raw struct 뒤에 `int` 개수와 재귀 객체를 붙이고, `write_rom`은 raw room, exit/monster/object 수와 그래프, 설명 문자열을 차례로 쓴다. | Rust가 기존 raw 바이트를 직접 해석하는 방식은 금지한다. 원래 ABI에서 컴파일된 C exporter가 읽는다. |
| 포인터 | raw struct 안에 `first_*`, `parent_*`, `ready`, 설명 포인터, 함수 포인터까지 들어 있다. loader는 일부 runtime link를 0으로 지우고 parent를 재구성한다. object catalog도 읽은 뒤 네 pointer를 0으로 지운다. | raw 파일은 주소·padding을 포함하는 로컬 ABI artifact다. address byte 동등성은 correctness 기준이 될 수 없다. |
| ABI 예 | 현재 macOS LP64 probe는 `long=8`, pointer=8, `object=376`, `room=760`, `creature=1952`, `iobuf=9312` byte를 보고했다. 객체 pointer 영역은 offset 344부터, creature runtime link는 1904부터다. 빈 inventory player 파일의 최소 크기 `1956`은 `sizeof(creature)+sizeof(int)`와 맞는다. | historical 32-bit/Windows/raw fixture를 이 수치로 해석하지 않는다. exporter 실행 image마다 ABI fingerprint를 함께 남긴다. |
| 캐시와 전역 | `Ply[PMAX]`, `Spy`, `Shutdown`, 명령/스펠 테이블, family 값이 전역이다. `files2.c`의 Room/Creature/Object sparse cache와 LRU queue도 process-global이며 room은 player가 있으면 eviction하지 않는다. | Rust 상태는 명시적인 `World`/`Session`/`Catalog` 입력으로 만들고, 전역 주소나 fd를 entity identity로 쓰지 않는다. |
| loop | `sock_loop()` 순서는 shutdown/reap → `io_check()` → `output_buf()` → `handle_commands()` → `update_game()`이다. `io_check()`는 75ms `select` timeout, `handle_commands()`는 fd 순서로 명령 하나씩 처리하며, output은 ring buffer와 interrupt bit에 의존한다. | event ordering, fd 순서, CRLF output, 입력 ring overflow 정책은 관찰 가능한 동작이다. async Rust runtime으로 먼저 바꾸지 않는다. |
| 시간/RNG | 시작 시 `srand(getpid()+time(0))`; `update_game()`은 초 단위로 여러 schedule을 실행한다. 수십 C file이 직접 `time(0)`을 호출한다. `mrand(a,b)`는 `rand()%((b-a+1)*10)/10+a`라는 legacy macro이고 `dice`도 이를 호출한다. | 현대 Rust RNG/range distribution으로 대체하면 call-count와 편향 모두 달라진다. replay에는 raw `rand()` 값과 모든 time read를 기록한다. |
| 기존 Rust seam | `rust/muhan-resource-index`와 `muhan-resource-ffi`는 path alias TSV만 처리하며 C는 `USE_RUST_RESOLVER`에서 이를 선택적으로 호출한다. | 이 seam의 “byte request/response, C fallback, feature-flag” 원칙은 재사용하되 global `OnceLock` map을 game-world 상태의 모델로 확대하지 않는다. |

근거 위치: [`src/mstruct.h`](../../src/mstruct.h), [`src/files1.c`](../../src/files1.c), [`src/files2.c`](../../src/files2.c), [`src/files3.c`](../../src/files3.c), [`src/file_player_store.c`](../../src/file_player_store.c), [`src/io.c`](../../src/io.c), [`src/update.c`](../../src/update.c), [`src/main.c`](../../src/main.c), [`src/mtype.h`](../../src/mtype.h), [`src/resource_path.c`](../../src/resource_path.c) 및 현재 focused serializer/catalog tests다.

특히 기존 reader의 pointer scrub은 compatibility 복구이지 format 정의가 아니다. `read_crt()`는 follower/enemy/talk/room/following/ready pointer를 지우고, `read_obj()`는 containment와 parent pointer를 지운다. `load_crt()`의 cached struct copy는 이 모든 link를 다 scrub하지 않는다. 따라서 raw struct를 DTO로 `memcpy`하거나 raw pointer 값을 golden에 넣는 것은 위험하다.

## 목표 경계

```text
legacy raw file / C heap
          │ C exporter: walk + validate + remove runtime links
          ▼
  CDTO v1 canonical bytes ─── SHA-256 ─── golden / replay evidence
          │                              ▲
          ├── Rust decode → pure transition → canonical encode
          │                              │
          └── C importer ← validated CDTO ┘
                         allocate → link → native C only

C owns sockets, file replacement, authoritative command execution until each cutover gate passes.
```

FFI ABI는 오직 opaque byte slice와 status/error buffer다. 예를 들어 C가 소유한 `mdto_export_creature`, `mdto_import_creature`, Rust가 소유한 `mdto_validate`, `mdto_apply`는 `(const uint8_t*, size_t) -> owned byte buffer` 형태로 교환하고 각 allocator/free pair를 명시한다. `creature *`, `room *`, `object *`, `iobuf *`, Rust reference, callback function pointer는 경계를 넘지 않는다. exporter/importer 자체는 C에 두어 legacy raw ABI를 그 ABI에서만 다룬다.

초기에는 exporter가 main loop의 명령 경계에서만 실행돼야 한다. C는 single-threaded이므로 별도 thread가 heap graph를 walk하면 안 된다. snapshot에는 monotonically increasing `snapshot_seq`, source ABI fingerprint, world/session scope, canonical digest를 붙여 trace 상의 어느 전이 전후인지 구분한다.

## CDTO v1: canonical DTO 계약

### Envelope와 wire 규칙

CDTO v1은 명시적인 binary wire format이다. C와 Rust가 각각 독립 구현하며 raw memory layout, host endian, `sizeof`, `time_t`, Rust `usize`를 쓰지 않는다.

```text
magic[8] = "MUHCDTO\0"
wire_version: u16 big-endian = 1
kind: u16 big-endian
payload_length: u32 big-endian
payload: canonical fields
sha256(payload): 32 bytes
```

- payload field는 `(field_id: u16 BE, type: u8, length: u32 BE, bytes)`이고 `field_id` 오름차순으로 정확히 한 번만 쓴다. 알려지지 않은 optional field는 decoder가 보존 가능한 extension bag으로 round-trip한다. 같은 field를 두 번 쓰거나 순서가 틀리면 거부한다.
- signed integer는 고정 폭 two's-complement (`i8/i16/i32/i64`), unsigned는 고정 폭 BE다. ABI 의존 `long`/enum/bitfield는 반드시 DTO의 정확한 폭으로 매핑한다. 범위를 넘으면 clamp하지 말고 export/import 오류로 기록한다.
- bytes와 text를 구분한다. legacy fixed char array는 NUL 뒤 쓰레기를 버린 `bytes` 또는 strict UTF-8 `text`로 export한다. 유효하지 않은 UTF-8은 원본 bytes와 `legacy_encoding` diagnostic으로 보존하고, 조용히 replacement character로 바꾸지 않는다.
- `flags[8]`, `spells[16]`, `quests[16]`는 bit position을 바꾸지 않는 fixed byte array다. `None`과 empty string/list, absent optional field를 서로 다르게 인코딩한다.
- list는 source list 순서를 유지한다. unordered map은 UTF-8 bytewise key order, entity list는 stable `entity_id` order다. canonical encoder는 입력 순서와 allocator 주소에 상관없이 같은 bytes를 낸다.
- decoder의 size/depth/count 예산은 C reader의 현재 caps 이상으로 명시한다: nested object 4,096, room exit 200, room monster 4,096, room object 8,192, description 1 MiB. envelope/payload 전체 upper bound도 kind별로 둔다.

### 논리 타입

`CreatureV1`은 name/description/talk/password/key bytes, level/class/race/type, stats, gold/experience, proficiency/realm arrays, spell/flag/quest bytes, `daily[10]`, `lasttime[45]`, `carry[10]`, room number와 inventory를 담는다. `fd`, `ready[*]` pointer, following/follower/enemy/talk linked lists, `parent_rom`은 runtime-only라서 persistent DTO에 그대로 넣지 않는다.

`ObjectV1`은 object scalar/fixed arrays와 순서 보존 `children: Vec<ObjectV1>`만 가진다. parent link는 child placement에서 importer가 재구성한다. `RoomV1`은 room scalars, descriptions, exits, permanent monsters, permanent objects를 가지고 active players와 in-process cache/LRU links는 제외한다. `ExitV1`, `DailyV1`, `LastTimeV1`, `BoardIndexV1`도 각각 fixed-width type이다. `BOARD_INDEX`는 현재 256-byte raw record이므로 board도 별도 DTO로 만들며 그것을 Rust struct mirror로 읽지 않는다.

cross-object 참조가 필요한 후속 module에는 snapshot-local `EntityId(u64)`를 exporter traversal 순서로 배정한다. DTO에 C address/fd가 아닌 id를 적고, importer는 두 phase로 (1) allocate/scalar decode, (2) id resolve/link)한다. unresolved id, duplicate id, containment cycle, multiple parent, duplicate wear slot은 reject한다. session id와 socket fd는 `SessionV1`의 별도 ephemeral scope에 속하고 persistence DTO에 들어가지 않는다.

### exporter/importer 책임

| 단계 | C exporter | C importer |
| --- | --- | --- |
| legacy read | 기존 `read_*`/`load_*`로 original ABI blob을 C heap graph로 만든다. raw file byte를 Rust에 노출하지 않는다. | raw legacy file import는 하지 않는다. 필요하면 original ABI C exporter를 먼저 거친다. |
| graph | count/depth/parent invariants를 검사하고 logical fields만 emit한다. pointer raw bits, cached runtime state, fd를 배제한다. | DTO를 전부 validate한 뒤 native allocators로 임시 graph를 만들고 parent/container/readied links를 재결합한다. |
| failure | field path, invariant, ABI fingerprint을 structured diagnostic에 남기고 snapshot을 publish하지 않는다. | partially built graph를 free하고 기존 authoritative graph/file을 건드리지 않는다. |
| commit | read-only. live mutation 없이 digest/evidence를 반환한다. | first slice에서는 clone only다. later module도 validate → temporary graph → atomic C-side swap 순서로만 활성화한다. |

`write_crt`/`write_obj`를 CDTO exporter로 재사용하지 않는다. 그것들은 pointer-bearing legacy writer다. 반대로 CDTO importer가 legacy persistence를 처음부터 대체하지 않는다. cutover 전에는 importer가 만든 native graph를 existing C writer로 저장하고, reload 후 CDTO digest로 의미적 동등성을 확인한다.

## 결정적 clock/RNG adapter

`Clock`은 `now_seconds() -> i64`, `local_calendar(now) -> Calendar`를 제공한다. `Rng`은 `next_legacy_rand() -> u32`와 `mrand(a,b)`를 제공한다. production-compatible C oracle trace에는 다음을 모두 event stream으로 쓴다.

```text
ClockRead { call_site_id, unix_seconds }
RandDraw  { call_site_id, libc_rand_return }
```

Rust replay는 raw draw stream을 소비해 C macro를 문자 그대로 계산한다.

```text
if a > b: a
else: a + (draw % ((b - a + 1) * 10)) / 10
```

이 방식은 Darwin/glibc의 `rand()` algorithm 차이와 modulo bias를 “고치지” 않고 oracle로 보존하며, C/Rust가 소비한 draw 수와 call-site order까지 비교할 수 있다. `dice`는 그 위에 동일하게 n번 `mrand(1,s)`를 호출한다. seed만 기록해서 다른 libc에서 재생하는 방식은 금지한다.

test-only compatibility build에서는 모든 `time(0)`/`localtime`/`ctime`과 `rand` call을 wrapper 또는 macro shim으로 우회해 trace한다. source-wide direct `time(0)` call을 한 번에 Rust clock으로 치환하지 않는다. 우선 `main`, `io`, `update`, vertical slice module의 call site에 id를 부여하고, trace lint가 새 direct call을 막는다. time trace가 부족하거나 draw under/over-consume하면 comparison은 **inconclusive가 아니라 failure**다.

## 검증 계층

### Golden master

fixture는 비밀 player file을 넣지 않는다. CI가 disposable `MUHAN_HOME`에서 C로 만든 sanitized fixture와 metadata를 version control한다.

- source provenance: git SHA, compiler/container image digest, OS/arch, `sizeof` 및 key `offsetof`, `CFLAGS`, raw input SHA-256
- expected: CDTO exact bytes/SHA-256, redacted semantic JSON for review, expected error class, output wire bytes, clock/RNG trace
- persistence fixture: raw file의 pointer bytes는 gold 기준이 아니다. `C raw → export → DTO` digest, `DTO → C clone → export` digest, C legacy write/reload 후 digest를 비교한다.
- every production bug gets a minimal raw/DTO/trace regression fixture before a Rust behavior change is accepted.

기존 [`tests/unit/files1_serializer_test.c`](../../tests/unit/files1_serializer_test.c), [`tests/unit/files2_load_obj_test.c`](../../tests/unit/files2_load_obj_test.c), player store/recovery tests와 [`tests/harness/run_scenario.py`](../../tests/harness/run_scenario.py)의 disposable runtime evidence를 seed로 사용한다. 현재 serializer test는 short write/EINTR/ENOSPC propagation을, catalog test는 stale object runtime pointer scrub과 short read를 이미 고정한다.

### Property, fuzz, model, differential

| 층 | 반드시 검증할 property |
| --- | --- |
| DTO codec | `decode(encode(x)) == x`; canonical encode idempotence; unknown optional field preservation; malformed/duplicate/out-of-order/truncated/oversize input 거부 |
| C boundary | `C native → CDTO → C clone → CDTO` digest 같음; importer 실패는 original graph/file 불변; parent/ready links는 rehydrated, runtime pointer bytes는 DTO에 없음 |
| graph fuzz | nested object depth/count, cyclic/multiple-owner reference, invalid UTF-8/NUL, integer boundaries, corrupt counts/lengths, stale pointer-looking bits. C importer는 ASan/UBSan 아래 crash/leak 없이 reject해야 한다. Rust는 `cargo fuzz`/Miri 가능 범위에서 같은 corpus를 쓴다. |
| model-based | 명시적 model이 session states, command queue, save failure/recovery, disconnect, tick를 생성한다. fd-order와 command/tick interleaving을 바꿔 C oracle과 Rust outcome/state/output/trace를 비교한다. |
| differential | 같은 pre-state CDTO, command bytes, event order, clock trace, raw RNG trace를 C oracle와 Rust slice에 넣고 post-state CDTO digest, output byte frames, error/status, clock/RNG consumption을 exact compare한다. |

fuzzer가 raw C ABI file을 Rust에 직접 주입하는 것은 하지 않는다. raw file fuzz는 original-ABI C loader/exporter hardening test이고, DTO fuzz는 language-neutral bridge test다. crash, timeout, allocation cap breach, accepted noncanonical encoding은 모두 failure다.

### Shadow replay

shadow는 C가 먼저 authoritative command를 처리한 뒤 실행한다. hook은 command 전 CDTO snapshot, command bytes, dispatch ordering key, clock/RNG trace와 C output frames를 capture해 Rust pure slice를 **격리된 clone**에서 재생한다. Rust output/state digest는 log/metric/evidence에만 쓰며 socket, player file, queue, DB, metrics decision에 영향을 주지 않는다.

비교 key는 `(build_sha, schema_version, slice_id, snapshot_seq, session_trace_id, command_index)`다. diff record에는 first differing DTO field path, first differing output byte offset, clock/RNG call-site divergence, C/Rust digest만 넣고 password/admission ticket/player secret은 넣지 않는다. sampling은 처음에는 all test/replay traffic, live 도입 시에는 authorization approval 뒤 read-only mirror traffic으로 확대한다. `shadow mismatch`, trace desync, exporter failure는 silent fallback이 아니라 cutover-blocking alert다.

## 안전한 이식 순서

1. **Contract foundation**: CDTO v1 spec, ABI fingerprint exporter, canonical C/Rust codec, fixture generator, redaction, trace schema. No live behavior change.
2. **첫 vertical slice: player + nested inventory snapshot**: disposable C runtime에서 `load_ply` → C exporter → Rust validate/canonicalize → C importer clone → C exporter를 수행한다. `CreatureV1`과 `ObjectV1`만 포함하고 source player file, live `Ply`, socket, save path에는 쓰지 않는다. nested containers와 empty inventory, corrupt/truncated file, stale pointer-bearing catalog fixture를 golden/property/fuzz한다.
3. **catalog templates**: object then creature catalog reader/exporter를 추가하고 C의 pointer scrub/LRU semantics를 explicit diagnostic과 DTO invariant로 만든다. cache replacement는 아직 C가 한다.
4. **room persistence**: exit, descriptions, permanent contents, room graph를 이식한다. read/write failure, capacity/eviction, backup/restore properties를 C oracle differential로 먼저 고정한다.
5. **pure command slices**: parser-normalized input과 state/output contract를 써서 read-only display commands부터 시작한다. 이어서 one-command mutation을 C-apply/Rust-shadow로 추가한다. command table, `RETURN` continuation, alias expansion, output ring은 C에 남긴다.
6. **time/RNG update slices**: update scheduler 하나씩, then combat/random effects. 각 slice는 recorded clock/draw trace zero-diff가 선행조건이다.
7. **optional active Rust execution**: 검증된 pure slice만 C adapter가 호출하고 C가 validated result를 apply한다. raw file writer, network loop, authentication/session lifecycle은 별도 zero-diff gate 전까지 C에 남긴다.
8. **loop/storage replacement은 마지막**: state ownership, persistence format migration, reactor replacement은 모든 earlier slice가 stable한 뒤 별도 migration/rollback design으로 다룬다.

이 순서는 object/creature graph라는 가장 큰 ABI 위험을 먼저 작고 write-free하게 다룬다. combat나 `select` loop를 첫 slice로 고르면 ABI, global ordering, output buffering, clock/RNG 차이가 한 diff에 섞여 원인을 판단할 수 없다.

## CI matrix와 cutover gate

| Job | every PR | nightly / release |
| --- | --- | --- |
| C oracle | `make -C src unit-test`, ABI probe, C exporter/importer focused tests, current deterministic session scenario | pinned Linux amd64 replay corpus, historical ABI container fixtures, ASan/UBSan loader/importer run |
| Rust | `cargo fmt --check`, clippy `-D warnings`, unit/property/golden test, FFI ownership/error-path test | cargo-fuzz corpus merge/minimize, prolonged model/replay seeds, Miri where supported |
| cross-language | canonical bytes/digest equality, malformed DTO corpus both reject, round-trip clone equality | all traces C-vs-Rust output/state/clock/RNG exact diff, schema compatibility matrix N/N-1 |
| platform | Linux amd64 is the pinned zero-diff oracle | macOS and Linux arm64 compile/semantic tests; they do not bless a different libc RNG sequence as zero diff |

Release/cutover gate for one slice is all of the following, with no allowlist that hides a semantic mismatch.

1. Pinned oracle container에서 모든 golden과 full recorded replay corpus의 post-state CDTO digest, output bytes, result status, clock reads, raw RNG draws가 exact zero diff다.
2. At least 10,000 command/tick transitions across 100 deterministic seeds and all relevant failure fixtures are zero diff; every curated production-regression fixture passes.
3. C/Rust CDTO decode/encode and C clone round-trips pass on every corpus item; malformed input is rejected identically or has a documented safer rejection category approved in the fixture.
4. Sanitizer/property/fuzz jobs have no crash, leak, UB finding, timeout, or allocation-cap bypass. Shadow replay runs for seven consecutive days of representative traffic with zero unexplained mismatch before active execution is enabled.
5. Migration flag defaults to C. Disable path is one config flip back to C and does not require DTO-to-raw conversion or data rollback; C raw persistence remains the recovery authority until storage has its own separately approved migration gate.

An intentional difference must be a new versioned fixture with owner, rationale, user-visible impact, and expiry/review date. It is not added to a generic “known diff” suppression list. A trace gap, changed output newline, extra RNG draw, or different DTO field is a diff even if the game appears to work.

## Immediate work items

1. Add a test-only C ABI probe/export harness that emits the `CreatureV1`/`ObjectV1` CDTO and fingerprint from a disposable player fixture.
2. Create `rust/muhan-core-dto` with the CDTO v1 encoder/decoder, no game heap or async runtime dependency, and FFI ownership tests beside the existing resource resolver workspace.
3. Add golden, property and differential commands to CI, then implement the clone-only first vertical slice. Do not route a production command to Rust until its shadow and zero-diff gate evidence exists.

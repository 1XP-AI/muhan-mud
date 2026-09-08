# Go 게임 서버 기능 원장 (G0 조사)

작성일: 2026-09-08 (KST) · 상태: **전체 인수 전의 보수적 G0 원장**

이 문서는 `docs/porting-research/go-server-execution-plan.md`의 G0 인벤토리다.
레거시 C의 등록 명령, 월드/전투/저장 코드를 source-backed로 열거하고, 기존
테스트를 Go 이전 작업의 oracle 후보로 매핑한다. 이 문서의 어느 행도 Go 기능이
구현되었거나 인수되었다는 뜻이 아니다.

중요: 이 원장의 `인수 상태=미착수`는 **작업 트리에서 구현물이 사라졌다는 뜻이
아니다**. 전체 명령 집합의 최종 인수 기준을 아직 충족하지 않았기 때문에 명령
행 단위 상태를 보수적으로 유지하는 것이다. 현재 실제 구현·검증된 Go 수직
슬라이스(방향 입력/영속 이동 receipt, 도착 함정, 플레이어 추종 관계 및 재귀 이동,
mixed `first_fol` 순서와 MDMFOL NPC 이동, TRAP_ALARM due catalog spawn, committed
movement room-event fan-out, canonical NPC ID/active 순서, 로그아웃 NPC 적대 정리,
PG replay/race 검증)는
`HANDOFF.md`와 `server/README.md`의 날짜순 실행 기록을 기준으로 확인한다.
추가로 player phase의 `UpdatePlayer` 영속 receipt/replay slice도 같은 실행 기록과
`TestWorldConnectorPlayerVitalPhasePostgresPersistsAndReplays`에서 확인한다. 이는
전체 `update.c` scheduler 인수가 아니다.
NPC 전투의 첫 slice(`공격` 대상 선택·THAC0/피해·critical·enemy relation·PG replay)와
lethal NPC 전이는 `TestPostgresAttackCommandPersistsAndReplays`/`TestPostgresLethalAttackCommandPersistsNPCDeath`/
`TestPostgresLethalAttackCommandPersistsNPCDrops`로
확인한다. lethal 경계는 NPC 제거, active/enemy/follower 정리, XP/성향·quest/proficiency,
금화·legacy drop graph의 canonical floor 이동, permanent timer를 한 후보로 저장하며,
allocator가 없거나 summon side effect가 필요한 경우 fail-closed한다. PVP·도망·소환
생성·전체 전투 명령 인수로는 아직 승격하지 않는다. 별도 무기 slice는
치명타 파괴, 일반 명중 시 드롭, 다음 swing의 shots 감소와 NPC 피해 비례 숙련도 보상을
`TestPostgresAttackCommandPersistsWeaponDrop` 및 world TDD로 검증했지만, 이것만으로
전체 전투 행을 `완료`로 올리지는 않는다. PUPDMG의 추가 swing 수와 lethal 중단도
`TestPostgresAttackCommandPersistsPowerDamageSequence`로 실제 PG replay를 검증했다.
`건강`/`점수` numeric status receipt도 blind/replay 회귀로 확인하지만, 전체 상태 명령
및 ANSI/title parser 인수로 승격하지 않는다.
`정보`의 첫 페이지 통계(이름·레벨·종족·직업·성향·접속시간·능력치·HP/MP·경험치·돈·
방어력·무게/개수·무기/마법 숙련도)도 `ExecuteInfoLine`과 `PlayerInfo`의 pure
read-only receipt로 연결하고 local race/vet/transport replay를 통과했다. canonical
Items·정의된 class/race/proficiency가 없으면 fail-closed하며, C의 title 계산과
`[엔터]` 후 `info_2` 주문 continuation은 아직 구현하지 않았다. 따라서 이 행의 전체
`info` 인수 상태는 계속 `미구현`이다.
`시간` bare 명령은 게임 시각과 PST wall-clock을 request에 고정한 read-only receipt로
연결했고 동일 command ID replay에서 시계를 다시 읽지 않는다. `도움말`/`?`도
프로세스가 주입한 UTF-8 `help/` 문서를 receipt로 읽는 첫 slice를 추가했다. bare
`helpfile`, `주술`/`정책`, 현재 Go handler가 있는 명령 주제의 `help.<cmdno>`를
지원하고, unknown topic은 C의 고정 no-help 응답을 저장한다. 누락/비 UTF-8 문서는
추정하지 않고 fail-closed하며, 동일 command ID replay와 state purity를 검증한다.
전체 C alias/약어, continuation prompt, title 출력 및 parser parity는 별도 후속이다.
`따라 <플레이어>`와 `내보내` 관계 명령도 same-room exact-name 및 reciprocal replay
회귀를 통과했다. `내보내`의 자기 leader 이탈과 지정 follower 해제 경로까지 연결했지만,
ARM64 PostgreSQL 17 `TestPostgresFollowAndLoseCommandPersistsAndReplays`도 통과했다.
전체 follow/lose/NPC follower 명령 인수로 승격하지 않는다.
`소지품`과 `장비`/`장` read-only receipt도 canonical item/ready 순서 및 blind/invisible
경계를 검증하고 ARM64 PostgreSQL 17 `TestPostgresItemsCommandPersistsAndReplays`를
통과했지만, item mutation과 전체 parser 인수로 승격하지 않는다.
`말`/따옴표 say receipt와 same-room 비동기 fan-out, 침묵·hidden 상태 경계도
`TestPostgresSayCommandPersistsAndReplays` 및 transport 회귀로 확인했지만, 전체
대화/DM/broadcast formatting 인수로 승격하지 않는다.
`누구`/`그룹` read-only receipt도 deterministic online 목록, visibility 및 mixed
`first_fol` 순서와 ARM64 PostgreSQL 17 `TestPostgresSocialCommandPersistsAndReplays`로
확인했지만, C 출력 동등성·group mutation 전체 인수로 승격하지 않는다.
`주워`/`버려` 계열 root transfer mutation도 nested ID 보존·blind/invisible 경계와
ARM64 PostgreSQL 17 `TestPostgresItemMutationCommandPersistsAndReplays`로 확인했지만,
전체 item mutation 및 무게/경비 규칙 인수로 승격하지 않는다.
`입어`/`쥐어`/`무장`/`벗어` 장비 mutation도 canonical inventory root와 20개 ready
slot 사이를 원자적으로 이동하고, 슬롯 충돌·파손·저주·기본 직업/성향/크기/무기 제한과
AC/THAC0 재계산을 적용한다. `TestPostgresEquipmentCommandPersistsAndReplays`가
ARM64 PostgreSQL 17에서 저장·동일 command ID replay를 통과했지만, `모두` 일괄 명령,
전체 C class/race/quest/event/charge 예외, 장비 사용 효과 및 전체 parser 인수로
승격하지 않는다.
`주워 모두`/`버려 모두`도 canonical floor/player root를 가시성·경비·무게·소지 수·
퀘스트/이벤트 보호 규칙과 함께 하나의 후보로 처리한다. 일반 단일 get/drop과 함께
unit/race 및 ARM64 PG17 replay를 통과했지만, container 내부 get/drop, gold/quest
보상, 제물 방·은행·상점 연동은 아직 별도 인수 범위다.
`꺼내 <가방> <물건>`/`넣어 <물건> <가방>` direct container child 이동도 ID/중첩
소유권·용량 counter를 보존하는 receipt로 연결했고 ARM64 PG17 replay를 통과했다.
이 문서는 그 수직 슬라이스를 다시 `완료`로 표시하지 않고, 남은 전체 명령 범위를
추적하는 원장으로만 사용한다.
`잔액`/`입금`/`출금`과 `보관물`/`받아`의 bank slice를 C `RBANK` 방 경계와 3억냥
상한, `모두` 해석, player gold/계정 잔액 원자 변경 및 canonical object-root graph
이동으로 연결했다. 로컬 unit/session/transport 회귀와 ARM64 PostgreSQL 17
`TestPostgresBankMoneyCommandPersistsAndReplays`/`TestPostgresBankItemCommandPersistsAndReplays`의
입금·아이템 보관·동일 command ID replay·출금을 통과했지만, 기존 bank graph 전체
이관과 상점·거래는 아직 별도 인수 범위다.

방 리소스 입장 경계도 추가했다. `AdmitLegacyRoom`은 strict decoder를 약화하지 않고
정책으로 승인된 text/trailing/path-ID issue만 catalog에 넣으며, 원본 bytes/hash/offset을
evidence로 보존한다. `LoadLegacyRoomCatalog`은 C path 규칙을 적용해 3,216개 파일 중
2,341개 canonical room과 875개 noncanonical artifact를 분리하고,
`LegacyRoomCatalog.NewState`가 빈 플레이어 초기 State를 만든다. 이관 예외를 숨기지
않는 구조·정책·catalog TDD는 통과했지만, legacy monster/object의 canonical ID 변환,
PG seed, 전체 room graph 및 runtime admission은 아직 `미구현; G1/G4`다.

## 상태와 범위

2026-09-08 구현 추적 보충(전체 인수와 구분): Go 터미널 가입·로그인과 PG 초안
저장/재로그인은 로컬 DB·브라우저 흐름까지 구현했다. 실제 플레이 상태 초기화,
기존 계정 이관, 이동·전투·게임 저장은 아직 인수되지 않았다. `server/internal/world/`
에는 원본 방 구조 읽기와 조사 API, `display_rom`의 환경/몬스터 설명 출력 부분이
추가됐다. 63개 방의 데이터 예외 처리와 월드 런타임 연결은 미완료다. 아래 G0
표의 미구현 표시는 개별 전체 기능 인수 기준이며 이런 부분 구현을 완료로 세지 않는다.

- Go 게임 기능의 **명령 행 단위 최종 인수 상태**는 모든 행에서 `미구현`이다.
  이는 전체 명령 집합에 대한 최종 인수가 아니라는 뜻이다. 현재 존재하는
  터미널 입력, 가입/로그인, 방향 이동 receipt, 함정, 추종자, canonical NPC
  identity/active 순서 slice는 별도 실행 기록에 구현·검증 증거가 있다. 어느
  하나도 전체 가입·로그인·월드 상태·전투·NPC·아이템·경제·저장 기능의 완료를
  뜻하지 않는다.
- C/Rust/기존 웹 테스트는 비교·이관용 참고 자산이다. C 동작을 그대로 복제해야
  한다는 뜻이 아니며, 알려진 버그는 differential fixture에서 별도로 판정한다.
- `입력 fixture`, `예상 출력/상태`, `Go 구현`, `테스트 명령/결과`, `이관 필요`,
  `인수 상태`는 아래 원장에 미확정 또는 미착수로 남긴다. 직접 handler를 검증하는
  테스트가 발견되지 않은 경우 `CMD-GAP`으로 표시했다.
- `src/frp.new`와 사용자 소유 변경은 조사하지 않았고 수정하지 않았다.

## C dispatch의 근거와 재현 가능한 집계

실행 경계는 다음과 같다.

1. `src/command1.c:1407-1488`의 `command()`가 입력 한 줄을 받고 `!` 재실행을 처리한다.
2. `src/command1.c:1497-1571`의 `parse()`가 최대 `COMMANDMAX` 토큰/숫자를
   `cmd`에 채운다. 한글 문장 끝 처리, 공백/`#` 처리가 여기에 있다.
3. `src/command1.c:1581-1626`의 `process_cmd()`가 `cmdlist[]`를 순서대로
   exact/약어 비교하고, `*` DM 명령 권한을 확인한 뒤 `cmdfn`을 호출한다.
4. 실제 등록표는 `src/global.c:203-558`의 `cmdlist[]`이며 마지막 `@`가 sentinel이다.
   `눌러`/`밀어`의 `cmdfn=0` 두 행은 `special_cmd` 경로를 가리키는 특수 표식이다.
5. 주문 이름/레벨 표는 `src/global.c:571-635`의 `spllist[]`(활성 주문 56개),
   전투 주문 계수 표는 `src/global.c:637-665`의 `ospell[]`(활성 행 24개)다.

다음은 파일을 변경하지 않고 현재 `cmdlist[]`를 집계하는 명령이다. C 주석과
문자열 내부 escape를 처리하므로, 문서의 수치를 갱신할 때 같은 명령을 다시
실행한다.

```sh
perl -0777 -ne '
  my $s = $_;
  $s =~ s!/\*.*?\*/!!gs;
  my ($b) = $s =~ /}\s*cmdlist\[\]\s*=\s*\{(.*?)\n\s*\};/s;
  my (%aliases, %handlers, %ids, $enabled, $special, $sentinel);
  while ($b =~ /\{\s*"((?:\\.|[^"\\])*)"\s*,\s*(-?\d+)\s*,\s*([A-Za-z_][A-Za-z0-9_]*|0)\s*\}/g) {
    my ($alias, $id, $fn) = ($1, $2, $3);
    $ids{$id}++;
    if ($fn eq "0") {
      $id == 0 ? $sentinel++ : $special++;
      next;
    }
    $enabled++;
    $aliases{$alias} = 1;
    $handlers{$fn} = 1;
  }
  printf "enabled_rows=%d unique_aliases=%d positive_ids=%d handlers=%d special_-2_rows=%d sentinel_rows=%d\n",
    $enabled, scalar(keys %aliases), scalar(grep { $_ > 0 } keys %ids),
    scalar(keys %handlers), $special, $sentinel;
' src/global.c
```

현재 출력:

```text
enabled_rows=343 unique_aliases=341 positive_ids=154 handlers=174 special_-2_rows=2 sentinel_rows=1
```

따라서 “명령 154개”는 numeric `cmdno`의 개수이고, 사용자가 입력할 수 있는
등록 행은 343개다. `북동`과 `남서`는 표에 중복 등록되어 고유 별칭 수와 행 수가
다르다. 깨진 바이트로 보이는 일부 이동 alias도 source에 실제로 등록된 값이므로
UTF-8로 임의 정규화하지 않는다. `은신술`/`가입`/`탈퇴`/`전수`/`변수나한권` 및
`sneak`는 C 주석 안에 있어 위 집계에 포함하지 않았다.

## 등록 명령 ledger

아래 표는 `src/global.c`에서 주석을 제거하고 추출한 **활성 command row 전체**다.
동일 `cmdno`에 여러 handler가 있는 경우(예: 은행, 가족, DM 148)는 C의 실제
dispatch 후보를 모두 적었다. 모든 행에 공통으로 적용되는 G0 상태는 다음과 같다.

| 필드 | 현재 값 |
| --- | --- |
| 입력 fixture | 미정. 최소 exact/약어/숫자/한글/권한/오류 fixture 필요 |
| 예상 출력·상태 | 미정. C 실행 transcript와 before/after 상태 캡처 필요 |
| Go 구현 | **미구현** |
| 테스트 명령·결과 | `CMD-GAP` (아래 C 자산 매핑 외 handler 직접 테스트 없음); 이 조사에서 실행하지 않음 |
| 이관 필요 | 필요. C handler와 파일/포인터/시간/RNG 의존성 분해 필요 |
| 인수 상태 | 미착수 |

### 일반 사용자 명령 (cmdno 1–100)

| ID | C handler(s) | source 등록 alias (중복 포함) |
| ---: | --- | --- |
| 1 | `move` | 나가는길, 광장, 향로, 수련장, 현감에게, 면회허가, 반하도인, 월광반, 반야바라밀, 8, 북, ㅂ, �ぃ�, 2, 남, ㄴ, �ⅲ�, 6, 동, ㄷ, �┌�, 4, 서, ㅅ, �В�, 북동, ㅂㄷ, 북서, 북동, 남서, ㅂㅅ, 남동, ㄴㄷ, 남서, ㄴㅅ, 9, 위, ㅇ, ��＂, 3, 밑, ㅁ, �ð�, 밖, 나가 |
| 2 | `look` | 봐, 보다, 조사 |
| 3 | `quit` | 끝 |
| 4 | `say` | 말, ", ' |
| 5 | `get` | 주워, 주, 가져, 꺼내 |
| 6 | `inventory` | 소지품 |
| 7 | `drop` | 버려, 넣어 |
| 8 | `who` | 누구 |
| 9 | `wear` | 입어 |
| 10 | `remove_obj` | 벗어 |
| 11 | `equipment` | 장비, 장 |
| 12 | `hold` | 쥐어, 잡아 |
| 13 | `ready` | 무장 |
| 14 | `help` | 도움말, ? |
| 15 | `health` | 건강, 점수 |
| 16 | `info` | 정보 |
| 17 | `sendman` | 얘기, 이야기 |
| 18 | `follow` | 따라 |
| 19 | `lose` | 내보내 |
| 20 | `group` | 그룹, 무리 |
| 21 | `track` | 추적 |
| 22 | `peek` | 엿봐 |
| 23 | `attack` | 공격, 공, 쳐, 때려 |
| 24 | `search` | 검색, 찾아 |
| 25 | `emote` | 표현 |
| 26 | `hide` | 숨겨, 숨어 |
| 27 | `set` | 설정 |
| 28 | `clear` | 해제 |
| 29 | `yell` | 외쳐 |
| 30 | `go` | 가, 들어가 |
| 31 | `openexit` | 열어 |
| 32 | `closeexit` | 닫아 |
| 33 | `unlock` | 풀어 |
| 34 | `lock` | 잠궈 |
| 35 | `picklock` | 따 |
| 36 | `steal` | 훔쳐 |
| 37 | `flee` | 도망, 도 |
| 38 | `cast` | 주문 |
| 39 | `study` | 배워, 연마 |
| 40 | `readscroll` | 읽어 |
| 41 | `list` | 품목 |
| 42 | `buy` | 사 |
| 43 | `sell` | 팔아 |
| 44 | `value` | 가치, 가격 |
| 45 | `backstab` | 기습 |
| 46 | `train` | 수련 |
| 47 | `give` | 줘 |
| 48 | `repair` | 수리 |
| 49 | `prt_time` | 시간 |
| 50 | `circle` | 교란 |
| 51 | `bash` | 맹공 |
| 52 | `savegame` | 저장 |
| 53 | `postsend` | 편지보내기 |
| 54 | `postread` | 편지받기 |
| 55 | `postdelete` | 편지삭제 |
| 56 | `talk` | 대화 |
| 57 | `gtalk` | 그룹말, 무리말, = |
| 58 | `drink` | 마셔, 먹어 |
| 59 | `broadsend` | 잡담, 잡 |
| 60 | `zap` | zap |
| 61 | `welcome` | 환영 |
| 62 | `turn` | 방혼술 |
| 63 | `bank`, `bank_inv`, `deposit`, `output_bank`, `withdraw` | 보관물, 잔액, 입금, 출금, 받아 |
| 64 | `haste` | 활보법 |
| 65 | `pray` | 신원법 |
| 66 | `prepare` | 경계 |
| 67 | `use` | 사용 |
| 68 | `ignore` | 듣기거부 |
| 69 | `whois` | 사용자검색 |
| 70 | `broadsend2` | 환호 |
| 71 | `teach` | 가르쳐 |
| 72 | `call_war` | 선전포고 |
| 73 | `description` | 묘사 |
| 74 | `purchase` | 구입 |
| 75 | `selection` | 선택 |
| 76 | `trade` | 교환 |
| 77 | `ply_suicide` | 목매달기 |
| 78 | `passwd` | 암호 |
| 79 | `vote` | 투표 |
| 80 | `pfinger` | 사용자정보 |
| 81 | `return_square` | 귀환, 귀 |
| 82 | `ply_aliases` | 줄임말, 줄 |
| 83 | `burn` | 태워, 소각 |
| 84 | `set_title`, `clear_title` | 칭호, 칭호삭제 |
| 85 | `forge`, `newforge` | 제련, 무기만들기 |
| 86 | `change_class` | 직업전환 |
| 87 | `power` | 기공집결 |
| 88 | `accurate` | 살기충전 |
| 89 | `absorb` | 흡성대법 |
| 90 | `meditate` | 참선 |
| 91 | `magic_stop` | 혈도봉쇄 |
| 92 | `writeboard` | 써 |
| 93 | `del_board` | 글삭제 |
| 94 | `look_board` | 게시판 |
| 95 | `chg_name` | 명명 |
| 96 | `info_obj`, `obj_compare` | 감정, 비교 |
| 97 | `kick` | 차기 |
| 98 | `poison_mon` | 독살포 |
| 99 | `up_dmg` | 잠력격발 |
| 100 | `action` | 보아, 감정표현, 노려봐, 끄덕, 응, 아니, 감, 감사, 미소, 청혼, 떨어, 해, 하품, 웃어, 미안, 악수, 하이파이브, 흡연, 담배, 절, 찔러, 춤, 노래, 울어, 달래, 당황, 생각, 부끄러, 놀려, 설레, 잘가, 바이, 안녕, 뽀뽀, 윙크, 구걸, 구박, 안아, 껴안아 |

### 관리자/특수 명령 (cmdno 101–154)

| ID | C handler(s) | source 등록 alias (중복 포함) |
| ---: | --- | --- |
| 101 | `dm_teleport` | *teleport, *순간이동 |
| 102 | `dm_rmstat` | *방번호, *rm, *방 |
| 103 | `dm_reload_rom` | *reload, *로드 |
| 104 | `dm_resave` | *save, *세이브 |
| 105 | `dm_create_obj` | *create, *뭐든지다만들어, *c |
| 106 | `dm_perm` | *perm, *영원 |
| 107 | `dm_invis` | *invis, *바람처럼사라져, *i |
| 108 | `dm_send` | *s, *send, *공지 |
| 109 | `dm_purge` | *purge, *청소, *청 |
| 110 | `dm_ac` | *ac, *방어력 |
| 111 | `dm_users` | *users, *누, *누구 |
| 112 | `dm_echo` | *echo, *말 |
| 113 | `dm_flushsave` | *flushrooms, *모든저장 |
| 114 | `dm_shutdown` | *shutdown, *종료 |
| 115 | `dm_force` | *f, *force, *뭐든지다시켜 |
| 116 | `dm_flush_crtobj` | *flushcrtobj, *재설정, *재 |
| 117 | `dm_create_crt` | *monster, *괴물, *괴 |
| 118 | `dm_stat` | *status, *상태 |
| 119 | `dm_add_rom` | *add, *방제작 |
| 120 | `dm_set` | *뭐든지다해 |
| 121 | `dm_log` | *log, *접속 |
| 122 | `dm_spy` | *spy, *뭐든지다엿봐 |
| 123 | `dm_loadlockout` | *lock, *제한 |
| 124 | `dm_finger` | *finger, *핑거 |
| 125 | `dm_list` | *list, *누구든지다봐 |
| 126 | `dm_info` | *info, *정보 |
| 127 | `dm_param` | *parameter, *수치 |
| 128 | `dm_silence` | *silence, *벙어리 |
| 129 | `dm_broadecho` | *broad, *방송 |
| 130 | `dm_replace` | *replace, *교체 |
| 131 | `dm_nameroom` | *name, *방이름 |
| 132 | `dm_append` | *append, *추가 |
| 133 | `dm_prepend` | *prepend, *서언 |
| 134 | `dm_cast` | *gcast, *전주문 |
| 135 | `dm_group` | *group, *그룹 |
| 136 | `notepad` | *notepad, *메모 |
| 137 | `dm_delete` | *delete, *지우기 |
| 138 | `dm_obj_name` | *oname, *뭐든지다바꿔 |
| 139 | `dm_crt_name` | *cname, *괴물이름 |
| 140 | `list_act` | *active, *활성 |
| 141 | `dm_dust` | *dust, *나도가끔화낸다 |
| 142 | `dm_follow` | *cfollow, *따르기 |
| 143 | `dm_help` | *dmhelp, *도움말 |
| 144 | `dm_attack` | *attack, *공격 |
| 145 | `list_enm` | *enemy, *적 |
| 146 | `list_charm` | *charm, *최면 |
| 147 | `dm_save_all_ply` | *사용자저장 |
| 148 | `family`, `family_news`, `family_who`, `boss_family`, `out_family`, `fm_out`, `family_member`, `family_talk`, `list_family`, `dm_moonstone`, `dm_monster` | 패거리가입, 패거리공지, 패거리누구, 가입허가, 패거리탈퇴, 패거리추방, 패거리원, 패거리말, ], 모든패거리, *떨어져라, *침공 |
| 149 | `buy_states` | 향상 |
| 150 | `marriage`, `m_send`, `divorce` | 결혼, 사랑말, 이혼 |
| 151 | `memo` | 메모 |
| 152 | `invite` | 초대 |
| 153 | `moon_set`, `resend` | 기억, 대답, / |
| 154 | `enemy_status` | 상태 |

### Handler source map

이 표는 위 ID의 source 위치를 좁히는 인덱스다. `src/comman5_old.c`는 같은
역사적 handler의 보존본이며 현재 `src/Makefile`의 `OBJECTS`에는 `command5.o`가
들어간다. 따라서 `comman5_old.c`를 운영 구현으로 읽지 않는다.

| 기능 묶음 | 주요 C source | 관련 ID/계약 |
| --- | --- | --- |
| 입력·로그인·생성·dispatch | `command1.c`, `io.c`, `player.c`, `player_store.c`, `player_path.c` | login/create, parse, alias, process_cmd, quit/return; ID 3, 52, 78, 81, 82 |
| 방·출구·이동·관찰 | `command2.c`, `command6.c`, `room.c`, `files1.c`, `files2.c`, `resource_path.c` | ID 1–2, 29–36, 49, 61–62 |
| 전투·도망·사망·적대 graph | `command5.c`, `command7.c`, `command8.c`, `creature.c`, `player.c` | ID 23, 37, 45, 50–51, 77, 97–99, DM 144–146 |
| NPC/tick/리젠/시간 | `update.c`, `creature.c`, `room.c`, `files2.c`, `main.c` | `update_game`, `update_users`, `update_random`, `update_time`, `update_monster*`, `update_exit` |
| 아이템·인벤토리·장비 | `command2.c`, `command3.c`, `command7.c`, `command8.c`, `command9.c`, `object.c`, `files1.c`, `files3.c` | ID 5–13, 40, 42–48, 58, 67, 83–85, 95–96 |
| 주문·마법·기술 | `magic1.c`–`magic8.c`, `command7.c`, `command8.c`, `command9.c`, `global.c:spllist/ospell` | ID 38–40, 58, 60, 62, 64–67, 87–91, 98–99; 주문 56개 |
| 상점·거래·은행 | `command7.c`, `command8.c`, `command10.c`, `bank.c`, `bank_store.c`, `bank_*` adapters | ID 41–44, 47–48, 63, 74–76 |
| 채팅·사회·게시판·우편 | `command2.c`, `command4.c`, `command10.c`, `command11.c`, `command12.c`, `post.c`, `board.c`, `action.c`, `alias.c` | ID 4, 17–20, 25, 53–57, 59, 68–73, 79–80, 84, 92–94, 100, 148–153 |
| 관리자 | `dm1.c`–`dm6.c`, `docs/dm_cmnd`, `docs/dm.doc` | ID 101–148의 `*` 명령; 권한/영속 side effect 별도 계약 필요 |

## 시스템 기능 ledger (명령 외 상태 전이)

명령 alias만 이관하면 기능 누락을 숨기게 되므로 C의 비명령 tick/저장/자료 형식도
별도 항목으로 기록한다. 아래 `expected`는 향후 fixture가 반드시 관찰해야 할
상태이며, 현재 값은 구현/검증 결과가 아니다.

| 원장 항목 | C 근거 | fixture 및 예상 출력/상태 | 기존 테스트 매핑 | Go 상태 / 인수 |
| --- | --- | --- | --- | --- |
| 세션·접속·입력 lifecycle | `io.c`, `command1.c`, `mstruct.h:iobuf/extra` | connect→prompt→command→disconnect; descriptor/session ownership, prompt/status | `tests/unit/trusted_admission_test.c`, `trusted_admission_file_authority_test.c`, `tests/unit/onboarding_*`, browser/stack onboarding tests | 미구현; 게임 session fixture 미착수 |
| 게임 캐릭터 생성·기존 로그인·비밀번호 | `command1.c:494-1210`, `player.c`, `files1.c` | 캐릭터 이름/비밀번호, 직업/성별/능력치/무기/성향/종족, 저장 후 재로그인; password는 출력/DB evidence에서 redacted | `tests/scenarios/create_and_relogin.json`, `tests/harness/run_onboarding_scenario.py`, `tests/unit/onboarding_credential_lifecycle_test.py`, `tests/unit/player_store_file_test.c` | **부분 규칙 조사만 존재; 전체 가입/로그인 미구현·미인수** |
| 파서·약어·alias | `command1.c:1497-1626`, `alias.c`, `global.c:cmdlist` | exact/약어 우선순위, `!` 재실행, 숫자/한글/공백/문장 끝, alias 저장/재생 | `tests/unit/info_alignment_utf8_test.py`, `tests/unit/alias_title_snapshot_v1_*`, `tests/unit/alias_title_snapshot_manifest_v1_*` (직접 parser oracle은 아님) | 미구현; `CMD-GAP` |
| 방/출구/이동/look | `command2.c`, `command6.c`, `room.c`, `files1.c`, `files2.c`, `mtype.h` flags | room id/name/description/visible exits, exit lock/open state, room constraints, player location and attachments | `tests/unit/files2_load_obj_test.c`, `tests/unit/room_time_test.c`, `tests/unit/resource_tree_manifest_test.py`; direct command test 없음 | 미구현; G1 fixture 필요 |
| 전투·공격·방어·도망·사망 | `command5.c`, `command7.c`, `command8.c`, `creature.c:263-624`, `player.c` | RNG seed/time injection, hit/damage/armor/thaco, enemy list, flee, death/XP/gold/drop/respawn, player/monster side effects | direct combat handler test 없음; `tests/unit/creature_v1_test.c`는 codec only, `tests/unit/update_schedule_test.c`는 schedule only | 미구현; G3 differential 필요 |
| NPC/tick/리젠/시간 | `update.c`, `main.c`, `room.c`, `creature.c`, `files2.c` | 20s user, random interval, active, 150s time, 20,000s moonstone, 4,000/5,000s invasion, timed exit/shutdown | `tests/unit/update_schedule_test.c`, `tests/unit/room_time_test.c` | 미구현; tick clock/RNG contract 필요 |
| 아이템 graph·인벤토리·장비 | `mstruct.h:130-242`, `command2.c`, `command3.c`, `object.c`, `files1.c`, `files3.c` | ordered recursive children, carry/weight, ready↔inventory normalization, wear restrictions, shots/flags, use/burn/repair | `tests/unit/object_v1_test.c`, `object_graph_v1_test.c`, `creature_v1_test.c`, `files1_decoder_test.c`, `files1_serializer_test.c`, `creature_object_layout_contract_test.py` | 미구현; codec tests are not gameplay acceptance |
| 주문·spell list·realm | `global.c:spllist/ospell`, `magic1.c`–`magic8.c`, `command9.c` | known spell/realm, MP cost, level/class/room restriction, duration/timer, combat vs utility, failure and dispel | direct magic tests 없음; `tests/unit/creature_v1_test.c` only serializes spell bytes | 미구현; G3 fixture 필요 |
| NPC 대화/talk files | `files3.c:256-342`, `command8.c:817-1039`, `mstruct.h:ttag`, `docs/crt_talk` | key→response/action/CAST/GIVE/ATTACK, bounded text and deterministic side effects | direct talk test 없음; resource/test fixture gap | 미구현 |
| shop/buy/sell/trade/repair/forge | `command7.c`, `command10.c`, `command8.c`, `docs/rom_stor` | shop storage room/price, item ownership/value, trade quest outputs, repair/forge choices and costs | no direct shop/forge command tests; object/file codec tests are indirect only | 미구현 |
| bank/inventory transfer | `bank.c`, `bank_store.c`, `bank_money_*`, `docs/porting-research/bank-live-capture-gap-20260907.md` | bank object graph and gold before/after, retry/conflict/legacy fallback, room requirement | `tests/unit/bank_store_test.c`, `bank_legacy_abi_test.c`, `bank_evidence_test.c`, `bank_snapshot_v1_test.c`, `bank_transfer_snapshot_v1_test.c`, `bank_money_*_test.c`; live gameplay route remains separate | 미구현; existing tests do not prove Go bank command |
| 우편·게시판·메모·공지 | `post.c`, `board.c`, `command11.c`, `command12.c`, `docs/dm.doc` | append/read/delete ordering, board index/file bounds, room/permission requirements, durable text | no direct handler tests; `tests/stack-e2e/*` covers onboarding/evidence, not in-game board | 미구현 |
| alias/title/description/name | `alias.c`, `command2.c`, `command8.c`, `command11.c`, `command12.c` | alias list/order, title mutation, description/name validation, persistence and UTF-8 bounds | `tests/unit/alias_title_snapshot_v1_test.c`, `alias_title_snapshot_manifest_v1_test.c`, related contract/fixture tests | 미구현; snapshot tests are migration contracts, not Go command tests |
| family/kingdom/marriage/vote/invite | `command11.c`, `command12.c`, `post.c`, `player.c`, `mtype.h` | membership/owner permissions, war/reward, family chat/news, marriage/divorce, invite/vote file state | direct feature tests 없음; stack e2e only asserts selected directory cleanup/metadata | 미구현; P1/P0 ownership and transaction contract needed |
| admin/DM world editing | `dm1.c`–`dm6.c`, `docs/dm_cmnd`, `docs/dm.doc` | class gate, room/object/monster creation/edit/delete, force, spy, global cast, shutdown, save/reload | `tests/unit/player_writer_contract_test.py` and static policy tests cover link/authority constraints only; direct DM behavior tests 없음 | 미구현; separate privileged acceptance suite needed |

## 저장 데이터·자원 ledger

`src/mstruct.h`의 `object`, `room`, `creature`, `exit_`, `daily`, `lasttime`와
`src/mtype.h`의 root path/flags가 저장 설계의 출발점이다. raw legacy format은
native struct/pointer/endianness/`long` 폭에 묶여 있으므로 Go DB schema로 직접
복사하지 않는다.

| 저장/자원 영역 | 읽기·쓰기 근거 | 영속해야 할 의미 | 현재 검증 자산 | Go 이관 상태 |
| --- | --- | --- | --- | --- |
| rooms / exits / descriptions | `files1.c:282-395,817-1025`, `files2.c:64-361`, `room.c`, `mtype.h:ROOMPATH` | room identity/name/flags/limits/timers/traffic/visited, ordered exits, descriptions, permanent objects/monsters | `files1_*`, `files2_load_obj_test.c`, `room_time_test.c`, `resource_tree_manifest_test.py`, `docs/porting-research/persistence-supabase.md` | 미구현; P0 |
| object catalog / nested object graph | `files1.c:64-130,411-477`, `files3.c:26-248`, `object.c`, `mstruct.h:object` | fixed strings, value/weight/type/dice/flags, ordered nested children, parent ownership rebuilt not serialized | `object_v1_test.c`, `object_graph_v1_test.c`, decoder/serializer tests | 미구현; P0 |
| monster/NPC catalog and room instances | `files1.c:207-294,478-741`, `files2.c:385-515`, `creature.c`, `docs/crt_flag`, `docs/crt_talk` | stats/flags/spells/AI/talk/quest/carry and room attachment; runtime enemy/follower/talk pointers are not raw DB fields | `creature_v1_test.c`, `creature_object_layout_contract_test.py`, codec harnesses | 미구현; P0 |
| player record and inventory | `player_store.c`, `file_player_store.c`, `player_path.c`, `files1.c:152-206,465-536`, `command8.c:717-805` | identity, password migration policy, stats/HP/MP/XP/gold/quests/timers, ordered inventory; ready items normalized before save | `player_store_*`, `player_record_serializer_test.c`, `player_snapshot_v1_test.c`, `legacy_player_*` harnesses | 미구현; P0 |
| bank files / gold | `bank.c`, `bank_store.c`, `bank_snapshot_v1*`, `bank_money_*` | bank item graph and gold transaction with idempotent retry/conflict semantics | bank unit and PG adapter tests; live capture explicitly gap-documented | 미구현; P0 |
| aliases/titles and social files | `alias.c`, `post.c`, `board.c`, `command11.c`, `command12.c`, `mtype.h` roots | aliases/title/description, mail/board/news, family/marriage/vote/invite/memo ownership/order | alias/title contracts; no complete social gameplay fixture | 미구현; P1 |
| runtime/session/tick evidence | `mstruct.h:extra/iobuf`, `io.c`, `update.c`, onboarding/M3 files | descriptor/socket/queues/timers/RNG/session identity are runtime; command id/version/receipt is durable only where contract requires | onboarding/M3/journal tests; these do not implement Go world state | 미구현; T/P2 boundary pending |
| static help/editor/resource files | `docs/*`, `resource_path.c`, `bin/*`, `objmon/*`, `rooms/*` | versioned catalog/resource bytes and path aliases; not normal player DML | `resource_tree_manifest_test.py`, path relocation tools, docs | 미구현; R |

## 기존 테스트와 Go 테스트의 구분

현재 발견한 테스트는 대부분 저장 codec, admission/onboarding, bank adapter,
M3 shadow/journal, web protocol 또는 정적 정책을 검증한다. 특히 다음은 **게임
명령 인수 테스트가 아니다**.

- `tests/unit/creature_v1_test.c`, `object_v1_test.c`, `object_graph_v1_test.c`:
  CDTO/graph codec의 경계만 검증한다.
- `tests/unit/files1_decoder_test.c`, `files1_serializer_test.c`:
  raw file read/write 계약만 검증한다.
- `tests/unit/update_schedule_test.c`, `room_time_test.c`:
  시간/스케줄 일부만 검증하고 tick side effect 전체를 검증하지 않는다.
- `tests/unit/bank_*`:
  legacy bank/adapter/metadata 계약이며 Go `bank` 명령과 동일한 end-to-end가 아니다.
- `tests/unit/onboarding_*`, `tests/browser-e2e/*`, `tests/stack-e2e/*`:
  현재 onboarding/admission/UI evidence 경계이며, 인증 이후 C 게임 명령을 모두
  실행하는 테스트가 아니다.
- `tests/unit/alias_title_*`:
  alias/title snapshot 계약이지만 `process_cmd`의 exact/약어 우선순위 oracle은
  아니다.

검증 명령의 결과를 새로 만들지 않았다. plan이 정한 Go 기준 명령
`cd server && go test ./...`, `go test -race ./...`는 향후 Go package가 추가될
때 실행하고, 이 문서의 C test 목록은 “통과”로 보고하지 않는다. C `src/Makefile`
의 `unit-test` target도 선언된 실행 그래프일 뿐 이 조사에서 실행한 결과가 아니다.

## G0에서 남은 gap과 다음 인수 준비

1. 각 ID/handler에 대해 최소 한 개의 deterministic input fixture와 exact output,
   상태 diff(방/플레이어/NPC/object/경제)를 만들 것.
2. `time`, RNG, UUID/command ID, socket/출력 큐를 주입하고, 순서가 의미 있는
   linked list와 영속 DB row의 ordering을 fixture에 명시할 것.
3. C 명령 handler에 직접 연결된 테스트가 없으므로, C 실행 transcript를 수집하는
   disposable oracle harness를 먼저 만들 것. 파일/운영 데이터와 비밀번호를
   fixture로 복사하지 않는다.
4. `mstruct.h` raw pointer 영역과 `player_snapshot_v1`/ObjectGraph 계약을
   구분하고, 저장 대상·runtime 대상·정적 resource를 Go/Postgres schema 전에
   승인할 것.
5. 등록표의 주석 명령(`은신술`, `가입`, `탈퇴`, `전수`, `변수나한권`)을 유지할지
   제외할지 사용자 승인으로 확정할 것. 현재는 등록 기능으로 세지 않았다.
6. 현재 원장은 전 기능을 나열했지만 완료율/분모/예상 기간을 주장하지 않는다.
   각 행의 인수 상태는 첫 C fixture와 실패→통과 Go 테스트가 생긴 뒤에만 갱신한다.

## 2026-09-08 resource graph admission 후속

`LegacyRoomAdmissionPolicy`는 zeroed creature record를
`empty-monster-placeholder` issue로 기록하고 compatibility policy에서만 런타임
catalog에서 격리한다. strict decoder/corpus는 완화하지 않는다. `ImportNPCs`,
`ImportRoomItems`, `ImportNPCItems`는 각각 NPC identity, room floor, NPC inventory의
one-time canonical ID graph migration이며 room/root/nested 또는 NPC membership 순서를
보존한다. `State.Validate`는 모든 canonical item owner를 함께 검사하고, projection과
NPC death transfer는 기존 ID를 재발급하지 않는다.

검증: `TestLegacyRoomCatalogImportsCanonicalResourceGraphs`,
`TestImportRoomItems*`, `TestImportNPCItems*`, `TestNPCDeathTransfersCanonicalNPCItemsWithoutReallocatingThem`,
`TestPostgresSeedCanonicalRoomGraphs`, ARM64 PostgreSQL 17 CLI canonical seed. 이 결과는
resource seed 경계의 증거이며 전체 아이템 경제·player/bank 이관·scheduler/tick 인수로
승격하지 않는다. 저장 데이터 ledger의 rooms/object/NPC 행은 여전히 전체 gameplay
acceptance가 미완료임을 유지한다.

## 2026-09-08 운영 루프 연결 후속

`RunPlayerVitalScheduler`가 `WorldConnector.RunPlayerVitalPhase`를 `-player-tick`
cadence에 연결했다. `player-vitals-<slot>` deterministic command ID, fixed request
timestamp/hour, pending retry, duplicate-slot skip, connector writer serialization,
shutdown worker cancellation을 `world_tick_test.go`에서 검증했다. 이 항목은 player-vital
부분 루프의 운영 연결 증거이며 `update.c`의 전체 NPC/room spawn/combat/time scheduler나
전체 명령 인수를 완료로 표시하지 않는다. 실제 ARM64 PostgreSQL receipt/replay 증거는
`TestWorldConnectorPlayerVitalPhasePostgresPersistsAndReplays`와
`TestWorldConnectorPlayerVitalTickReplaysAcrossConnectorRestart`다.

## 2026-09-08 central command parser 후속

`session.ParseCommand`와 `CommandKind`가 현재 Go durable handler의 alias 분류를 한곳으로
모으고 `WorldConnector.Submit`의 순차 fallback을 제거했다. quote-aware tokenization,
7-token bound, direction-first dispatch, unsupported command fail-closed 응답을
`command_parser_test.go`와 transport dispatch 회귀로 검증했다. C의 전체 154 positive
command handler, prefix ambiguity, occurrence 문법, alias persistence는 여전히
미구현 ledger 항목으로 유지한다.

## 2026-09-08 document help 후속

`CommandHelp`를 parser/transport에 추가하고 `ExecuteHelpLine`을 durable receipt 경계에
연결했다. `help/`의 `helpfile`, `spellfile`, `policy`, 현재 Go handler 명령의
`help.<cmdno>`를 읽으며, source가 없거나 UTF-8이 아니면 receipt를 만들지 않는다.
unknown topic은 원본 C와 같은 no-help 응답을 결정론적으로 재생한다. unit/transport
TDD와 실제 ARM64 PostgreSQL 17 + Chromium E2E(`1 passed (11.0s)`)가 통과했지만, 이는
전체 C command table·문서 continuation·info title·약어 동등성 인수가 아니다.

## 2026-09-08 welcome/yell/emote 후속

`환영`은 고정 `help/welcome` 문서를 `fs.FS` 주입 경계에서 읽는 read-only receipt다.
누락·비 UTF-8은 fail-closed하고, 동일 command ID replay는 문서 재읽기와 commit을
재실행하지 않는다. `외쳐`는 `command6.c:yell`의 empty/silent/PHIDDN 순서를 원자
상태 전이로 옮기고, commit 후 current-room named event와 ordered-exit anonymous event를
파생한다. 본인·replay 재방송을 막고, unknown exit topology는 후보 상태를 저장하지
않는다. `TestPostgresYellCommandPersistsAndReplays`가 ARM64 PostgreSQL 17 저장·replay를
검증한다.

`action.c` 일반 플레이어 감정표현은 현재 출력 계약이 확보된 bounded exact alias 집합만
등록했다. `감정표현`, `노려봐`, `끄덕`/`응`, `감`/`감사`, `미소`, `청혼`, `떨어`, `해`,
`하품`, `웃어`, `미안`, `악수`, `하이파이브`, `박수`, `흡연`/`담배`, `절`, `찔러`, `춤`,
`노래`, `울어`, `달래`, `당황`, `생각`, `부끄러`, `놀려`, `설레`, `바이`/`잘가`, `안녕`,
`뽀뽀`, `윙크`, `구걸`, `구박`, `안아`/`껴안아`를 exact command로 처리한다. PHIDDN 해제와
PSILNC ordering을 보존하고, exact same-room online player target에만 target-specific
projection을 허용한다. NPC/prefix/occurrence/미검증 target은 fail-closed다. target·room
fan-out과 actor 제외는 transport에서 committed snapshot으로만 수행한다.
`TestPostgresEmoteCommandPersistsAndReplays`와 `TestWorldConnectorSubmitDispatchesEmoteWithTargetAndRoomProjection`
이 저장·replay·event 경계를 검증한다.

검증 명령과 결과: `go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1`, `go vet ./...`,
전용 ARM64 `postgres:17-alpine` PG command tests, 실제 Go+PG+Chromium E2E **1 passed
(10.4s)**, `pnpm test:browser`의 표준/feature-off 각 1 passed. 이는 전체 154 C command,
strict room corpus의 기존 63개 예외, 전체 action alias·모바일/WSS/Ingress·testnet 인수를
완료했다는 의미가 아니다.

## 2026-09-08 express/look-at-target 후속

`표현`(`command11.c:emote`)은 actor response만 receipt에 저장하고, 255 UTF-8 byte
bound·control/invalid UTF-8 fail-closed, empty/silent no-op, non-silent PHIDDN clear와
PLECHO response ordering을 검증한다. committed snapshot에서만 `:이름님이 <text>.` room
event를 파생해 actor/replay 중복 fan-out을 막는다. `보아 <대상>`은 `action.c` explicit
target branch의 bounded slice로, exact same-room canonical target과 NPC-first traversal,
visibility/detect/DM-invisible gate를 적용한다. player target에는 target-specific event,
observer에는 room event를 주며 NPC target은 room projection만 허용한다. bare `보아`,
prefix/occurrence/object inspection과 전체 `조사`는 미구현 ledger 항목으로 유지한다.

`TestPostgresExpressCommandPersistsAndReplays`, `TestPostgresLookAtTargetCommandPersistsAndReplays`,
transport fan-out/receipt tests, full Go race/vet, Linux ARM64 cross-build와 실제
Go+PostgreSQL+Chromium E2E **1 passed (10.1s)**가 통과했다. 이는 전체 명령/alias,
strict room corpus의 기존 63개 예외, testnet 배포 인수를 승격하지 않는다.

## 2026-09-08 strict 방 본문 예외 감사

`TestRoomBodyCorpus`의 기준 실패를 다시 실행한 결과, 원본 `rooms/` 3,216개 중
63개가 계속 strict body decode를 거부했다. `InspectLegacyRoom` 기준 이슈는
`invalid-euc-kr` 80건, `missing-text-terminator` 13건, `trailing-data` 7건으로
총 100건이며, 이슈 offset은 변환 대상 필드의 시작 위치다. 이 기준선은
`server/internal/world/legacy_room_audit_fixture.go`의 63개 path/크기/소비 위치/
SHA-256/이슈 위치 fixture와 `TestRoomBodyCorpusExceptionAudit`가 고정한다.

이번 조사에서는 공통 runtime 변환을 적용하지 않았다.

- `invalid-euc-kr`는 일반 한글 문자열뿐 아니라 원본 바이트에 EUC-KR 표에 없는
  2바이트열(예: `r00/r00100`의 offset 588)과 그래픽성 바이트가 섞인 필드다.
  `resources_utf8/rooms`는 해당 파일의 바이트와 동일하여 독립적인 정정 원천이
  아니며, 대체 문자·삭제·임의 매핑은 원작 출력과 저장 값을 바꾼다.
- `missing-text-terminator`는 고정 폭 텍스트 필드가 폭을 모두 사용한 사례다.
  inspection은 필드 경계에서 읽기를 제한해 원본을 보존하지만, NUL을 합성하거나
  자르는 것은 C의 실제 문자열 사용 계약을 확보하기 전에는 runtime 변환으로
  승격할 수 없다.
- `trailing-data` 7건은 알려진 구조를 `Consumed`까지 읽은 뒤 비영(非零) 데이터가
  남는다. `r00/r00173`의 offset 751처럼 남은 바이트가 몬스터·아이템·타이머처럼
  보이는 경우도 있어, C reader가 EOF를 검사하지 않았다는 사실만으로 폐기 또는
  재해석을 결정할 수 없다. 원본 writer의 파일 길이 축소 보장과 파일별 oracle이
  없으므로 자동 절삭은 금지한다.

fixture는 각 예외의 원본 SHA-256·소비 위치·이슈 순서를 비교하고, caller raw bytes가
  변하지 않는 것과 `DecodeLegacyRoom` 및 zero-value `LegacyRoomAdmissionPolicy`가
  계속 거부하는 것을 확인한다. 따라서 이번 lane의 strict 예외 수는 **63개에서
  63개로 유지**되며, raw corpus와 strict/default 정책을 보존한 감사 증거만 추가됐다.
  호환 정책으로 runtime catalog에 넣을 때도 기존 evidence 보존·명시적 검토 경계를
  유지해야 한다. 후속 runtime 변환은 C 실행 transcript 또는 파일별 수동 매핑과
  상태/출력 fixture가 확보된 뒤 한 issue class 이하의 bounded change로 재검토한다.

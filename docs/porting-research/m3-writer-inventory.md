# M3 character + bank writer inventory

상태: 코드 기반 정적 조사 (2026-09-02) · 이 문서와 계약 테스트만 변경

## 결론

현재 canonical character record의 유일한 저장 진입점은 `save_ply()`이며,
실제 파일 교체는 `file_player_store_save()` 한 곳에서 수행한다. `load_ply()`도
`PlayerStore` dispatch를 거쳐 `file_player_store_load()`로 간다. 따라서 M3의 첫
facade는 이 seam을 유지한 채 `PlayerStore`의 저장 결과와 불확정 결과를 보존해야
한다.

bank는 이 경계 밖이다. `load_bank()`/`save_bank()`가 `src/bank.c`에서
`PLAYERPATH/bank/<name>`을 직접 열고 `read_obj()`/`write_obj()`를 호출한다.
현재는 player save와 bank save가 별도 호출되어 경제 aggregate를 원자적으로
보호하지 않는다. 이 bypass는 의도된 현행 baseline으로 계약 테스트에 고정했다.

## 저장 형식과 facade 후보

| 대상 | 현재 구현 | 성공/실패 의미 | dual-write 위험 | 필요한 facade 순서 |
| --- | --- | --- | --- | --- |
| character | `src/player_store.c:1-37`의 `save_ply/load_ply` dispatch → `src/file_player_store.c:75-203` | `PLAYER_STORE_OK`; load는 `NOT_FOUND`(ENOENT), `CORRUPT`(짧거나 parser 실패), `IO_ERROR`(path/open/stat/read/close). save는 serialize/write/fsync/close/rename/parent-fsync 중 하나라도 실패하면 `IO_ERROR`. rename 뒤 parent fsync 실패도 error이므로 결과가 불확정일 수 있다. | legacy 파일과 DB를 각각 성공시키려 하면 partial commit, 재시도 중 duplicate ownership, 오래된 snapshot overwrite가 생긴다. file replacement는 atomic이어도 Postgres와 transaction을 공유하지 않는다. | 1) `PlayerStore` facade에 command/correlation ID와 pre/post hash를 붙인다. 2) local intent journal을 fsync한다. 3) legacy replacement 성공 후 canonical DB/outbox를 기록한다. 4) reconciliation에서 ambiguous result를 확인한다. |
| character path | `src/player_path.c:129-223` (`player_path_from_name`, `player_path_ensure_dir`, `player_path_open_readonly`) | canonical name → SHA-1 첫 byte shard → `player/<shard>/<name>`. ensure는 shard를 0700으로 만들고 no-follow descriptor를 확인한다. readonly open은 root/player/shard/file를 `openat(...O_NOFOLLOW)`로 열고 regular file 및 size limit를 확인한다. 실패는 `-1` + errno. | DB key를 path 문자열이나 SQL `lower()`와 혼동하면 UTF-8 byte identity/shard가 깨진다. raw path를 별도 writer가 계산하면 facade 우회가 된다. | 1) canonical name/hash resolver. 2) descriptor-based read. 3) 모든 writer가 resolver/facade를 통해서만 path를 얻도록 전환. |
| bank | `src/bank.c:16-69`의 `load_bank/save_bank` | 두 함수 모두 `-1` 하나로 not-found, malformed, open/read/write/close failure를 합친다. `save_bank`는 기존 파일을 `O_RDWR`로 열고, 없으면 `O_CREAT|O_TRUNC`; `write_obj` 실패만 확인하며 close 결과는 무시한다. 기존 파일을 truncate하지 않아 새 payload가 더 짧을 때 tail이 남을 수 있다. | player gold와 bank object tree 이동이 `savegame_nomsg()`와 `save_bank()`로 분리되어 한쪽만 성공할 수 있다. direct truncate/write는 crash 중 partial bank와 concurrent overwrite를 만든다. | 1) bank object codec을 bounded read/write facade로 감싼다. 2) bank + character gold/item move를 하나의 command transaction으로 만든다. 3) legacy writer journal/epoch를 붙인다. 4) DB canonical 전환 뒤 legacy bank는 read-only shadow로 둔다. |

## character writer/reader callsite 전수 목록

### `save_ply()` (모두 facade를 통과하는 정상 callsite)

| 위치 | 함수/경로 | 의미와 실패 처리 |
| --- | --- | --- |
| `src/command8.c:717-765` | `savegame` | ready item을 duplicate에서 inventory로 풀어 저장한 뒤 원본을 복구한다. 실패는 사용자에게 재시도 안내. |
| `src/command8.c:768-807` | `savegame_nomsg` | 같은 duplicate/unready 규칙의 무음 저장. 실패는 `merror` nonfatal. 대부분 gameplay command의 공용 저장 경로. |
| `src/command1.c:1013-1021` | `create_ply` | 신규 character 초기화 뒤 저장. provisioning이면 실패를 onboarding abort로, 일반 생성이면 접속을 유지하고 재저장을 안내. |
| `src/io.c:1103-1163` | `disconnect` | active player `uninit_ply` 뒤 저장. 실패 시 `player_recovery_enqueue`가 소유권을 가져가 재시도하고, queue가 고갈되면 fatal. |
| `src/player_recovery.c:62-80` | `player_recovery_retry_one` | disconnect에서 보류한 player를 한 번에 하나 재시도. 성공할 때만 queue pointer를 해제. |
| `src/command12.c:155-257` | `fm_out` | family 탈퇴를 loaded record에 적용하고 저장. 실패 시 family state를 복구하거나 작업을 중단. |
| `src/command11.c:107-180` | `chpasswd` | password 변경을 저장. 실패 시 접속을 유지하며 저장 확인을 다시 요구한다. legacy password가 raw player record에 포함되는 민감한 writer다. |
| `src/special1.c:259-361` | `check_item`/`check_contain` | 비정상 item 제거 후 player snapshot 저장. 실패는 log만 남기고 검사 경로는 계속된다. |
| `src/player_recovery.c:72` | 위 retry 경로 | 별도 raw write가 아니라 `save_ply` 재호출이며, 모든 deferred save의 유일한 재시도 지점이다. |

간접적으로 `savegame`/`savegame_nomsg`를 호출하는 gameplay callsite는
`src/player.c:783`, `src/creature.c:524`, `src/files2.c:304`,
`src/command2.c:887,1144,1330,1436,1475,1567,1670,1680`,
`src/command6.c:858-859`, `src/command8.c:125-126,185-186`,
`src/command9.c:63`, `src/magic7.c:736-737`, 그리고 `src/bank.c:192,267,273,332,386,482,592`다.
이들은 player payload를 직접 열지 않고 공용 save facade를 사용한다.

### `load_ply()`

| 위치 | 함수/경로 | 의미와 실패 처리 |
| --- | --- | --- |
| `src/command1.c:212-316` | `login` | 이름 검증 후 roster login. NOT_FOUND는 create 질문으로, corrupt/io는 disconnect. |
| `src/command1.c:427-485` | `trusted_admission_login` | trusted ticket의 character를 로드하고 실패 시 admission을 닫는다. 두 load는 ticket/재검증 경로다. |
| `src/command1.c:552-610` | `onboarding_provision` | 이름이 아직 없어야 하므로 NOT_FOUND만 신규 생성 허용; 기존/오류는 fail closed. |
| `src/command1.c:667-718` | `onboarding_claim` | 기존 record를 읽어 canonical name/SUICD를 확인하고 claim challenge를 만든다. 실패 시 loaded tree를 해제하고 ownership을 바꾸지 않는다. |
| `src/command1.c:383-395` | login 재읽기 | disconnect/reconnect 또는 login continuation에서 다시 로드; 실패는 접속 종료. |
| `src/command11.c:410-447` | player info | online이 아닌 character를 읽어 표시한 뒤 `free_crt`; 읽기는 read-only여야 한다. |
| `src/command11.c:1320` | marriage/divorce 보조 | offline 상대 record를 읽어 관계 처리 입력으로 사용. caller의 후속 save는 별도다. |
| `src/command12.c:155-203` | `fm_out` | offline family member를 읽어 수정 가능 여부를 확인한다. |
| `src/onboarding_recovery.c:373-418` | `ore_recover_pending` | receipt가 가리키는 player를 읽어 pending recovery를 확인; 불일치/오류는 quarantine/실패. |
| `src/plist.c:10-82` | offline listing utility | 각 이름을 읽어 출력하고 free; gameplay writer가 아니다. |

`src/bank_check.c:29`의 `load_ply_from_file`은 `load_ply`와 다른 legacy
utility/API이며 bank 점검 도구에서만 쓰인다. 이 별도 read path는 M3에서
같은 bounded decoder/facade로 통합해야 한다.

## bank `load_bank/save_bank` callsite 전수 목록

| 위치 | 함수 | 동작 | dual-write/실패 의미 |
| --- | --- | --- | --- |
| `src/bank.c:85-108` | `bank_inv` | bank object를 없으면 빈 container로 만들고 저장, 있으면 목록 출력 또는 `input_bank` 호출 | 빈 bank 생성이 player save와 무관한 direct write. save 실패를 사용자에게 전파하지 않는다. |
| `src/bank.c:111-135` | `bank` | bank balance를 읽어 표시 | read failure를 빈 object로 취급하며 allocated object lifecycle도 caller가 관리한다. |
| `src/bank.c:137-196` | `input_bank` | player inventory → bank object 이동 후 `savegame_nomsg`와 `save_bank`를 순차 실행 | player save 성공/bank save 실패 또는 반대가 가능해 아이템 복제/손실 위험이 가장 직접적이다. |
| `src/bank.c:199-278` | `output_bank` | bank object → player inventory 이동 후 bank save와 player save를 순차 실행 | 동일한 split commit; event item 분기에서는 player save가 추가로 실행된다. |
| `src/bank.c:280-338` | `deposit` | player gold 차감 후 bank value 증가, bank save 뒤 player save | currency debit/credit이 두 파일에 나뉘며 save 결과를 무시한다. |
| `src/bank.c:340-392` | `withdraw` | bank value 차감 후 player gold 증가, bank save 뒤 player save | bank 저장 실패 후 memory gold가 이미 증가할 수 있다. |
| `src/bank.c:394-486` | `drop_all_bank` | inventory 전체/부분을 bank container로 이동 후 player save + bank save | capacity/filtered item이 섞인 aggregate를 두 writer가 갱신한다. |
| `src/bank.c:489-593` | `get_all_bank` | bank container를 player inventory로 이동 후 bank save + player save | money와 item 분기 및 early return 때문에 partial save 위험이 크다. |
| `src/special1.c:259-325` | `check_item` | bank object를 읽어 bad item을 검사; 저장하지 않음 | bank read가 player integrity 검사와 독립되어 있다. |
| `src/bank_check.c:15-42` | offline `main` | `load_ply_from_file` 후 bank load, 없으면 빈 bank save | 운영 보정 도구가 직접 bank writer를 호출하는 별도 mutation 권한이다. |

## player path raw open/rename/unlink 분류

### canonical player record에 닿는 경로

| 위치 | API | 분류 | 위험/후속 순서 |
| --- | --- | --- | --- |
| `src/file_player_store.c:85-131` | `player_path_ensure_dir`, `player_path_from_name`, `mkstemp`, `write_crt`, `fsync`, `rename`, parent `fsync` | **승인된 writer** | 현재 유일한 atomic-ish player writer. temporary file cleanup, no-follow parent check, error 반환을 facade contract로 보존한다. |
| `src/player_path.c:183-223` | `player_path_open_readonly` + `openat` | 승인된 bounded reader | name validation, no-follow descriptor walk, regular-file/size bound가 있으므로 모든 reader가 이 함수로 수렴해야 한다. |
| `src/command1.c:291-295` | `player_path_from_name` + `rp_stat` | login metadata read | `ctime`만 읽고 payload는 `load_ply`; writer facade와 분리한다. |
| `src/command1.c:1013` | `save_ply` | 승인 writer callsite | raw path를 열지 않는다. |
| `src/command5.c:709-727` | `player_path_from_name` 후 shell `mv` | **legacy archive bypass baseline** | suicide가 canonical player, alias, bank를 disconnect 뒤 세 개의 shell command로 이동한다. 이름이 shell quoting되지 않아 injection/partial archive 위험. DB cutover 전 archive facade의 마지막 단계에서 descriptor rename + journal로 대체해야 한다. |
| `src/dm6.c:62-73` | `player_path_from_name` 후 shell `mv ...~~` | **legacy archive bypass baseline** | DM lightning이 player record를 archive move한다. disconnect와 shell move 사이 crash 및 command injection 위험. archive facade로 대체한다. |
| `src/command12.c:103-107,199-203` | `player_path_from_name` + `open/stat` | read-only existence/mtime check | open은 `O_RDONLY`; 같은 함수의 `fal` memo writer는 별도 path이며 canonical record writer가 아니다. |
| `src/command11.c:435-438` | `player_path_from_name` + `stat` | read-only metadata check | direct writer 없음. |
| `src/post.c:50-61` | `player_path_from_name` + `open(O_RDONLY)` | recipient existence check | recipient player file는 read-only; actual write는 `POSTPATH` temp path다. |
| `src/onboarding_recovery.c:344-358` | path + `lstat/open(O_RDONLY|O_NOFOLLOW)` | recovery identity check | `ore_same_player`가 inode/device/link를 비교하고 writer는 호출하지 않는다. |

`src/comman5_old.c:734-742`는 활성 build의 `command5.c`와 같은 오래된
복사본이며, build target에는 들어가지 않는다. 그래도 raw archive pattern이
재활성화되지 않도록 inventory에 남긴다.

### 같은 `PLAYERPATH` 아래지만 character record가 아닌 파일

`src/alias.c:31-88,232-259`의 alias, `src/post.c`의 `fal`/family news,
`src/command11.c`의 vote/family/marriage, `src/command12.c`의 family/invite,
`src/player.c:248-253`의 `fal` notice는 모두 별도 durable aggregate다. 이들은
`player_path_from_name`으로 canonical record를 여는 writer가 아니지만,
나중에 player facade를 도입할 때 함께 raw `PLAYERPATH` access를 grep으로
검토해야 한다. 특히 alias는 `rp_open(...O_WRONLY|O_CREAT|O_TRUNC)`로 직접
replace하고, memo/news는 append/delete semantics가 있다.

## 권장 facade 이관 순서

1. **Character read/write seam 고정** — `save_ply/load_ply` 결과 enum, canonical
   UTF-8 name/shard, `write_crt/read_crt` ABI와 nested object bounds를 freeze한다.
2. **Atomic legacy writer journal** — `file_player_store_save` 앞뒤 source hash,
   command ID, rename/parent-fsync 결과를 기록한다. ambiguous post-rename error는
   blind retry하지 말고 hash 확인으로 resolve한다.
3. **Bank codec + transaction facade** — `read_obj/write_obj`를 bounded facade로
   감싸고 character gold, bank value, item location 변경을 한 command transaction으로
   묶는다. 이 단계 전에는 DB dual-write를 시작하지 않는다.
4. **Archive/delete facade** — suicide/lightning의 shell `mv`를 descriptor-based
   archive operation으로 옮기고 alias/bank/archive metadata를 하나의 journal로
   연결한다.
5. **Callsite cutover + shadow** — gameplay의 save/load는 facade만 사용하고,
   bank bypass baseline을 제거한 뒤 DB shadow/reconciliation을 켠다. canonical 승격
   전까지 legacy C가 유일한 active writer여야 한다.

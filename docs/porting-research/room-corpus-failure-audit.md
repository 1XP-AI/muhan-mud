# Strict room corpus failure audit

상태: **known failure / OPEN** (2026-09-10) · 범위: Go G4 데이터 이관 증거

이 문서는 원본 `rooms/` 바이트를 보정하거나 Go 디코더의 엄격도를 낮추지 않고,
현재 strict room body 실패 63건을 source-backed하게 분류한 감사 결과다. 문서와
읽기 전용 검증만 수행했으며 `src/frp.new`, Go core/parser/transport, 원본 room
파일은 수정하지 않았다.

## 결론

- 원본 `rooms/`에는 정규 파일 3,216개가 있다. `TestRoomBodyCorpus`는 그중
  **3,153개를 strict decode하고 63개를 거절**한다. 이 63은 현재 알려진
  승격 차단 조건이다.
- `InspectLegacyRoom`은 3,216개 모두 구조적으로 조사하며 structural failure는
  0건이다. 63개 방에서 issue 100건을 보고한다: `invalid-euc-kr` 80,
  `missing-text-terminator` 13, `trailing-data` 7.
- 경로 분류는 독립적인 overlay다. 63개 중 C 경로와 일치하는 canonical 파일은
  46개, path-shard가 맞지 않는 historical artifact는 17개다. 17개는 모두
  `invalid-euc-kr`이며, body issue 수에 추가로 세지 않는다.
- 현재 증거에서 **실제 미구현 필드/offset decoder gap은 0건**이다. 헤더 3,216개가
  모두 읽히고, `InspectLegacyRoom`의 모든 strict 실패가 아래 세 issue 종류 중
  하나로 재현된다. C가 text를 opaque bytes로 읽거나 EOF를 검사하지 않는 것과 Go
  strict contract의 차이는 호환 정책 문제이지, 원인 불명 parser gap으로 세지
  않았다.

## 증거 사슬

| 단계 | source-backed 증거 |
| --- | --- |
| 원본 provenance | 모든 실패 파일은 Git 초기 커밋 `f3cdc1ed08f5a7e2e13d963ea864c5bc8686165d`에서 추적된다. `resources_manifest/resource-map.v1.json`은 `rooms/*`의 legacy path(hex/CP949), normalized path, blob SHA-1, SHA-256, size를 보존한다. 실패 파일의 manifest `kind`/`text_encoding`은 모두 `binary`다. |
| normalized copy | `rooms/`와 `resources_utf8/rooms/`의 3,216개 대응 파일을 바이트 비교한 결과 누락 0, 불일치 0이다. normalized tree는 정정 원천이 아니다. |
| exact fixture | `server/internal/world/legacy_room_audit_fixture.go`가 각 63개 path의 size, `Consumed`, SHA-256, issue 순서/offset을 고정한다. |
| inspection | `legacy_body.go`의 `InspectLegacyRoom`은 원본 복사본·SHA-256·소비 위치·issue를 반환하며, `LegacyRoom`을 runtime admission 허가로 해석하지 않는다. |
| strict decoder | `legacy_room.go`/`legacy_body.go`는 ILP32 little-endian `room=480`, `exit=44`, `object=352`, `creature=1184` layout, bounded counts, EUC-KR text, 세 description, exact EOF를 검사한다. |
| C 기준 | `src/mstruct.h:134-192`의 raw room/object layout, `src/files1.c:295-400`의 `write_rom`, `src/files1.c:817-1015`의 `read_rom`, `src/files2.c:64-140,229-253`의 `load_rom`/resave 경계를 대조했다. |
| path 기준 | C `load_rom`은 `src/files2.c:85-87`에서 `rooms/r%02d/r%05d` (`index/1000`)를 열고 `src/files2.c:99`에서 path index를 runtime room ID로 쓴다. Go `canonicalRoomPath`/admission manifest도 이 경계를 보존한다. |

## 수량과 분류

### 방 단위 (63개)

| 방 단위 분류 | 방 수 | canonical | noncanonical artifact | 의미 |
| --- | ---: | ---: | ---: | --- |
| invalid EUC-KR만 | 49 | 32 | 17 | 하나 이상의 NUL-terminated text field가 EUC-KR strict decode 실패 |
| missing terminator만 | 6 | 6 | 0 | 고정폭 text field 안에 NUL 없음 |
| invalid EUC-KR + missing terminator | 1 | 1 | 0 | `r03/r03388`의 두 object name과 한 use output |
| trailing data만 | 7 | 7 | 0 | 알려진 room sequence 뒤에 비어 있지 않은 tail |
| **합계** | **63** | **46** | **17** | 한 방에 issue가 여러 개일 수 있음 |

### Issue 단위 (100건)

| `LegacyIssue.Kind` | 건수 | source field/형태 | 판정 |
| --- | ---: | --- | --- |
| `invalid-euc-kr` | 80 | room `long_description` 34건, `object[0].contents[*].name` 46건. 모두 NUL은 있으나 strict EUC-KR decode가 실패한다. | 원본 text bytes/인코딩 anomaly. 대체문자·삭제·임의 CP949 매핑 금지 |
| `missing-text-terminator` | 13 | 모두 80-byte `object[*].contents[*].use_output` 고정폭 field. | NUL 합성·field 절삭 전 C writer/사용 계약 증명 필요 |
| `trailing-data` | 7 | 세 description까지 읽은 `Consumed` 뒤에 non-zero tail이 남는다. C `read_rom`은 EOF를 검사하지 않는다. | stale/append/다른 record 가능성을 조사하되 tail을 폐기·재해석하지 않음 |

`invalid-euc-kr`는 `legacyText`가 `korean.EUCKR` decoder 오류 또는 U+FFFD를
거부한 결과다(`legacy_room.go:34-44`). `missing-text-terminator`는 같은 함수의
NUL 탐색 실패로 별도 기록된다(`legacy_body.go:134-150`). 따라서 두 종류를
“문자열이 이상하다”로 합치면 안 된다.

## 파일별 source-backed inventory

아래 offset은 해당 field의 시작 offset이다. size/consumed/SHA-256의 전체 값은
동일 path에 대해 `legacy_room_audit_fixture.go`와 resource manifest가 고정하므로
문서에는 반복하지 않는다.

### Canonical path의 encoding anomaly — 63 issue occurrences, 33 files

`room.long_description` (17건):

```text
r00/r00100@588  r00/r00265@632  r00/r00269@632  r00/r00277@3408
r00/r00332@632  r00/r00376@588  r00/r00400@588  r00/r00412@2220
r00/r00422@588  r00/r00547@588  r00/r00593@2132 r00/r00665@588
r00/r00708@544  r00/r00748@588  r00/r00759@588  r03/r03466@26532
r05/r05029@900
```

`object[0].contents[*].name` (46건):

```text
r03/r03200@11616
r03/r03295@8012,9436
r03/r03374@2804,4584,5296,5652,6008,18824
r03/r03377@1960
r03/r03380@7564
r03/r03387@892,1248,1604,8368
r03/r03388@1248,10504
r03/r03391@1960,2316,2672,3028,3384
r03/r03398@892,1248,1604,1960,15132
r03/r03412@892,1604
r03/r03420@8012
r03/r03428@1648,18736
r03/r03429@892,3384,4808,5164,5520,5876
r03/r03430@892,1248,1604
r03/r03435@892,3740,4096
r03/r03460@2004,10904
```

이 46건은 모두 r03의 nested object name field에 있고, field 내부 NUL은
존재한다. 즉 object tree framing을 못 읽은 것이 아니라, 그 field의 원본
bytes를 strict text로 승인할 근거가 없는 경우다.

### Canonical path의 missing terminator — 13 issue occurrences, 7 files

모두 object `use_output` 80-byte field이며, 끝 byte까지 NUL이 없다.

```text
r03/r03388@21760
r03/r03438@15352
r03/r03449@43120
r03/r03462@4672,5028,5384,5740,6096,6452
r03/r03464@21804
r03/r03468@18956,19312
r03/r03482@27456
```

`r03/r03388`은 위 encoding anomaly 두 건(`@1248`, `@10504`)도 함께 가진다.

### Canonical path의 trailing data — 7 files

Go가 세 description까지 정확히 소비한 뒤 남은 tail의 길이를 함께 기록한다.
모든 tail은 non-zero byte를 포함한다. 길이가 raw struct 조각처럼 보인다는
사실은 증거일 뿐, 누락된 count를 복원했다는 뜻이 아니다.

| path | file size | `Consumed` | tail bytes |
| --- | ---: | ---: | ---: |
| `r00/r00173` | 2295 | 751 | 1544 |
| `r00/r00278` | 1403 | 1047 | 356 |
| `r00/r00390` | 2330 | 786 | 1544 |
| `r00/r00443` | 3397 | 1021 | 2376 |
| `r03/r03053` | 2525 | 981 | 1544 |
| `r03/r03073` | 2080 | 892 | 1188 |
| `r03/r03502` | 2151 | 963 | 1188 |

C `write_rom`은 예상 sequence를 쓰지만, `resave_rom`/`resave_all_rom`은 기존
파일을 truncate하지 않는 open 경계를 사용하고 `read_rom`은 세 description을
읽은 뒤 EOF를 확인하지 않는다. 따라서 stale tail 경로는 가능하지만, 이 7개가
어느 write 실행에서 생겼는지는 현재 증명하지 못했다.

### Noncanonical path/provenance overlay — 17 files

다음 파일은 room ID가 1,000 미만인데 `r01`/`r05` 아래에 있다. C 경로 규칙상
기대 디렉터리는 모두 `r00`이다. 각 파일은 body issue도 가지지만 canonical room의
대체 정의로 사용하지 않는다.

```text
r01/r00100@588  r01/r00265@632  r01/r00269@632  r01/r00277@676
r01/r00332@632  r01/r00376@588  r01/r00400@588  r01/r00412@2220
r01/r00422@588  r01/r00547@632  r01/r00593@588  r01/r00665@588
r01/r00708@544  r01/r00748@588  r01/r00759@588  r05/r00547@632
r05/r00593@2132
```

이 overlay는 strict failure를 숨기기 위한 skip 목록이 아니다. admission manifest는
전체 3,216 source path를 보존하면서 2,341 canonical path를 catalog 대상으로 하고,
875 historical artifact를 `noncanonical-path`로 `Ignored()`에 남긴다. 위 17개도
그 875개 중 일부로 raw SHA/issue evidence는 보존해야 한다.

## 실제 decoder gap 판정

현재 판정은 **0건 confirmed**다.

1. `TestTrackedRoomHeaderCorpus`가 3,216/3,216 header를 읽는다. ABI/경로 이전의
   header truncation이나 exit count 오류가 이 63을 설명하지 않는다.
2. `TestInspectionCorpus`가 3,216/3,216을 조사하고 `structuralFailures=0`을
   기록한다. 63개는 모두 inspection issue가 있으며 issue offset도 field boundary
   안에 있다.
3. `TestRoomBodyCorpusExceptionAudit`가 fixture의 path, size, consumed, SHA-256,
   issue 순서와 63/100/80/13/7 counts를 재확인한다. zero-value admission policy와
   strict decoder는 모든 audited exception을 계속 거부한다.
4. C `read_rom`은 text encoding을 검증하지 않고 trailing EOF도 확인하지 않는다.
   그러므로 C에서 읽혔을 가능성과 Go strict admission 가능성을 혼동하지 않는다.

향후 C differential에서 “원본 bytes가 strict EUC-KR/terminator/EOF 계약을 실제로
만족하는데 Go가 거부”하는 파일이 나오면 그때만 `decoder gap`으로 재분류하고,
해당 파일의 독립 regression fixture를 먼저 추가한다. 현재는 그런 파일이 없다.

## 다음 조치와 승격 조건

| 분류 | 다음 조치 | 승격 조건 |
| --- | --- | --- |
| invalid EUC-KR (80) | 파일별 C 실행/출력 또는 승인된 인코딩 매핑을 확보하고, 원본 bytes와 저장/표시 결과를 differential 비교한다. | replacement/deletion/임의 mapping 없이 파일별 근거와 회귀 fixture 확보 |
| missing terminator (13) | C writer가 80-byte field를 어떻게 만들었는지와 실제 사용(`strlen`) 경계를 조사한다. | NUL 합성·절삭의 원작 동등성 또는 quarantine 결정이 문서화됨 |
| trailing data (7) | `resave_*`의 no-truncate 가능성과 생성 commit/file history를 대조하고 tail을 별도 hash로 보존한다. | tail이 stale인지 유효 record인지 파일별 증명; 자동 trim 금지 |
| path/provenance (17) | historical artifact를 계속 ignored/audit-only로 두고, canonical path가 없는 room은 별도 source review를 받는다. | path owner, header ID, duplicate/alternate definition 정책이 manifest에 고정됨 |
| decoder gap (0) | 새 증거가 생길 때만 bounded decoder regression을 추가한다. | 현재는 조치 없음; gap으로 승격하지 않음 |

63개가 해결되기 전에는 `TestRoomBodyCorpus`를 skip한 전체 Go gate나 compatibility
policy catalog load를 데이터 이관 완료로 해석하지 않는다. 이후에도 room graph
reference, monster/item canonical graph, PG seed/restart, backup/restore,
re-import idempotence를 별도 G4 증거로 추가해야 한다.

## 재현 명령과 결과

실행 위치는 `server/`다.

```text
go test -race ./internal/world -run '^(TestInspectionCorpus|TestRoomBodyCorpusExceptionAudit|TestTrackedRoomHeaderCorpus|TestLegacyRoomCatalogFollowsCPathAndRecordsHistoricalArtifacts)$' -count=1 -v
PASS
  TestInspectionCorpus: rooms=3216 issueRooms=63 structuralFailures=0
  TestRoomBodyCorpusExceptionAudit: PASS
  TestTrackedRoomHeaderCorpus: decoded 3216 original room headers
  TestLegacyRoomCatalogFollowsCPathAndRecordsHistoricalArtifacts: PASS

go test -race ./internal/world -run '^TestRoomBodyCorpus$' -count=1 -v
KNOWN FAILURE (exit 1): rooms=3216 failed=63; world admission remains disabled
```

`TestRoomBodyCorpus`의 실패는 이번 감사가 만든 regression이 아니라 이 문서가
고정하는 현재 known failure다. 감사 범위에는 커밋·push·배포가 없다.

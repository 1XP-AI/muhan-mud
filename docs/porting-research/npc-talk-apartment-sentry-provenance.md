# `아파트_수위_아저씨-127` NPC 대화 자산 provenance

조사 기준: 2026-09-09. 이 기록은 Go 런타임이 아니라 체크인된 자산의 원본
바이트와 저장소 이력만 다룬다.

## 결론

정확한 복구 원본을 찾지 못했다. `resources_utf8/objmon/talk/아파트_수위_아저씨-127`은
현재도 CP949 strict decode가 실패하는 유일한 talk 자산으로 취급해야 하며, 임의의
문자·바이트 보정이나 추정 기반 삭제를 적용하지 않는다. 전체 `TalkCatalog` admission은
이 파일을 만나면 계속 실패해야 한다.

## 확인된 바이트와 저장소 증거

| 항목 | 값 |
| --- | --- |
| 정규화 경로 | `resources_utf8/objmon/talk/아파트_수위_아저씨-127` |
| legacy 경로 hex | `6F626A6D6F6E2F74616C6B2FBEC6C6C4C6AE5FBCF6C0A75FBEC6C0FABEBE2D313237` |
| legacy 경로(CP949) | `objmon/talk/아파트_수위_아저씨-127` |
| blob SHA-1 | `de6917d05fafb188cc52a0cad70854d5370a5265` |
| SHA-256 | `e64e47a24cf48b54c3c7aab271994c2e46d30196ed45273f6fa492b2f7912674` |
| 크기 | 552 bytes |

`resources_manifest/path-alias.v1.tsv`와 `resource-map.v1.json`은 위 legacy 경로,
정규화 경로, blob SHA-1, SHA-256, 552-byte 크기를 같은 값으로 기록한다. 따라서
정규화 단계에서 내용을 바꾼 증거는 없고, 체크인 파일은 legacy blob의 byte-for-byte
복사본이다.

Git 이력에서 이 blob은 다음 커밋들에 같은 object로 남아 있다.

- `f3cdc1ed08f5a7e2e13d963ea864c5bc8686165d` (2012-01-19 초기 커밋): CP949 byte-path
  `objmon/talk/<동일 경로 bytes>`로 최초 추가.
- `b8c54c055f781c04a07efc29f42276e367ebda7b` 및
  `51742245989fbbd1b3c21b7caad99f54d1fd06fd` (2026-02-08): `resources_utf8`에
  정규화 경로로 추가했지만 같은 blob을 사용.
- `29183e75852e1224a715870560c9726d8decd7b2` (2026-02-11): legacy byte-path를
  제거하고 정규화 tree를 유지한 변환 커밋. 해당 파일 내용의 교정 기록은 없다.

현재 저장소의 reachable/unreachable Git blob 전체를 대상으로 동일한 문맥과
one-byte deletion 후보를 비교했지만, 유효 CP949 551-byte 사본이나 별도 원문 blob은
발견되지 않았다. 따라서 아래 후보는 관찰용 가설일 뿐 복구본이 아니다.

```text
현재 파일: 552 bytes, SHA-256 e64e47a24cf48b54c3c7aab271994c2e46d30196ed45273f6fa492b2f7912674
offset 216의 0xBA를 제거한 가설: 551 bytes, SHA-256 aa7beceba6078f0c0fa9604914944da7d70badc635d96a191bf54310cde2ccf5
```

가설 결과가 자연스러운 문장을 만들더라도, 이를 원본 증거로 간주하지 않는다.

## strict decoding 결과

- 전체 파일은 UTF-8이 아니다(첫 byte `0xC0`).
- Python 표준 `bytes.decode("cp949")` strict decoder는 **전역 offset 216**에서
  `0xBA` 때문에 실패한다(EUC-KR strict도 같은 위치에서 실패).
- Go `x/text` EUC-KR decoder는 이 잘못된 byte를 오류 대신 U+FFFD로 치환할 수 있으므로,
  Go의 `decodeTalkText`가 replacement rune을 다시 거부해야 한다. 회귀 테스트는 두
  경계를 모두 고정한다.
- 실패 위치는 **6번째 줄, 줄 내부 offset 39**이며, 주변 bytes는
  `...C0 BA BA 20 BC F6 BD C3...`이다. 즉 앞의 `C0 BA` 뒤에 단독 `BA`가 있고
  다음 byte가 ASCII space다.
- replacement decode는 해당 위치를 `�`로 바꾸므로 게임 출력에 사용하면 안 된다.

회귀 테스트
[`talk_catalog_resource_regression_test.go`](../../server/internal/world/talk_catalog_resource_regression_test.go)는
위 크기·SHA-256·offset·문맥을 고정하고, strict talk decode와 단일 파일로 격리한
`LoadTalkCatalog` admission이 모두 거부하는지 검증한다. 정확한 원본 바이트와 provenance가
확보되기 전에는 이 테스트의 기대값이나 자산을 완화하지 않는다.

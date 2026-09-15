# 상점 경제 bounded slice

이 문서는 Go 서버의 상점 경제 중 `품목`(list)과 `팔아`(sell)만 구현한 수직 slice의 계약과 의도적인 경계를 기록한다. 기존 구매 경로(`QuoteShopPurchase`/`BuyShopItem`/`RunShopPurchase`)와 파일·상태 전이를 공유하지 않으며, 판매 구현은 `shop_marketplace.go`가 소유한다.

## 원본 근거

- `src/global.c`는 `품목`을 `list`, `팔아`를 `sell`에 연결한다. 영어 별칭은 원본 전역 명령 계약에 없다.
- `src/command7.c:list`는 `RSHOPP` 방에서 바로 다음 방을 읽고 `first_obj` 순서대로 이름과 `value`를 출력한다.
- `src/command7.c:sell`은 `RPAWNS` 방에서 직접 소지 root를 찾고, `MIN(value / 2, 100000)`을 지급한다. 지급액이 20 미만이거나 저품질 무기/완드/열쇠, `ONEWEV`, 내용물이 든 물건, 두루마리/독약이면 거부한다. `OINVIS`는 `PDINVI`가 있을 때만 허용된다.
- `src/mtype.h`의 room/object/player flag 번호를 그대로 사용한다. `RPAWNS=2`, `RSHOPP=0`, `RNOTEL=12`, `PDINVI=21`, `OINVIS=2`, `ONEWEV=50`, `OTEMPP=8`, `OPERM2=9`, `OPERMT=0`이다.
- `docs/rom_stor`는 상점 저장고가 상점 방의 다음 room 번호이고 `RNOTEL`이어야 하며, 저장고 물품은 영구 물품이어야 한다고 설명한다.

## 구현된 계약

### `품목`

온라인 canonical player가 `RSHOPP` 방에 있을 때만 실행된다. 다음 room의 canonical `ItemCollection.Inventory` root를 저장 순서 그대로 읽어 deterministic receipt와 평문 응답을 만든다. 읽기 전용이며, 보이지 않는 저장고 stock도 원본 `list`처럼 별도 visibility 필터 없이 표시한다. 음수 가격·음수/손상 weight·legacy `Resource.Objects` fallback은 fail-closed다.

### `팔아 <정확한 이름> [occurrence]`

온라인 canonical player가 `RPAWNS` 방에 있고, 다음 `RNOTEL` room이 canonical pawn storage일 때만 실행된다. 이름은 대소문자를 무시하는 정확한 직접 inventory root 일치이며, 중복 이름은 1-based occurrence를 명시해야 한다. prefix/key selection은 source fixture가 없으므로 허용하지 않는다.

검증을 통과하면 root의 canonical item ID를 유지한 채 player inventory에서 pawn storage로 이동하고, storage 소유권을 위해 `OPERMT`를 설정하며 `OTEMPP`/`OPERM2`를 해제한다. item weight와 소지 weight 전후 값, 기존 temporary 여부, visibility, 정확한 `value/2` capped payout을 receipt에 남긴다. gold, ownership, room flags, duplicate IDs, overflow, nested contents의 실패는 상태·receipt 모두 생성하지 않는다. 성공 시 `PHIDDN`을 해제한다.

## 의도적으로 보류한 source branch

- 원본의 시간/RNG 기반 “기분이 좋아 두 배” branch는 재현 가능한 Go command receipt 계약이 없고 원본 코드도 이후 기본 지급을 다시 더하는 모호한 경로를 가지므로 구현하지 않는다.
- 원본 `find_obj`의 prefix/key/숨김(`OHIDDN`) 선택 의미는 현재 canonical parser에 source fixture가 없어 exact-name + explicit occurrence로 fail-closed한다. `OINVIS`/`PDINVI`만 검증한다.
- 원본 판매의 broadcast, ANSI `obj_str`, allocator/free 및 legacy floor linked-list는 durable state에 직접 대응하지 않아 평문 receipt와 canonical graph 이동으로 제한한다.
- merchant NPC 판매/구매, `가치`, repair shop, 재고 refresh/상점 운영자 화면은 이 slice에 포함하지 않는다.
- 원본 list가 `load_rom` 실패 시 출력하는 fallback과 저장고 `RNOTEL` 누락은 운영 상태를 추측하지 않도록 Go에서 거부한다.

## 검증

전용 world/session/transport 테스트가 parser alias, deterministic read-only list, exact payout, flags/visibility/weight/duplicate/nested/overflow 경계, receipt replay와 connector dispatch를 검증한다. PostgreSQL 통합 테스트는 `MUHAN_SHOP_MARKETPLACE_TEST_DATABASE_URL`이 설정된 경우에만 실행하고, 없으면 격리 DB가 없어 skip한다. `src/frp.new`는 이 작업에서 읽기·수정·stage하지 않는다.

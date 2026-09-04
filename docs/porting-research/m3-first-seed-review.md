# M3 first-seed 독립 설계 검토

검토 범위: M3 migrations 090/100/110/120/130, PostgreSQL 17 통합·계약 테스트, `m3-shadow-receipt-contract.md`, `m3-journal-v2-gates.md`, `character_save_journal_v2_writer.*`, `character_save_journal_v2_live_ops.*`. 본 문서는 설계 검토 산출물이며 migration/code/DB 상태를 변경하지 않는다.

## 결론: absent의 두 의미

`game_character_legacy_heads`에서 `head_state='absent'`는 “파일이 없는 것을 관찰한 유효 head”다. `storage_format`은 양수, `head_sha256`은 NULL, revision은 0 이상이어야 하며, 이 행이 이미 있으면 `record_legacy_published_receipt(... expected_state='absent', expected_sha256=NULL)`가 CAS 입력으로 사용될 수 있다. 반대로 head row 자체가 없거나 v3 route가 반환하는 `uninitialized`는 “DB가 아직 관찰을 신뢰할 수 있게 고정하지 못한 상태”다.

`record_legacy_published_receipt`는 missing-row를 만났을 때 `expected_state='existing'`이고, route의 lowercase `imported_file_sha256`가 caller의 exact observed prehash와 같을 때에만 revision-0 `existing` row를 만들고 계속한다. `expected_state='absent'`, imported hash NULL, malformed/non-lowercase imported hash, 또는 imported hash와 prehash 불일치는 모두 P0001 `shadow receipt head is uninitialized`/동등 거부로 끝나며 head row·receipt row를 만들지 않는다. 따라서 “absent file을 DB가 first-seed”하는 정책은 현 계약에 없다; 그것은 legacy file authority가 먼저 관찰·고정한 별도 미래 프로토콜이어야 한다.

## 허용/거부 규칙

허용: canonical route가 같은 `(world_id, legacy_name_key)`에서 authoritative `character_id`/shard/format/lifecycle을 반환하고, lifecycle은 `imported_unclaimed|provisioning|active`, storage format은 1, writer tuple은 현재 installed·unsealed·unexpired exact tuple이다. C가 같은 PVC의 persisted tuple과 process-lifetime `.m3-writer.lock`을 보유하고, exact expected precondition으로 stage/live를 검증한 뒤 `PREPARED → LEGACY_PUBLISHED`를 남긴 경우에만 hash-only receipt를 시도한다.

거부: route identity/name/world/shard/format/lifecycle drift, malformed canonical response/imported hash/head shape, malformed digest/envelope, stale or different writer, expired writer without exact renewal, sealed writer, successor-installed predecessor, absent expected state against a missing head, stale revision/prehash, and same command UUID with any changed digest/payload. SQLSTATE `22023`/transport INVALID는 malformed input/evidence이고 P0001/REJECTED is contract rejection; both leave DB unchanged, while local invalid/ambiguous evidence freezes rather than repairs or consults DB for permission.

## TDD 불변성 표

| 시나리오 | DB 판정 | 파일 판정 / 반드시 무변인 것 |
|---|---|---|
| 두 writer/process 경쟁 | 같은 PVC flock은 한 process만 보유; DB epoch은 one live writer. B는 live predecessor 동안 P0001. | loser는 writer/route/RPC/file mutation 없음; winner의 stage, live, journal만 계약 순서대로 진행 |
| same-digest retry | 동일 `(character_id, command_id)`의 12-field envelope이면 exact retry는 read-only이며 current exact tuple 및 일관된 head가 필요. receipt/head timestamp·revision 재전이 없음. | already-published live bytes, journal marker, stage topology/inode/link count 보존; local ACK marker만 exact evidence 재검증·durably converge 가능 |
| different-digest retry / UUID payload mismatch | 기존 receipt와 command UUID가 같아도 digest/route/epoch/revision/pre/post가 다르면 P0001; receipt/head unchanged | live, stage, PREPARED/LEGACY_PUBLISHED/DB_ACKED evidence byte-for-byte 보존; overwrite/regenerate/delete 금지 |
| expired writer | same persisted instance + same installed unsealed epoch만 exact renew 후 ACK 가능; expiry 자체는 local publish 권한을 취소하지 않음 | DB offline/expired 동안 local legacy publish/backlog 가능하지만 ACK 전 evidence 보존; renewal 전 DB mutation 없음 |
| sealed writer | seal 후 old tuple renew/receipt/seal은 영구 P0001 | published live/journal/stage evidence 그대로; 새 writer가 old file을 덮거나 rollback하지 않음 |
| successor writer installed | A가 sealed+expired drain 후 B install하면 A tuple은 영구 fence; A receipt/head unchanged | A의 pending local evidence freeze/retain; B가 A stage/journal을 권위로 취급하거나 삭제하지 않음 |
| route identity drift | route row 재잠금 후 supplied world/name/character/shard/format/lifecycle mismatch P0001; canonical digest mismatch 22023 | callback output은 실패 시 stale bytes가 없어야 하며, stage/live/journal/DB 모두 무변 |
| malformed imported hash | missing head seed 조건을 만족하지 못함(P0001/no row); route/transport malformed response는 deferred/invalid fail-closed | imported hash를 normalize/guess하지 않음. C 파일, stage, journal, DB head/receipt 모두 무변 |
| partial initial receipt/marker | DB receipt는 partial state가 아니며, incomplete/invalid DB call은 no receipt/head mutation | partial/conflicting `.published.tmp`, partial marker, consumed/alias/nlink ambiguity를 exact bytes/inode/link topology 그대로 보존; `LEGACY_PUBLISHED`만 exact live posthash로 재시도 |
| absent file with existing `head_state='absent'` | valid absent head + expected absent/null + next revision + exact tuple이면 receipt/head CAS 허용 | no-replace `linkat → parent fsync → stage unlink → stage fsync`; destination race는 conflicting target/stage를 덮지 않고 freeze |
| missing head row (`uninitialized`) + absent observation | 현재 DB RPC는 seed 거부 P0001; absent-head seeding은 future file-authority protocol | stage/live/journal untouched; DB가 파일을 publish/rollback/추측하지 않음 |

## 포팅 판정

Writer C는 descriptor-relative trusted root/ancestry, exact persisted instance+epoch, process-lifetime flock을 검증한다. live-ops adapter는 READY transport와 canonical bounded route만 통과시키고, route/receipt 실패 시 output을 채우지 않으며, DB를 local publish authorizer로 사용하지 않는다. PG17 계약은 absent v3 route revision 0, first receipt revision 1, exact retry, malformed response clearing, class 22→INVALID, P0001→REJECTED, offline→DEFERRED, sealed/successor permanent fence를 검증한다.

초기 파일이 실제로 absent이면 현재 계약상 “허용 가능한 first seed”는 `head_state='absent'`를 DB에서 미리 확정해 두는 경우뿐이다. missing row를 receipt RPC가 자동 생성하도록 확장하거나, imported hash를 absent의 증거로 재해석하는 것은 legacy file authority/immutable receipt-head CAS 계약과 테스트 기대를 깨므로 거부해야 한다.

## M7a additive absent-head seed

`20260918000000_m3_absent_head_seed.sql`은 receipt RPC와 분리된 `mud_writer` 전용 `private.seed_game_character_absent_head(...)`를 추가한다. 호출 전제는 C가 held trusted root에서 canonical legacy file이 실제로 없는 것을 이미 증명했다는 것이며, DB는 file을 read/write/publish하거나 그 부재를 추측하지 않는다.

RPC는 receipt와 같은 순서로 world advisory lock, character `FOR SHARE`, writer epoch `FOR UPDATE`, head `FOR UPDATE`를 취하고 canonical world/name, character identity, storage format, exact unsealed/unexpired writer instance+epoch를 다시 확인한다. `imported_file_sha256`가 있거나 missing head가 아닌 경우에는 fail-closed하며, 오직 missing head에 `(absent, NULL, format, revision=0, writer_epoch=NULL)`만 삽입한다; 그 exact baseline의 retry만 timestamp를 포함해 무변이 성공한다.

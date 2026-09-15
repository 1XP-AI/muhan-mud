# Go G4 백업·복구 감사

2026-09-10 기준으로 Go `mud_go` 저장 경계와 로컬 복구 증거를 대조한다. 이 문서는
운영 데이터나 Supabase에 접속하지 않으며, 백업 파일의 장기 보관·암호화 정책을
승인하거나 배포 완료를 뜻하지 않는다.

## 기존 증거의 범위

| 대상 | 실제로 검증하는 것 | 검증하지 않는 것 |
| --- | --- | --- |
| `WorldBackup` 단위 테스트 | format/version, canonical JSON, state SHA-256, unknown/trailing JSON, `world.DecodeState` 거부 | PostgreSQL 백업 아카이브와 프로세스 재시작 |
| `TestPostgresWorldBackupRestoreFencesReceipts` | disposable PostgreSQL에서 expected revision, receipt 보유 대상의 fail-closed 복원, Force의 receipt 삭제·writer epoch 증가·구 writer 거부 | `pg_dump`/`pg_restore`가 실제 Go receipt 행을 보존하는지 |
| `server/cmd/muhan/backup_cli_test.go` | 0600 regular file, 64 MiB bound, atomic publication/no-overwrite, flag boundary | 실제 DB를 대상으로 한 CLI export/import |
| `scripts/verify-replay-db-backup-local.sh` | legacy `private.game_character_*` snapshot/receipt 테이블의 custom-format dump/restore와 C/Rust reader 재생 | Go `mud_go.worlds`, `world_commands`, `world_writer_claims` |

기존 Go API/CLI 구현은 애플리케이션 snapshot envelope와 원자 복원을 제공하지만,
legacy snapshot harness는 별도 스키마를 대상으로 한다. 따라서 둘을 합쳐 “Go DB
백업 복원 완료”라고 해석할 수 없다.

## 보강한 disposable PostgreSQL 경계

`bash scripts/run-go-backup-restore-local.sh --allow-disposable`는 미리 설치된
`postgres:17-alpine`만 사용한다. 스크립트가 만든 loopback 전용 컨테이너와 두 개의
임시 데이터베이스만 소유하며 종료 시 컨테이너와 archive 임시 파일을 정리한다.

1. Go storage 테스트가 source DB에 world revision 2, command receipt 2개,
   writer claim 1개를 저장한다.
2. 컨테이너 내부의 PostgreSQL 17 `pg_dump --format=custom`으로 source DB를
   archive하고, 빈 target DB에 같은 이미지의 `pg_restore --exit-on-error`로
   복원한다.
3. Go storage 테스트가 target에서 revision/state와 두 receipt의 동일 command ID
   replay를 확인한다. replay 결과는 저장된 response/revision을 사용하고 새
   revision을 만들지 않는다.
4. 복원된 writer epoch을 새 claim이 올린 뒤, 이전 epoch을 가진 writer가 새
   command를 저장하지 못하는지 확인한다. 새 writer의 후속 command는 revision 3으로
   저장되어 복원된 receipt와 revision fencing이 함께 동작함을 확인한다.

전용 테스트 이름은 `TestPostgresWorldBackupPhysicalRestore`이며, `seed`와 `verify`
두 단계로만 실행하도록 환경 변수를 요구한다. 환경 변수가 없으면 일반 Go test에서
skip되므로 DB 테스트 skip을 성공 증거로 세지 않는다.

## 인수 상태

- Go schema의 실제 custom-format PostgreSQL backup→restore, receipt replay, stale
  writer fencing: 위 스크립트가 PASS한 로컬 disposable 증거로 기록한다.
- 파일 export/import는 checksum·권한·원자 publication까지 구현되어 있으나 운영
  보관 위치, 접근 통제, 암호화/키 회전, retention, off-site 복사와 정기 복원 연습은
  미완료다.
- PostgreSQL 서버 장애/PITR, 전체 레거시 데이터 이관의 duplicate/loss 대조, 전투·tick
  중 프로세스 장애, Supabase 운영 연결, WSS/Ingress와 testnet 배포는 이 증거의 범위
  밖이며 G4/G5 승격 조건으로 남긴다.

로컬 검증은 작업 트리의 Go 변경을 포함해 실행하고, CI·push·운영 DB·클라우드 자원은
사용하지 않는다.

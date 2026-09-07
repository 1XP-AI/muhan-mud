# Testnet readiness after local stack GREEN — 2026-09-07

## Verified, read-only observations

- Local application source `ad8f9de` passes frozen Docker stack acceptance:
  32 tests, zero failures/skips. This uses deterministic Auth responses.
- Context `testnet-1xp`, namespace `default`, release `muhan-mud-testnet` is
  deployed at Helm revision 9, chart `muhan-mud-0.1.8`, updated September 5.
- Web and `/auth/v1/health` both return HTTP 200. This proves reachability,
  not real login/token/refresh or user-to-character acceptance.
- Auth runs `supabase/gotrue:v2.189.0`, email signup and autoconfirm enabled.
  All inspected application/Auth pods are ready.
- MUD/Gateway/Web use `tech1xp/muhan-mud-testnet:latest`; observed runtime
  digest is `sha256:5f025ee1732981b2cd6d9aaef18ffc858b90905f6cc1e80a9bbf8d141ce174ec`.
  User-supplied release values do not specify sourceRevision/image.digest.
  The deployed source commit has not been established from this evidence.
- MUD container has neither MUD_M3_MODE nor MUD_M3_PLAYER_SNAPSHOT_V1 env.
  Gateway/Web onboarding is true; current release values do not specify
  evidence-first completion, artifact relay, or normalized-shadow options.
  Do not interpret absent values alone as a complete rendered-value audit.
- Local infrastructure scoped tests: rendered-chart, release-wrapper and
  normalized-deployment-prerequisites, **54 passed / 0 failed**.
  Log: `/tmp/muhan-release-gates.log`. Infra latest scoped commit `3cf3503d`.

## Deployment blockers and next executable gates

1. Extend disposable stack acceptance to the pinned real GoTrue Auth service.
   Use only synthetic test users/passwords and isolated DB/network; verify real
   password login/JWT consumption and both browser onboarding paths. Do not
   create production test users or read production secrets for this check.
2. Provide a local-only production-image build path. The current infra
   `muhan-mud/build.sh` calls root `build.sh`, which selects
   `cloud-tech1xp-testnet`, targets linux/amd64 and immediately pushes.
   **Do not run that wrapper under the local-first policy.** The successful
   local stack image is a test image, not evidence that the release image builds.
3. Review/publish the tested private source revision deliberately, build the
   runtime locally, verify its source identity, then pin its image digest.
   Do not substitute a mutable branch/tag or guess an OCI source revision.
4. Verify backup/restore readiness and current schema; render/lint the exact
   staged upgrade. Prepare schema before enabling M3 shadow and normalized
   comparison. Follow the infrastructure NORMALIZED-SHADOW.md staged gates.
5. Run real testnet acceptance against the pinned deployment, including login,
   provision/claim, ordinary gameplay, restart/recovery and comparison evidence.
   M3 shadow is not a database gameplay-authority cutover. Remaining DB/Rust
   authority milestones retain their separate differential/cutover gates.

No cluster mutation, secret access, test-user creation, cloud build, image push,
GitHub Actions dispatch, or deployment occurred during this readiness check.
This records incomplete gates; it does not mark the overall goal complete.

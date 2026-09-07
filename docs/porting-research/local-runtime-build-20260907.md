# Production runtime: local build gate

## Latest execution

The local runtime build ran in session `36085` with source
`2cfedc036f613fd8c902257aa1125c4222928b5b`. Evidence directory:
`/var/folders/7s/1pkt8kzx41zg5k2ffpkz_zpr0000gn/T/muhan-runtime-build.RNIORD`.
BuildKit confirms the default docker driver and loads the named local source
context (42.80 MB); the private-fetch stage is bypassed without credentials.
It exited 1 during runtime apt-get update: Debian Bookworm repositories
reported invalid signatures (apt exit 100). No runtime image was produced.
The cause of the signature failures is not yet established; do not bypass
signature validation. Image completion and runtime binary tests remain pending.

The investigation found the original inspection process had the shared
`~/.docker/buildx/.lock` open, alongside other projects and Docker Desktop.
An isolated config queried the same daemon immediately. The wrapper now gives
Buildx its own task-local metadata directory, without changing Docker registry
credentials or restarting the shared daemon. No other process was stopped.

Actual CLI execution also exposed an unsupported `buildx inspect --format`
flag that the initial mock had missed. A regression first reproduced the
failure, then passed after changing to captured standard inspection output
and extracting its Driver field. The complete wrapper test passes again.

## Initial gate and history

The real-Auth browser stack passed at `ead4537`; the deployment runtime image
has not yet been built or accepted. The test image is not deployment evidence.

`scripts/build-runtime-local.sh` accepts a full, locally available commit SHA
and the deployment Dockerfile. It freezes the commit with Git archive and
overrides the Dockerfile's `source` stage with a named local BuildKit context.
The deployment recipe is copied unchanged and its SHA-256 recorded. No private
Git token, cloud builder, image push, or cluster write is requested.

The target remains linux/amd64 because testnet nodes and the deployment recipe
currently use amd64. This is separate from ARM64 CI migration.

Invocation (from the repository root):

```sh
DOCKER_HOST=unix:///Users/jjangg96/.docker/run/docker.sock \
  bash scripts/build-runtime-local.sh \
  2cfedc036f613fd8c902257aa1125c4222928b5b \
  /Users/jjangg96/Documents/1xp/tesnet-1xp.nosync/muhan-mud/Dockerfile
```

Append `--dry-run` to validate inputs without Docker access. Real execution
requires the default builder's docker driver, loads a unique local image,
and verifies architecture and OCI source revision. Task-owned scratch data
and build logs remain available; it does not prune shared caches or images.

Validation so far: shell syntax and local boundary tests pass, including
rejection of abbreviated revisions and remote Docker endpoints. Actual
named-context override, complete packaging, and executable runtime checks
remain unverified until the image build succeeds.

The boundary suite now also runs the complete wrapper with a recording mock
Docker executable. It checks the archived Gateway source exists without Git
metadata, the main context is empty, the local builder/platform/target flags
are correct, a cloud driver is rejected before building, a failed build exits
without claiming success or inspecting an image, and a wrong image architecture
is rejected. All checks pass. These are wrapper tests, not BuildKit execution
or production runtime acceptance.

Docker daemon `_ping` currently returns OK, but an existing `buildx inspect
default --bootstrap` process remains live without output after five minutes.
No duplicate build was started. Host disk has about 11 GiB available; capacity
must be checked before building this multi-stage image. No data was deleted.
The daemon info endpoint also responds: aarch64, Docker 29.6.1, overlayfs,
zero running containers. The same builder-inspection process was polled again
and is still live; the daemon response alone does not prove BuildKit readiness.

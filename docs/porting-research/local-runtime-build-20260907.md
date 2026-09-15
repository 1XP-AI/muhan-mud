# Production runtime: local build gate

## Latest execution

### Final image packaging enforcement

The infrastructure Dockerfile now runs the source-owned package verifier after
`USER muhan:muhan`, with BuildKit networking disabled and the verifier mounted
read-only from the source context. All three deployed service paths are checked
before the runtime target can succeed. This requires source commit `0183376`
or a descendant; the old example commit below predates the verifier and must
not be used with this updated recipe.

The exact verifier passed against all three generated packages under Node 22
with UID/GID 10001, read-only root, network none and read-only package mounts.
Infrastructure package, normalized prerequisite, and rendered-chart suites pass
49/49 after updating two assertions that described the old reconciler copy
path/deploy list. Log: `/tmp/muhan-runtime-chart-package-check.log`.
This verifies the check and chart contracts, not execution of the final Docker
stage: VM storage still reports zero available and the full build was not retried.

### Other service packages and reusable smoke

Built and deployed importer and relay packages from frozen `fb581b7` in
`/tmp/muhan-services-package.A538I1`. Unlike reconciler, neither service has a
service-local dist gitignore, and both existing deployment outputs contain
compiled code and pg. No speculative manifest changes were made to them.
Both modules and their packaged pg dependencies loaded in Node 22.23.2 ARM64
using the retained local test image with network disabled, read-only root,
and only the generated package directory mounted read-only.

Added `scripts/verify-service-packages.mjs`: checks the compiled module, pg
resolution relative to that module, and existence of declared node dist CLIs.
It does not connect to PostgreSQL or execute administrative commands. Passed
against all three corrected packages; failed against the original reconciler
output missing dist. This is a load/packaging smoke, not gameplay, database,
AMD64, or completed production image acceptance.

### Reconciler packaging gate

While Docker VM capacity remains zero, inspected the deployment recipe and
found it copies only reconciler package.json and dist, but no pg dependency.
The reconciler's two direct PostgreSQL adapters require pg at construction.
Added an infrastructure regression test (initially failed) and changed the
reconciler stage to pnpm deploy production output, copying its node_modules.

An actual host-side frozen-workspace install/build/deploy then exposed a second
failure: the service's dist gitignore excluded compiled output from deployment.
Adding `files: ["dist"]` to its package manifest restored that output. The
resulting standalone package successfully instantiated and closed both real
PostgreSQL adapters without connecting to a database. Evidence remains under
`/tmp/muhan-reconciler-package.pLUhRe` (`out` failed, `out-fixed` passed).
Host validation used pnpm 10.15.1 / Node 24.10.0, not runtime Node 22; complete
Docker runtime acceptance is still required. Both app and infrastructure changes
must be used together in the next build.

### Storage diagnosis after the failed build

A read-only container from retained successful test image
`muhan-local-stack-1788771444-98899:local` reports the Docker filesystem as
32 GB total, 31 GB used, **0 available / 100%**, with inode usage only 64%.
Host free space is not evidence of free Docker VM space.

After checking `docker ps -a --filter ancestor=...` returned no users, removed
only these two older task-created image tags with non-forced image removal:

- `muhan-local-stack-1788771377-97292:local`
- `muhan-local-stack-1788770502-83305:local`

Both image IDs were removed, but available space still reads zero. The latest
successful image and all test evidence remain; removed images are rebuildable.
No shared cache, volume, other project image, or other process was deleted.

Diagnostic apt-get update in the retained test image, with read-only root and
bounded tmpfs for apt lists/cache and /tmp, passed normal signature verification
for both arm64 and amd64 package indexes from all three Bookworm repositories.
This strongly points to storage pressure rather than an unsigned upstream
repository, but is not a same-image reproduction: the failed production stage
uses a different base image. Do not claim the production build is fixed.
Do not repeatedly retry the full build while VM capacity remains zero.
Next required environment step is additional Docker VM capacity or owner-scoped
cleanup approved for the remaining storage. Broad cache pruning is not allowed.

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

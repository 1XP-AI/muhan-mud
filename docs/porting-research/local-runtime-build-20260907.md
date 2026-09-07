# Production runtime: local build gate

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

Docker daemon `_ping` currently returns OK, but an existing `buildx inspect
default --bootstrap` process remains live without output after five minutes.
No duplicate build was started. Host disk has about 11 GiB available; capacity
must be checked before building this multi-stage image. No data was deleted.

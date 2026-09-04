import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { test } from 'node:test'

test('runtime image provisions and explicitly injects the pinned replay verifier path', async () => {
  const dockerfile = await readFile(new URL('../Dockerfile', import.meta.url), 'utf8')
  const runner = '/usr/local/libexec/muhan/player_snapshot_v1_replay_verify'

  assert.match(dockerfile, /cargo build --locked --release --manifest-path rust\/Cargo.toml -p muhan-core-dto --bin player_snapshot_v1_replay_verify/)
  assert.match(dockerfile, new RegExp(
    `^COPY --from=core-dto-build --chmod=0555 /build/rust/target/release/player_snapshot_v1_replay_verify ${runner}$`,
    'm',
  ))
  assert.match(dockerfile, new RegExp(`^ENV M4_PLAYER_SNAPSHOT_V1_REPLAY_VERIFY_PATH=${runner}$`, 'm'))
  assert.match(dockerfile, /^USER node$/m)
})

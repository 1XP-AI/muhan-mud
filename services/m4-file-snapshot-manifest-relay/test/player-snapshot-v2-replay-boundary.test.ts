import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { EventEmitter } from 'node:events'
import { mkdtemp, readFile, readdir, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { PassThrough } from 'node:stream'
import { test } from 'node:test'
import {
  PlayerSnapshotV2ReplayObserver,
  type PlayerSnapshotV2ReplayVerifier,
} from '../src/player-snapshot-v2-replay-observer.js'
import {
  verifyPlayerSnapshotV2Replay,
  type PlayerSnapshotV2ReplayProcessFactory,
} from '../src/player-snapshot-v2-replay-verifier.js'
import { NodePlayerSnapshotV2ReplayShadowJournal } from '../src/player-snapshot-v1-replay-shadow-journal.js'
import type { PlayerSnapshotV1ReplayObservationContext } from '../src/player-snapshot-v1-replay-observer.js'

const payload = Buffer.from('closed v2 replay payload')
const context: PlayerSnapshotV1ReplayObservationContext = {
  commandId: '11111111-1111-4111-8111-111111111111',
  characterId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
  receiptRequestSha256: 'a'.repeat(64),
  sourcePostSha256: 'b'.repeat(64),
}

function report(level: number): string {
  const digest = createHash('sha256').update(payload).digest('hex')
  return [
    'format=player-snapshot-v1-replay-verification', 'version=2', 'algorithm=sha-256',
    `input_digest=${digest}`, `canonical_digest=${digest}`, `canonical_octets=${payload.length}`,
    'inventory_node_count=0', `raw_level_u8=${level}`, '',
  ].join('\n')
}

function processFactory(stdout: string): PlayerSnapshotV2ReplayProcessFactory {
  return () => {
    const child = Object.assign(new EventEmitter(), {
      stdin: new PassThrough(), stdout: PassThrough.from([stdout]), stderr: PassThrough.from(['']),
      kill: () => true,
    })
    setImmediate(() => child.emit('close', 0, null))
    return child as never
  }
}

test('explicit v2 parser accepts each raw-U8 boundary and rejects a v1 report', async () => {
  for (const level of [0, 42, 255]) {
    const result = await verifyPlayerSnapshotV2Replay(payload, {
      runnerPath: '/opt/muhan/player_snapshot_v2_replay_verify', processFactory: processFactory(report(level)),
    })
    assert.equal(result.rawLevelU8, level)
    assert.equal(result.version, '2')
  }
  await assert.rejects(() => verifyPlayerSnapshotV2Replay(payload, {
    runnerPath: '/opt/muhan/player_snapshot_v2_replay_verify',
    processFactory: processFactory(report(42).replace('version=2', 'version=1')),
  }))
})

test('explicit v2 observer writes only closed v2 metadata', async () => {
  const journalPath = await mkdtemp(join(tmpdir(), 'm4-player-snapshot-v2-replay-journal-'))
  const digest = createHash('sha256').update(payload).digest('hex')
  try {
    for (const level of [0, 42, 255]) {
      const verifier: PlayerSnapshotV2ReplayVerifier = async () => ({
        format: 'player-snapshot-v1-replay-verification', version: '2', algorithm: 'sha-256',
        inputDigest: digest, canonicalDigest: digest, canonicalOctets: payload.length,
        inventoryNodeCount: 0, rawLevelU8: level,
      })
      const observer = new PlayerSnapshotV2ReplayObserver(
        '/opt/muhan/player_snapshot_v2_replay_verify', verifier,
        new NodePlayerSnapshotV2ReplayShadowJournal(journalPath),
      )
      assert.equal(await observer.observe(payload, context), 'observed')
    }
    const entries = await Promise.all((await readdir(journalPath)).map(async (name) => JSON.parse(await readFile(join(journalPath, name), 'utf8'))))
    assert.equal(entries.length, 3)
    assert.deepEqual(entries.map((entry) => entry.rawLevelU8).sort((a, b) => a - b), [0, 42, 255])
    for (const entry of entries) {
      assert.deepEqual(Object.keys(entry).sort(), ['characterId', 'commandId', 'format', 'rawLevelU8', 'receiptRequestSha256', 'sourcePostSha256', 'verification', 'version'])
      assert.equal(entry.version, '2')
      assert.deepEqual(Object.keys(entry.verification).sort(), ['algorithm', 'canonicalDigest', 'canonicalOctets', 'format', 'inputDigest', 'inventoryNodeCount', 'version'])
      assert.equal(JSON.stringify(entry).includes('payload'), false)
    }
  } finally {
    await rm(journalPath, { recursive: true, force: true })
  }
})

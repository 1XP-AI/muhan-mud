import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import { test } from 'node:test'
import {
  InvalidAliasTitleSnapshotV1ArtifactError,
  parseAliasTitleSnapshotV1Artifact,
} from '../src/alias-title-snapshot-v1-artifact.js'

const fixtures = new URL('../../../tests/fixtures/', import.meta.url)

async function hexFixture(name: string): Promise<Buffer> {
  return Buffer.from((await readFile(new URL(name, fixtures), 'ascii')).trim(), 'hex')
}

function u16(value: number): Buffer { const bytes = Buffer.alloc(2); bytes.writeUInt16BE(value); return bytes }
function u32(value: number): Buffer { const bytes = Buffer.alloc(4); bytes.writeUInt32BE(value); return bytes }
function field(id: number, type: number, value: Uint8Array): Buffer { return Buffer.concat([u16(id), Buffer.from([type]), u32(value.length), value]) }
function cdto(body: Uint8Array, kind = 9): Buffer {
  return Buffer.concat([Buffer.from('MUHCDTO\0', 'ascii'), Buffer.from([0, 1, 0, kind]), u32(body.length), body, createHash('sha256').update(body).digest()])
}
function rejects(bytes: Uint8Array): void {
  assert.throws(() => parseAliasTitleSnapshotV1Artifact(bytes), InvalidAliasTitleSnapshotV1ArtifactError)
}

test('AliasTitleSnapshotV1 parser accepts checked-in synthetic kind-9 CDTO fixtures and preserves terminal digests', async () => {
  const rows = [
    ['alias_title_snapshot_v1_valid_ordering.hex', '98c8072580e6ccf50bb7c625875c8c1e993e7f49e88d4b7bde7e37e20169d5cd'],
    ['alias_title_snapshot_v1_valid_boundary.hex', '4de3ec38f868a45c1c2fef057589559e52ba7740313d3941ffd26dc9790ff0b4'],
    ['alias_title_snapshot_v1_valid_max_count.hex', '27aeed92a38659dff48cc84aa1ed5df4260c4e05d381a3f619db154577862d0f'],
  ] as const
  for (const [name, digest] of rows) {
    const parsed = parseAliasTitleSnapshotV1Artifact(await hexFixture(name))
    assert.equal(parsed.canonicalDigest, digest)
    assert.equal(parsed.canonicalOctets, (await hexFixture(name)).length)
  }
})

test('AliasTitleSnapshotV1 parser keeps alias order and distinguishes absent from empty titles', async () => {
  const ordered = parseAliasTitleSnapshotV1Artifact(await hexFixture('alias_title_snapshot_v1_valid_ordering.hex'))
  assert.deepEqual(ordered.aliases.map((entry) => [Buffer.from(entry.alias).toString(), Buffer.from(entry.process).toString()]), [
    ['n', 'north'], ['a', 'attack target'],
  ])
  assert.equal(Buffer.from(ordered.title!).toString(), 'the Swift')

  const list = Buffer.from([1, 0x61, 0, 0])
  const absent = cdto(Buffer.concat([field(1, 2, u16(1)), field(2, 9, list), field(3, 11, Buffer.from([0])), field(4, 9, Buffer.alloc(0))]))
  const empty = cdto(Buffer.concat([field(1, 2, u16(1)), field(2, 9, list), field(3, 11, Buffer.from([1])), field(4, 9, Buffer.alloc(0))]))
  assert.equal(parseAliasTitleSnapshotV1Artifact(absent).title, undefined)
  assert.deepEqual(parseAliasTitleSnapshotV1Artifact(empty).title, Buffer.alloc(0))
})

test('AliasTitleSnapshotV1 parser rejects malformed, wrong-kind, and noncanonical kind-9 bytes', async () => {
  const fixture = await hexFixture('alias_title_snapshot_v1_valid_ordering.hex')
  const wrongKind = Buffer.from(fixture); wrongKind[11] = 8; rejects(wrongKind)
  const badDigest = Buffer.from(fixture); badDigest[badDigest.length - 1] ^= 1; rejects(badDigest)
  rejects(fixture.subarray(0, -1))

  const duplicateAliases = Buffer.from([1, 0x61, 0, 0, 1, 0x61, 0, 0])
  rejects(cdto(Buffer.concat([field(1, 2, u16(1)), field(2, 9, duplicateAliases), field(3, 11, Buffer.from([1])), field(4, 9, Buffer.alloc(0))])))
  rejects(cdto(Buffer.concat([field(2, 2, u16(1)), field(1, 9, Buffer.alloc(0)), field(3, 11, Buffer.from([0])), field(4, 9, Buffer.alloc(0))])))
  rejects(cdto(Buffer.concat([field(1, 2, u16(1)), field(2, 9, Buffer.alloc(0)), field(3, 11, Buffer.from([0])), field(4, 9, Buffer.from('not absent'))])))
})

test('default M4 relay remains detached from AliasTitleSnapshotV1 bytes', async () => {
  assert.doesNotMatch(await readFile(new URL('../src/cli.ts', import.meta.url), 'utf8'), /alias-title-snapshot-v1/i)
})

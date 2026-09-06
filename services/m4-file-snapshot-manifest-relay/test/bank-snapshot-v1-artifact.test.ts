import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { test } from 'node:test'
import { parseManifest } from '../src/manifest.js'
import { InvalidBankSnapshotV1ArtifactError, parseBankSnapshotV1Artifact } from '../src/bank-snapshot-v1-artifact.js'
import { relayBankSnapshotV1ArtifactsOnce } from '../src/bank-snapshot-v1-artifact-relay.js'
import { readFile } from 'node:fs/promises'

const id = '11111111-1111-4111-8111-111111111111'
function be16(n:number) { const b=Buffer.alloc(2); b.writeUInt16BE(n); return b }
function be32(n:number) { const b=Buffer.alloc(4); b.writeUInt32BE(n); return b }
function cdto(kind:number, body:Buffer) { return Buffer.concat([Buffer.from('MUHCDTO\0'),Buffer.from([0,1,0,kind]),be32(body.length),body,createHash('sha256').update(body).digest()]) }
function manifest() { return Buffer.from(`version=1\nworld_id=muhan-01\ncharacter_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\ncommand_id=${id}\ncanonical_name_hex=4d3341\nrequest_sha256=${'a'.repeat(64)}\npost_sha256=${'b'.repeat(64)}\nwriter_instance_id=bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb\nsnapshot_format=legacy-file-manifest-v1\nwriter_epoch=7\nwriter_revision=2\nstorage_format=1\nsnapshot_octets=128\n`) }
function node(index:number,parent:number|null,sibling:number) { const value=Buffer.alloc(349); value.writeUInt32BE(index,0); value.writeUInt32BE(parent ?? 0xffffffff,4); value.writeUInt32BE(sibling,8); return value }
function bank(nodes:readonly Buffer[]) { const fields=[be16(1),Buffer.from([3]),be32(4),be32(nodes.length)]; for (let i=0;i<nodes.length;i++) fields.push(be16(i+2),Buffer.from([9]),be32(349),nodes[i]!); return cdto(8,Buffer.concat([be16(1),Buffer.from([9]),be32(cdto(6,Buffer.concat(fields)).length),cdto(6,Buffer.concat(fields))])) }
function artifact(payload=bank([node(0,null,0)])) { const h=createHash('sha256').update(payload).digest('hex'); return Buffer.concat([Buffer.from(`version=1\nartifact_format=bank-snapshot-v1\nworld_id=muhan-01\ncharacter_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\ncommand_id=${id}\ncanonical_name_hex=4d3341\nrequest_sha256=${'a'.repeat(64)}\nsource_post_sha256=${'b'.repeat(64)}\nwriter_epoch=7\nwriter_revision=2\nbank_sha256=${h}\nbank_octets=${payload.length}\n\n`),payload]) }
function rejects(payload:Buffer) { assert.throws(() => parseBankSnapshotV1Artifact(`${id}.bank-snapshot-v1`,artifact(payload),parseManifest(manifest())), InvalidBankSnapshotV1ArtifactError) }
test('bank artifact parser binds M3 receipt identity and normalized canonical topology', () => { const a=parseBankSnapshotV1Artifact(`${id}.bank-snapshot-v1`,artifact(),parseManifest(manifest())); assert.deepEqual(a.nodes,[{nodeIndex:0,parentNodeIndex:null,siblingOrdinal:0}]); assert.throws(()=>parseBankSnapshotV1Artifact(`${id}.bank-snapshot-v1`,artifact(),parseManifest(Buffer.from(manifest().toString().replace('writer_revision=2','writer_revision=3'))))) })
test('bank artifact parser rejects every topology shape the C BankSnapshotV1 decoder rejects', () => {
  rejects(bank([]))
  rejects(bank([node(0,null,0),node(1,null,1)]))
  rejects(bank([node(0,0,0)]))
  rejects(bank(Array.from({ length:65 }, (_, index) => node(index,index === 0 ? null : index-1,0))))
  rejects(bank([node(0,null,1)]))
})
test('bank artifact parser rejects noncanonical ObjectV1 bytes beyond the topology prefix', () => {
  const fixedStringTail=node(0,null,0); fixedStringTail[13]=1; rejects(bank([fixedStringTail]))
  const invalidShots=node(0,null,0); invalidShots.writeInt16BE(1,324); invalidShots.writeInt16BE(2,326); rejects(bank([invalidShots]))
})
test('bank relay records exact evidence once and does not retry mismatches', async () => { const calls:string[]=[]; const fs={scan:async()=>[{name:`${id}.bank-snapshot-v1`,bytes:artifact(),receiptManifestBytes:manifest()},{name:'bad.bank-snapshot-v1',bytes:artifact(),receiptManifestBytes:manifest()}]}; const store={recordBankSnapshotV1TopologyShadow:async(a:{commandId:string})=>{calls.push(a.commandId);return calls.length===1?'RECORDED' as const:'EXACT_RETRY' as const}}; assert.equal((await relayBankSnapshotV1ArtifactsOnce('/ignored',store,fs)).recorded,1); assert.deepEqual(calls,[id]) })
test('default M4 relay entrypoint remains detached from bank evidence', async () => { assert.doesNotMatch(await readFile(new URL('../src/cli.ts', import.meta.url), 'utf8'), /bank-snapshot-v1/i) })

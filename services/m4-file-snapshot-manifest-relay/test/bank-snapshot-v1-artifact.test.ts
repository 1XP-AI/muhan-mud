import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { test } from 'node:test'
import { parseManifest } from '../src/manifest.js'
import { parseBankSnapshotV1Artifact } from '../src/bank-snapshot-v1-artifact.js'
import { relayBankSnapshotV1ArtifactsOnce } from '../src/bank-snapshot-v1-artifact-relay.js'
import { readFile } from 'node:fs/promises'

const id = '11111111-1111-4111-8111-111111111111'
function be16(n:number) { const b=Buffer.alloc(2); b.writeUInt16BE(n); return b }
function be32(n:number) { const b=Buffer.alloc(4); b.writeUInt32BE(n); return b }
function cdto(kind:number, body:Buffer) { return Buffer.concat([Buffer.from('MUHCDTO\0'),Buffer.from([0,1,0,kind]),be32(body.length),body,createHash('sha256').update(body).digest()]) }
function manifest() { return Buffer.from(`version=1\nworld_id=muhan-01\ncharacter_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\ncommand_id=${id}\ncanonical_name_hex=4d3341\nrequest_sha256=${'a'.repeat(64)}\npost_sha256=${'b'.repeat(64)}\nwriter_instance_id=bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb\nsnapshot_format=legacy-file-manifest-v1\nwriter_epoch=7\nwriter_revision=2\nstorage_format=1\nsnapshot_octets=128\n`) }
function artifact() { const node=Buffer.alloc(349); node.writeUInt32BE(0,0); node.writeUInt32BE(0xffffffff,4); node.writeUInt32BE(0,8); const graph=cdto(6,Buffer.concat([be16(1),Buffer.from([3]),be32(4),be32(1),be16(2),Buffer.from([9]),be32(349),node])); const bank=cdto(8,Buffer.concat([be16(1),Buffer.from([9]),be32(graph.length),graph])); const h=createHash('sha256').update(bank).digest('hex'); return Buffer.concat([Buffer.from(`version=1\nartifact_format=bank-snapshot-v1\nworld_id=muhan-01\ncharacter_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\ncommand_id=${id}\ncanonical_name_hex=4d3341\nrequest_sha256=${'a'.repeat(64)}\nsource_post_sha256=${'b'.repeat(64)}\nwriter_epoch=7\nwriter_revision=2\nbank_sha256=${h}\nbank_octets=${bank.length}\n\n`),bank]) }
test('bank artifact parser binds M3 receipt identity and normalized canonical topology', () => { const a=parseBankSnapshotV1Artifact(`${id}.bank-snapshot-v1`,artifact(),parseManifest(manifest())); assert.deepEqual(a.nodes,[{nodeIndex:0,parentNodeIndex:null,siblingOrdinal:0}]); assert.throws(()=>parseBankSnapshotV1Artifact(`${id}.bank-snapshot-v1`,artifact(),parseManifest(Buffer.from(manifest().toString().replace('writer_revision=2','writer_revision=3'))))) })
test('bank relay records exact evidence once and does not retry mismatches', async () => { const calls:string[]=[]; const fs={scan:async()=>[{name:`${id}.bank-snapshot-v1`,bytes:artifact(),receiptManifestBytes:manifest()},{name:'bad.bank-snapshot-v1',bytes:artifact(),receiptManifestBytes:manifest()}]}; const store={recordBankSnapshotV1TopologyShadow:async(a:{commandId:string})=>{calls.push(a.commandId);return calls.length===1?'RECORDED' as const:'EXACT_RETRY' as const}}; assert.equal((await relayBankSnapshotV1ArtifactsOnce('/ignored',store,fs)).recorded,1); assert.deepEqual(calls,[id]) })
test('default M4 relay entrypoint remains detached from bank evidence', async () => { assert.doesNotMatch(await readFile(new URL('../src/cli.ts', import.meta.url), 'utf8'), /bank-snapshot-v1/i) })

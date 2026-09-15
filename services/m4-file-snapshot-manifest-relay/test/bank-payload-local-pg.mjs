import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { createRequire } from 'node:module'
import { mkdtemp, writeFile, rm, readFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { main } from '../dist/bank-snapshot-v1-payload-cli.js'

if (process.env.BANK_PAYLOAD_LOCAL_DISPOSABLE !== '1' || process.platform !== 'linux') throw new Error('disposable Linux test only')
const port = process.env.BANK_PAYLOAD_LOCAL_PORT
if (!/^[1-9][0-9]{0,4}$/.test(port ?? '') || Number(port)>65535) throw new Error('invalid local port')
const { Client } = createRequire(import.meta.url)('pg')
const admin = new Client({ connectionString:`postgresql://postgres:contract-only-password@127.0.0.1:${port}/postgres` })
const id='a9530000-0000-0000-0000-000000000001', command='c9530000-0000-0000-0000-000000000001', writer='b9530000-0000-0000-0000-000000000001'
const hash = (bytes) => createHash('sha256').update(bytes).digest('hex')
const b16 = (n) => { const b=Buffer.alloc(2); b.writeUInt16BE(n); return b }
const b32 = (n) => { const b=Buffer.alloc(4); b.writeUInt32BE(n); return b }
const envelope = (kind,body) => Buffer.concat([Buffer.from('MUHCDTO\0'),b16(1),b16(kind),b32(body.length),body,Buffer.from(hash(body),'hex')])
const nodes = [null,0,0].map((parent,index) => {
  const b=Buffer.alloc(349); b.writeUInt32BE(index); b.writeUInt32BE(parent ?? 0xffffffff,4); b.writeUInt32BE(index===2?1:0,8)
  b.writeBigInt64BE(index===0 ? -9223372036854775808n : BigInt(index),312)
  return b
})
const graph=envelope(6,Buffer.concat([b16(1),Buffer.from([3]),b32(4),b32(nodes.length),...nodes.flatMap((n,i)=>[b16(i+2),Buffer.from([9]),b32(n.length),n])]))
const payload=envelope(8,Buffer.concat([b16(1),Buffer.from([9]),b32(graph.length),graph]))
const directory=await mkdtemp(join(tmpdir(),'muhan-bank-pg-'))
try {
  await admin.connect()
  await admin.query("alter role mud_writer_login password 'bank-local-contract-password'")
  await admin.query(`insert into public.game_characters(id,world_id,legacy_name,legacy_name_key,legacy_shard,lifecycle,storage_format)
    values($1,'bank-payload-e2e','Bankhero','Bankhero',substr(encode(public.digest(convert_to('Bankhero','UTF8'),'sha1'),'hex'),1,2),'imported_unclaimed',1)`,[id])
  await admin.query("insert into private.game_character_legacy_heads(character_id,head_state,storage_format,revision) values($1,'absent',1,0)",[id])
  await admin.query("select * from private.acquire_game_world_writer_epoch('bank-payload-e2e',$1,clock_timestamp()+interval '3 minutes')",[writer])
  const {rows:[{request}]}=await admin.query(`select private.game_character_shadow_request_sha256('bank-payload-e2e',$1::uuid,'Bankhero',substr(encode(public.digest(convert_to('Bankhero','UTF8'),'sha1'),'hex'),1,2),$2::uuid,$3::uuid,1::bigint,1::bigint,'absent',null::text,repeat('a',64),1::smallint) as request`,[id,command,writer])
  await admin.query(`select private.record_legacy_published_receipt('bank-payload-e2e','Bankhero',$1::uuid,$2::uuid,$3::uuid,$4,1::bigint,1::bigint,'absent',null::text,repeat('a',64),1::smallint)`,[id,command,writer,request])
  const login = new Client({connectionString:`postgresql://mud_writer_login:bank-local-contract-password@127.0.0.1:${port}/postgres`})
  try {
    await login.connect(); await login.query('set role mud_writer')
    await login.query("select private.record_m4_file_snapshot_manifest_for_receipt($1,$2,$3,'legacy-file-manifest-v1',repeat('a',64),9)",[id,command,request])
  } finally { await login.end() }
  const manifest=Buffer.from(`version=1\nworld_id=bank-payload-e2e\ncharacter_id=${id}\ncommand_id=${command}\ncanonical_name_hex=42616e6b6865726f\nrequest_sha256=${request}\npost_sha256=${'a'.repeat(64)}\nwriter_instance_id=${writer}\nsnapshot_format=legacy-file-manifest-v1\nwriter_epoch=1\nwriter_revision=1\nstorage_format=1\nsnapshot_octets=9\n`)
  const artifact=Buffer.concat([Buffer.from(`version=1\nartifact_format=bank-snapshot-v1\nworld_id=bank-payload-e2e\ncharacter_id=${id}\ncommand_id=${command}\ncanonical_name_hex=42616e6b6865726f\nrequest_sha256=${request}\nsource_post_sha256=${'a'.repeat(64)}\nwriter_epoch=1\nwriter_revision=1\nbank_sha256=${hash(payload)}\nbank_octets=${payload.length}\n\n`),payload])
  const artifactPath=join(directory,`${command}.bank-snapshot-v1`)
  await writeFile(join(directory,`${command}.manifest`),manifest,{mode:0o600})
  await writeFile(artifactPath,artifact,{mode:0o600})
  const env={M4_BANK_SNAPSHOT_V1_PAYLOAD_ENABLED:'true',M4_BANK_SNAPSHOT_V1_PAYLOAD_OUTBOX_DIR:directory,M4_BANK_SNAPSHOT_V1_PAYLOAD_DATABASE_URL:`postgresql://mud_writer_login:bank-local-contract-password@127.0.0.1:${port}/postgres`}
  assert.equal(await main(env,['--once']),0)
  const query='select payload,bank_sha256,recorded_at::text from private.game_character_bank_snapshot_v1_payloads where character_id=$1 and command_id=$2'
  const before=(await admin.query(query,[id,command])).rows
  assert.equal(before.length,1); assert.deepEqual(before[0].payload,payload); assert.equal(before[0].bank_sha256,hash(payload))
  assert.equal(await main(env,['--once']),0)
  assert.deepEqual((await admin.query(query,[id,command])).rows,before)
  assert.deepEqual(await readFile(artifactPath),artifact,'relay does not alter immutable source')
  const broken=Buffer.from(artifact); broken[broken.length-1]^=1
  await writeFile(artifactPath,broken,{mode:0o600})
  assert.equal(await main(env,['--once']),1)
  assert.deepEqual((await admin.query(query,[id,command])).rows,before)
  console.log('GREEN real bank filesystem -> CLI -> writer login -> PG: nested bytes preserved, retry immutable, corruption rejected')
} finally {
  await admin.end()
  await rm(directory,{recursive:true,force:true})
}

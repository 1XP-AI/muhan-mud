import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import { spawnSync } from 'node:child_process'
if(process.env.BANK_PAYLOAD_LOCAL_DISPOSABLE!=='1'||process.platform!=='linux') throw new Error('disposable Linux only')
const port=process.env.BANK_PAYLOAD_LOCAL_PORT, binary=process.env.BANK_TRANSFER_PLANNER
if(!/^[1-9][0-9]{0,4}$/.test(port??'')||Number(port)>65535||!binary?.startsWith('/')) throw new Error('invalid local configuration')
const {Client}=createRequire(import.meta.url)('pg')
const db=new Client({connectionString:`postgresql://postgres:contract-only-password@127.0.0.1:${port}/postgres`})
const sha=b=>createHash('sha256').update(b).digest()
function rebody(wire,body) { return Buffer.concat([wire.subarray(0,16),body,sha(body)]) }
function playerGold(wire,value) {
  const body=Buffer.from(wire.subarray(16,-32))
  for(let p=0;p<body.length;p+=7+body.readUInt32BE(p+3)) {
    if(body.readUInt16BE(p)===25) { assert.equal(body.readUInt32BE(p+3),8); body.writeBigInt64BE(value,p+7); return rebody(wire,body) }
  }
  throw new Error('gold field missing')
}
function bankGold(wire,value) {
  const outer=Buffer.from(wire.subarray(16,-32)), graph=outer.subarray(7)
  const body=Buffer.from(graph.subarray(16,-32)); body.writeBigInt64BE(value,330)
  rebody(graph,body).copy(outer,7); return rebody(wire,outer)
}
function planned(state,direction,amount,badDigest=false) {
  const lengths=Buffer.alloc(8); lengths.writeUInt32BE(state.player_payload.length); lengths.writeUInt32BE(state.bank_payload.length,4)
  return spawnSync(binary,[direction,String(amount),badDigest?'0'.repeat(64):state.player_hash,state.bank_hash],{
    input:Buffer.concat([lengths,state.player_payload,state.bank_payload]),maxBuffer:9*1024*1024,timeout:5000,
  })
}
const id='a9190000-0000-0000-0000-000000000001'
const read=async()=> (await db.query("select revision::text,player_payload,bank_payload,encode(public.digest(player_payload,'sha256'),'hex') player_hash,encode(public.digest(bank_payload,'sha256'),'hex') bank_hash from private.game_character_paired_snapshot_states where character_id=$1",[id])).rows[0]
try {
  await db.connect()
  const raw=Buffer.from((await readFile(new URL('../../../tests/fixtures/player_snapshot_v1_canonical.hex',import.meta.url),'utf8')).trim(),'hex')
  const {rows:[{payload:rawBank}]}=await db.query("select payload from private.game_character_bank_snapshot_v1_payloads where character_id='a9530000-0000-0000-0000-000000000001'")
  const player=playerGold(raw,100n), bank=bankGold(rawBank,50n)
  await db.query(`insert into public.game_characters(id,world_id,legacy_name,legacy_name_key,legacy_shard,lifecycle,storage_format)
    values($1,'rust-pair','Pvahero','Pvahero',substr(encode(public.digest(convert_to('Pvahero','UTF8'),'sha1'),'hex'),1,2),'imported_unclaimed',1)`,[id])
  await db.query('insert into private.game_character_paired_snapshot_states values($1,0,$2,$3)',[id,player,bank])
  const commit='select * from private.commit_paired_snapshot_candidate($1,$2,$3,$4,$5)'
  for(const [index,direction] of ['deposit','withdraw'].entries()) {
    const before=await read()
    for(const negative of [planned(before,direction,25,true),planned(before,direction,999999)]) {
      assert.equal(negative.status,1); assert.equal(negative.stdout.length,0)
    }
    assert.deepEqual(await read(),before)
    const out=planned(before,direction,25)
    assert.equal(out.status,0,out.stderr.toString())
    const pl=out.stdout.readUInt32BE(0),bl=out.stdout.readUInt32BE(4)
    assert.equal(out.stdout.length,8+pl+bl)
    const p=out.stdout.subarray(8,8+pl),b=out.stdout.subarray(8+pl)
    assert.deepEqual(p,playerGold(player,direction==='deposit'?75n:100n))
    assert.deepEqual(b,bankGold(bank,direction==='deposit'?75n:50n))
    const args=[id,`c9190000-0000-0000-0000-00000000000${index+1}`,before.revision,p,b]
    assert.equal((await db.query(commit,args)).rows[0].outcome,'COMMITTED')
    assert.equal((await db.query(commit,args)).rows[0].outcome,'EXACT_RETRY')
    const after=await read(); assert.equal(after.revision,String(index+1)); assert.deepEqual(after.player_payload,p); assert.deepEqual(after.bank_payload,b)
  }
  const final=await read(); assert.deepEqual(final.player_payload,player); assert.deepEqual(final.bank_payload,bank)
  await assert.rejects(db.query(commit,[id,'c9190000-0000-0000-0000-000000000003',0,player,bank]),e=>e.code==='40001')
  assert.deepEqual(await read(),final)
  console.log('GREEN DB snapshots -> digest-bound Rust deposit/withdraw -> atomic DB pair and exact retry; full-byte roundtrip preserved')
} finally { await db.end() }

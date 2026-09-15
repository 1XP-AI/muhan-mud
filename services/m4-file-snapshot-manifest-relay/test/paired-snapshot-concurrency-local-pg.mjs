import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { readFile } from 'node:fs/promises'

if (process.env.BANK_PAYLOAD_LOCAL_DISPOSABLE !== '1' || process.platform !== 'linux') throw new Error('disposable Linux test only')
const port=process.env.BANK_PAYLOAD_LOCAL_PORT
if (!/^[1-9][0-9]{0,4}$/.test(port ?? '') || Number(port)>65535) throw new Error('invalid local port')
const {Client}=createRequire(import.meta.url)('pg')
const connectionString=`postgresql://postgres:contract-only-password@127.0.0.1:${port}/postgres`
const client=()=>new Client({connectionString,statement_timeout:10000})
const observer=client()
const fixture=async (profile)=>Buffer.from((await readFile(new URL(`../../../tests/fixtures/player_snapshot_v1_${profile}.hex`,import.meta.url),'utf8')).trim(),'hex')
const original=await fixture('canonical'), changed=await fixture('tree_inventory')
const sql='select * from private.commit_paired_snapshot_candidate($1,$2,0,$3,$4)'
async function waitForBlocking(waiter,holder) {
  const deadline=Date.now()+5000
  while (Date.now()<deadline) {
    const {rows}=await observer.query('select $2::int=any(pg_blocking_pids($1::int)) as blocked',[waiter,holder])
    if(rows[0].blocked) return
    await new Promise(resolve=>setTimeout(resolve,20))
  }
  throw new Error('competing transaction did not block on the held pair')
}
try {
  await observer.connect()
  const {rows:[{payload:bank}]}=await observer.query("select payload from private.game_character_bank_snapshot_v1_payloads where character_id='a9530000-0000-0000-0000-000000000001'")
  for (const [index,mode] of ['stale','retry','rollback','disconnect'].entries()) {
    const character=`a9180000-0000-0000-0000-00000000000${index+1}`
    const first=`c9180000-0000-0000-0000-00000000000${index+1}`
    const second=`d9180000-0000-0000-0000-00000000000${index+1}`
    await observer.query(`insert into public.game_characters(id,world_id,legacy_name,legacy_name_key,legacy_shard,lifecycle,storage_format)
      values($1,$2,'Pvahero','Pvahero',substr(encode(public.digest(convert_to('Pvahero','UTF8'),'sha1'),'hex'),1,2),'imported_unclaimed',1)`,[character,`pair-race-${mode}`])
    await observer.query('insert into private.game_character_paired_snapshot_states values($1,0,$2,$3)',[character,original,bank])
    const a=client(),b=client()
    let ended=false
    try {
      await a.connect(); await b.connect()
      const {rows:[{pid:ap}]}=await a.query('select pg_backend_pid() as pid')
      const {rows:[{pid:bp}]}=await b.query('select pg_backend_pid() as pid')
      await a.query('begin')
      assert.equal((await a.query(sql,[character,first,changed,bank])).rows[0].outcome,'COMMITTED')
      // Convert rejection immediately so the intentional stale case cannot be
      // reported as an unhandled Promise rejection while observing its lock.
      const pending=b.query(sql,[character,mode==='retry'?first:second,mode==='retry'?changed:original,bank])
        .then(result=>({result}),error=>({error}))
      await waitForBlocking(bp,ap)
      if(mode==='disconnect') { await a.end(); ended=true }
      else await a.query(mode==='rollback'?'rollback':'commit')
      const outcome=await pending
      if(mode==='stale') assert.equal(outcome.error?.code,'40001')
      else {
        assert.ok(!outcome.error)
        assert.deepEqual(outcome.result.rows,[{outcome:mode==='retry'?'EXACT_RETRY':'COMMITTED',committed_revision:'1'}])
      }
      const state=(await observer.query('select revision::text,player_payload,bank_payload from private.game_character_paired_snapshot_states where character_id=$1',[character])).rows
      assert.deepEqual(state,[{revision:'1',player_payload:mode==='stale'||mode==='retry'?changed:original,bank_payload:bank}])
      const commands=(await observer.query('select command_id,committed_revision::text from private.game_character_paired_snapshot_commands where character_id=$1',[character])).rows
      assert.deepEqual(commands,[{command_id:mode==='stale'||mode==='retry'?first:second,committed_revision:'1'}])
      console.log(`GREEN paired snapshots concurrent ${mode}: observed blocking, one committed pair, one command`)
    } finally { await Promise.all([ended?Promise.resolve():a.end(),b.end()]) }
  }
} finally { await observer.end() }

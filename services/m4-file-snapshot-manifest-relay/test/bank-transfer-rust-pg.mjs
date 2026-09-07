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
const qualified=process.env.BANK_TRANSFER_QUALIFIED==='1'
const id=qualified?'a9210000-0000-0000-0000-000000000001':'a9190000-0000-0000-0000-000000000001'
const world=qualified?'qualified-rust-pair':'rust-pair'
const actor='e9210000-0000-0000-0000-000000000001',session='f9210000-0000-0000-0000-000000000001',writer='b9210000-0000-0000-0000-000000000001'
const signature='private.commit_qualified_money_transfer(uuid,text,uuid,uuid,text,uuid,bigint,uuid,bigint,text,bigint,bytea,bytea)'
const readSignature='private.read_qualified_money_transfer_state(uuid,text,uuid,uuid,text,uuid,bigint)'
const readSql='select * from private.read_qualified_money_transfer_state($1,$2,$3,$4,$5,$6,$7)'
const nativeRead=(args,options='')=>spawnSync(process.env.BANK_TRANSFER_NATIVE_READER,args.map(String),{
  env:{...process.env,PGPORT:port,PGPASSWORD:'bank-local-contract-password',PGOPTIONS:options,
    ASAN_OPTIONS:'detect_leaks=1:halt_on_error=1',UBSAN_OPTIONS:'halt_on_error=1'},
  timeout:5000,maxBuffer:9*1024*1024,
})
let login
const read=async()=> (await db.query("select revision::text,player_payload,bank_payload,encode(public.digest(player_payload,'sha256'),'hex') player_hash,encode(public.digest(bank_payload,'sha256'),'hex') bank_hash from private.game_character_paired_snapshot_states where character_id=$1",[id])).rows[0]
try {
  await db.connect()
  const raw=Buffer.from((await readFile(new URL('../../../tests/fixtures/player_snapshot_v1_canonical.hex',import.meta.url),'utf8')).trim(),'hex')
  const {rows:[{payload:rawBank}]}=await db.query("select payload from private.game_character_bank_snapshot_v1_payloads where character_id='a9530000-0000-0000-0000-000000000001'")
  const player=playerGold(raw,100n), bank=bankGold(rawBank,50n)
  for(const [gold,balance,nextGold,nextBalance,direction,amount,valid] of [
    [100n,50n,75n,75n,'deposit',25,true],
    [100n,50n,125n,25n,'withdraw',25,true],
    [100n,299999999n,99n,300000000n,'deposit',1,true],
    [100n,300000000n,99n,300000001n,'deposit',1,false],
    [0n,50n,-1n,51n,'deposit',1,false],
    [100n,0n,101n,-1n,'withdraw',1,false],
    [9223372036854775807n,50n,-9223372036854775808n,49n,'withdraw',1,false],
    [100n,50n,100n,50n,'deposit',0,false],
    [100n,50n,75n,75n,null,25,false],
  ]) {
    const {rows}=await db.query('select private.money_transfer_pair_valid($1,$2,$3,$4,$5,$6) valid',[
      playerGold(player,gold),bankGold(bank,balance),playerGold(player,nextGold),bankGold(bank,nextBalance),direction,amount,
    ])
    assert.equal(rows[0].valid,valid)
  }
  await db.query(`insert into public.game_characters(id,world_id,legacy_name,legacy_name_key,legacy_shard,lifecycle,storage_format)
    values($1,$2,'Pvahero','Pvahero',substr(encode(public.digest(convert_to('Pvahero','UTF8'),'sha1'),'hex'),1,2),'imported_unclaimed',1)`,[id,world])
  await db.query('insert into private.game_character_paired_snapshot_states values($1,0,$2,$3)',[id,player,bank])
  const commit='select * from private.commit_money_transfer_candidate($1,$2,$3,$4,$5,$6,$7)'
  const qualifiedSql='select * from private.commit_qualified_money_transfer($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)'
  const authority=[world,actor,session,'test-gateway',writer,1]
  if(qualified) {
    assert.equal((await db.query('select has_function_privilege($1,$2,$3) allowed',['mud_writer',signature,'EXECUTE'])).rows[0].allowed,false)
    assert.equal((await db.query('select has_function_privilege($1,$2,$3) allowed',['mud_writer',readSignature,'EXECUTE'])).rows[0].allowed,false)
    await db.query('insert into auth.users(id) values($1)',[actor])
    await db.query("update public.game_characters set owner_user_id=$2,lifecycle='active',claimed_at=clock_timestamp() where id=$1",[id,actor])
    await db.query("insert into private.game_character_sessions(character_id,session_id,actor_user_id,gateway_instance_id,expires_at) values($1,$2,$3,'test-gateway',clock_timestamp()+interval '3 minutes')",[id,session,actor])
    await db.query("select * from private.acquire_game_world_writer_epoch($1,$2,clock_timestamp()+interval '3 minutes')",[world,writer])
    // Disposable integration grant only; explicitly revoked in finally.
    await db.query(`grant execute on function ${signature} to mud_writer`)
    await db.query(`grant execute on function ${readSignature} to mud_writer`)
    login=new Client({connectionString:`postgresql://mud_writer_login:bank-local-contract-password@127.0.0.1:${port}/postgres`})
    await login.connect(); await login.query('set role mud_writer')
    await assert.rejects(login.query('select * from private.game_character_paired_snapshot_states'),e=>e.code==='42501')
    await assert.rejects(db.query(readSql,[id,...authority]),e=>e.code==='P0001')
    const wrong=[id,...authority]; wrong[2]='e9210000-0000-0000-0000-000000000099'
    await assert.rejects(login.query(readSql,wrong),e=>e.code==='P0001')
    assert.ok(process.env.BANK_TRANSFER_NATIVE_READER?.startsWith('/'))
    const denied=nativeRead(wrong)
    assert.equal(denied.status,1); assert.equal(denied.stdout.length,0)
    await db.query('begin')
    try {
      await db.query('select character_id from private.game_character_paired_snapshot_states where character_id=$1 for update',[id])
      const start=Date.now()
      const timeout=nativeRead([id,...authority],'-c lock_timeout=0 -c statement_timeout=0')
      assert.equal(timeout.status,1,'native reader must terminate on its own deadline')
      assert.equal(timeout.stdout.length,0)
      assert.ok(Date.now()-start>=1500,'probe must wait for the native deadline, not an immediate error')
      assert.ok(Date.now()-start<4500,'native deadline must precede the process watchdog')
    } finally { await db.query('rollback') }
  }
  const execute=(args)=>qualified?login.query(qualifiedSql,[args[0],...authority,...args.slice(1)]):db.query(commit,args)
  const nativeCommit=(args,overrideAuthority=authority,options='')=>{
    const lengths=Buffer.alloc(8); lengths.writeUInt32BE(args[5].length); lengths.writeUInt32BE(args[6].length,4)
    return spawnSync(process.env.BANK_TRANSFER_NATIVE_COMMIT,[args[0],...overrideAuthority,...args.slice(1,5)].map(String),{
      input:Buffer.concat([lengths,args[5],args[6]]),timeout:5000,maxBuffer:1024,
      env:{...process.env,PGPORT:port,PGPASSWORD:'bank-local-contract-password',PGOPTIONS:options,
        ASAN_OPTIONS:'detect_leaks=1:halt_on_error=1',UBSAN_OPTIONS:'halt_on_error=1'},
    })
  }
  assert.equal((await db.query("select has_function_privilege('mud_writer','private.commit_money_transfer_candidate(uuid,uuid,bigint,text,bigint,bytea,bytea)','EXECUTE') allowed")).rows[0].allowed,false)
  for(const [index,direction] of ['deposit','withdraw'].entries()) {
    const before=qualified?(await login.query(readSql,[id,...authority])).rows[0]:await read()
    assert.deepEqual(before,await read(),'qualified writer reads the exact same pair and digests without table SELECT')
    let input=before
    if(qualified) {
      const native=nativeRead([id,...authority])
      assert.equal(native.status,0,native.stderr.toString())
      assert.equal(native.stderr.toString(),`${before.revision} ${before.player_hash} ${before.bank_hash}\n`)
      const pl=native.stdout.readUInt32BE(0),bl=native.stdout.readUInt32BE(4)
      assert.equal(native.stdout.length,8+pl+bl)
      input={...before,player_payload:native.stdout.subarray(8,8+pl),bank_payload:native.stdout.subarray(8+pl)}
      assert.deepEqual(input,before)
    }
    for(const negative of [planned(input,direction,25,true),planned(input,direction,999999)]) {
      assert.equal(negative.status,1); assert.equal(negative.stdout.length,0)
    }
    assert.deepEqual(await read(),before)
    const out=planned(input,direction,25)
    assert.equal(out.status,0,out.stderr.toString())
    const pl=out.stdout.readUInt32BE(0),bl=out.stdout.readUInt32BE(4)
    assert.equal(out.stdout.length,8+pl+bl)
    const p=out.stdout.subarray(8,8+pl),b=out.stdout.subarray(8+pl)
    assert.deepEqual(p,playerGold(player,direction==='deposit'?75n:100n))
    assert.deepEqual(b,bankGold(bank,direction==='deposit'?75n:50n))
    const args=[id,`c9190000-0000-0000-0000-00000000000${index+1}`,before.revision,direction,25,p,b]
    const nameBody=Buffer.from(p.subarray(16,-32)); nameBody[7]=81
    const renamed=rebody(p,nameBody)
    const bankBody=Buffer.from(b.subarray(16,-32)), inner=bankBody.subarray(7), innerBody=Buffer.from(inner.subarray(16,-32)); innerBody[30]=65
    rebody(inner,innerBody).copy(bankBody,7)
    const renamedBank=rebody(b,bankBody)
    for(const [badPlayer,badBank] of [[renamed,b],[p,renamedBank],[playerGold(p,777n),b]]) {
      await assert.rejects(execute([...args.slice(0,5),badPlayer,badBank]),e=>e.code==='22023')
      assert.deepEqual(await read(),before,'invalid transfer leaves both snapshots and revision unchanged')
    }
    if(index===0) {
      await db.query("create function pg_temp.reject_money_intent() returns trigger language plpgsql as $$ begin raise exception using errcode='P0001',message='injected intent failure'; end $$")
      await db.query('create trigger injected_money_failure before insert on private.game_character_money_transfer_intents for each row execute function pg_temp.reject_money_intent()')
      await assert.rejects(execute(args),e=>e.code==='P0001')
      assert.deepEqual(await read(),before)
      assert.equal((await db.query('select count(*)::int count from private.game_character_paired_snapshot_commands where character_id=$1',[id])).rows[0].count,0)
      await db.query('drop trigger injected_money_failure on private.game_character_money_transfer_intents')
    }
    if(qualified) {
      const wrong=[args[0],...authority,...args.slice(1)]; wrong[2]='e9210000-0000-0000-0000-000000000002'
      await assert.rejects(login.query(qualifiedSql,wrong),e=>e.code==='P0001')
      await assert.rejects(db.query(qualifiedSql,[args[0],...authority,...args.slice(1)]),e=>e.code==='P0001')
      if(index===0) {
        // The writer has already locked/read its live session when it waits
        // on the snapshot row. Eligibility must use time AFTER that wait.
        await login.query("set statement_timeout='10s'")
        // Override the role's short lock timeout only in this disposable
        // session, so expiry rather than lock timeout decides this case.
        await login.query("set lock_timeout='5s'")
        const pid=(await login.query('select pg_backend_pid() pid')).rows[0].pid
        for(const [leaseTable,keyColumn,key,label] of [
          ['game_character_sessions','character_id',id,'session'],
          ['game_character_writer_epochs','world_id',world,'writer lease'],
        ]) {
        await db.query(`update private.${leaseTable} set expires_at=clock_timestamp()+interval '3 seconds' where ${keyColumn}=$1`,[key])
        await db.query('begin')
        let waiting
        try {
          await db.query('select character_id from private.game_character_paired_snapshot_states where character_id=$1 for update',[id])
          waiting=execute(args).then(result=>({result}),error=>({error}))
          let blocked=false
          const deadline=Date.now()+2000
          while(Date.now()<deadline) {
            blocked=(await db.query('select pg_backend_pid()=any(pg_blocking_pids($1)) blocked',[pid])).rows[0].blocked
            if(blocked) break
            await new Promise(resolve=>setTimeout(resolve,20))
          }
          assert.equal(blocked,true,'writer must actually wait on the snapshot lock')
          assert.equal((await db.query(`select expires_at>clock_timestamp() live from private.${leaseTable} where ${keyColumn}=$1`,[key])).rows[0].live,true)
          await db.query(`select pg_sleep(greatest(0,extract(epoch from expires_at-clock_timestamp()))::double precision+0.05) from private.${leaseTable} where ${keyColumn}=$1`,[key])
        } finally {
          await db.query('rollback')
        }
        const waited=await waiting
        assert.equal(waited.error?.code,'P0001',`expired ${label} must not authorize the delayed commit`)
        assert.deepEqual(await read(),before)
        for(const table of ['game_character_paired_snapshot_commands','game_character_money_transfer_intents','game_character_money_transfer_authorities']) {
          assert.equal((await db.query(`select count(*)::int count from private.${table} where character_id=$1`,[id])).rows[0].count,0)
        }
        await db.query(`update private.${leaseTable} set expires_at=clock_timestamp()+interval '3 minutes' where ${keyColumn}=$1`,[key])
        console.log(`GREEN ${label} expiry during observed snapshot lock wait rejects commit without state or journal writes`)
        }
        for(const [renewal,parameters,label] of [
          ["select * from public.renew_game_character_session($1,$2,clock_timestamp()+interval '3 minutes')",[session,'test-gateway'],'session'],
          ["select * from private.renew_game_world_writer_epoch($1,$2,$3,clock_timestamp()+interval '3 minutes')",[world,writer,1],'writer lease'],
        ]) {
          await db.query('begin')
          await login.query('begin')
          try {
            await db.query(renewal,parameters)
            const waiting=execute(args).then(result=>({result}),error=>({error}))
            let blocked=false
            const deadline=Date.now()+2000
            while(Date.now()<deadline) {
              blocked=(await db.query('select pg_backend_pid()=any(pg_blocking_pids($1)) blocked',[pid])).rows[0].blocked
              if(blocked) break
              await new Promise(resolve=>setTimeout(resolve,20))
            }
            assert.equal(blocked,true,`${label} renewal must block the concurrent commit`)
            await db.query('commit')
            const waited=await waiting
            assert.equal(waited.error,undefined)
            assert.equal(waited.result.rows[0].outcome,'COMMITTED')
          } finally {
            await db.query('rollback')
            await login.query('rollback')
          }
          assert.deepEqual(await read(),before,'rolled-back probe must not advance either snapshot')
          for(const table of ['game_character_paired_snapshot_commands','game_character_money_transfer_intents','game_character_money_transfer_authorities']) {
            assert.equal((await db.query(`select count(*)::int count from private.${table} where character_id=$1`,[id])).rows[0].count,0)
          }
          console.log(`GREEN ${label} renewal serializes before qualified commit without deadlock`)
        }
      }
    }
    if(qualified) {
      assert.ok(process.env.BANK_TRANSFER_NATIVE_COMMIT?.startsWith('/'))
      const wrongAuthority=[...authority]; wrongAuthority[1]='e9210000-0000-0000-0000-000000000099'
      const rejected=nativeCommit(args,wrongAuthority)
      assert.equal(rejected.status,1); assert.equal(rejected.stdout.length,0)
      if(index===0) {
        await db.query('begin')
        try {
          await db.query('select character_id from private.game_character_paired_snapshot_states where character_id=$1 for update',[id])
          const start=Date.now()
          // Wrong actor guarantees this probe cannot commit after disconnect;
          // the transport still must report UNKNOWN while it lacks a reply.
          const unknown=nativeCommit(args,wrongAuthority,'-c lock_timeout=0 -c statement_timeout=0')
          assert.equal(unknown.status,3); assert.equal(unknown.stdout.length,0)
          assert.ok(Date.now()-start>=1500 && Date.now()-start<4500)
        } finally { await db.query('rollback') }
      }
      for(const expected of ['COMMITTED','EXACT_RETRY']) {
        const result=nativeCommit(args)
        assert.equal(result.status,0,result.stderr.toString())
        assert.equal(result.stdout.toString(),`${expected} ${index+1}\n`)
      }
    } else {
      assert.equal((await execute(args)).rows[0].outcome,'COMMITTED')
      assert.equal((await execute(args)).rows[0].outcome,'EXACT_RETRY')
    }
    await assert.rejects(execute([...args.slice(0,3),direction,26,p,b]),e=>e.code==='P0001')
    await assert.rejects(execute([...args.slice(0,3),direction==='deposit'?'withdraw':'deposit',25,p,b]),e=>e.code==='P0001')
    const after=await read(); assert.equal(after.revision,String(index+1)); assert.deepEqual(after.player_payload,p); assert.deepEqual(after.bank_payload,b)
  }
  const final=await read(); assert.deepEqual(final.player_payload,player); assert.deepEqual(final.bank_payload,bank)
  await assert.rejects(execute([id,'c9190000-0000-0000-0000-000000000003',0,'deposit',25,player,bank]),e=>e.code==='40001')
  assert.deepEqual(await read(),final)
  assert.equal((await db.query('select count(*)::int count from private.game_character_money_transfer_intents where character_id=$1',[id])).rows[0].count,2)
  if(qualified) {
    const rows=(await db.query('select actor_user_id,session_id,writer_instance_id,writer_epoch::text from private.game_character_money_transfer_authorities where character_id=$1',[id])).rows
    assert.equal(rows.length,2)
    for(const row of rows) assert.deepEqual(row,{actor_user_id:actor,session_id:session,writer_instance_id:writer,writer_epoch:'1'})
    console.log('GREEN qualified writer login: gated pair read -> Rust result -> atomic commit and immutable command binding')
  }
  console.log('GREEN DB snapshots -> digest-bound Rust deposit/withdraw -> atomic DB pair and exact retry; full-byte roundtrip preserved')
} finally {
  if(login) await login.end()
  if(qualified) {
    await db.query(`revoke execute on function ${signature} from mud_writer`)
    await db.query(`revoke execute on function ${readSignature} from mud_writer`)
    assert.equal((await db.query('select has_function_privilege($1,$2,$3) allowed',['mud_writer',signature,'EXECUTE'])).rows[0].allowed,false)
    assert.equal((await db.query('select has_function_privilege($1,$2,$3) allowed',['mud_writer',readSignature,'EXECUTE'])).rows[0].allowed,false)
  }
  await db.end()
}

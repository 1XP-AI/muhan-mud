import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { createHash } from 'node:crypto'
import { readFile,mkdtemp,rm,appendFile } from 'node:fs/promises'
import {tmpdir} from 'node:os'
import {join} from 'node:path'
import {prepareMoneyPending,readMoneyPending,claimMoneyCharacterFence} from '../dist/money-pending-request.js'
import {releaseConfirmedMoney} from '../dist/money-pending-release.js'
import {preparePlayerPending,readPlayerPending,claimPlayerCharacterFence} from '../dist/player-pending-request.js'
import {recoverPlayerPendingOnce} from '../dist/player-pending-recovery.js'
import {releaseConfirmedPlayer} from '../dist/player-pending-release.js'
import {fileURLToPath} from 'node:url'
import { spawn,spawnSync } from 'node:child_process'
import { loseCommittedAck } from './lost-money-ack.mjs'
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
  return spawnSync(process.env.BANK_TRANSFER_NATIVE_PLANNER??binary,[direction,String(amount),badDigest?'0'.repeat(64):state.player_hash,state.bank_hash],{
    input:Buffer.concat([lengths,state.player_payload,state.bank_payload]),maxBuffer:9*1024*1024,timeout:5000,
  })
}
const qualified=process.env.BANK_TRANSFER_QUALIFIED==='1'
const id=qualified?'a9210000-0000-0000-0000-000000000001':'a9190000-0000-0000-0000-000000000001'
const world=qualified?'qualified-rust-pair':'rust-pair'
const actor='e9210000-0000-0000-0000-000000000001',session='f9210000-0000-0000-0000-000000000001',writer='b9210000-0000-0000-0000-000000000001'
const signature='private.commit_qualified_money_transfer(uuid,text,uuid,uuid,text,uuid,bigint,uuid,bigint,text,bigint,bytea,bytea)'
const readSignature='private.read_qualified_money_transfer_state(uuid,text,uuid,uuid,text,uuid,bigint)'
const recoverySignature='private.reconcile_money_transfer(uuid,text,uuid,uuid,text,uuid,bigint,uuid,bigint,text,bigint,bytea,bytea,uuid,bigint)'
const playerRouteSignature='private.resolve_player_paired_route(text,text,uuid,bigint)'
const playerLoadSignature='private.read_player_paired_snapshot(text,text,uuid,bigint,uuid,bigint)'
const playerSaveSignature='private.commit_player_snapshot(text,text,uuid,bigint,uuid,uuid,bigint,text,bytea)'
const playerRecoverySignature='private.reconcile_player_snapshot(text,text,uuid,bigint,uuid,uuid,bigint,text,bytea,uuid,bigint)'
const playerRecoverySql='select * from private.reconcile_player_snapshot($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)'
const playerRouteSql='select * from private.resolve_player_paired_route($1,$2,$3,$4)'
const nativeRoute=values=>{
  assert.ok(process.env.PLAYER_PAIRED_ROUTE_NATIVE?.startsWith('/'))
  return spawnSync(process.env.PLAYER_PAIRED_ROUTE_NATIVE,values.map(String),{
    env:{...process.env,PGPORT:port,PGPASSWORD:'bank-local-contract-password',PGOPTIONS:'',
      ASAN_OPTIONS:'detect_leaks=1:halt_on_error=1',UBSAN_OPTIONS:'halt_on_error=1'},timeout:5000,maxBuffer:8192,
  })
}
const readSql='select * from private.read_qualified_money_transfer_state($1,$2,$3,$4,$5,$6,$7)'
const nativeRead=(args,options='')=>spawnSync(process.env.BANK_TRANSFER_NATIVE_READER,args.map(String),{
  env:{...process.env,PGPORT:port,PGPASSWORD:'bank-local-contract-password',PGOPTIONS:options,
    ASAN_OPTIONS:'detect_leaks=1:halt_on_error=1',UBSAN_OPTIONS:'halt_on_error=1'},
  timeout:5000,maxBuffer:9*1024*1024,
})
let login,playerRecoveryRequest
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
    assert.equal((await db.query('select has_function_privilege($1,$2,$3) allowed',['mud_writer',playerRouteSignature,'EXECUTE'])).rows[0].allowed,false)
    await db.query(`grant execute on function ${playerRouteSignature} to mud_writer`)
    assert.equal((await db.query('select has_function_privilege($1,$2,$3) allowed',['mud_writer',playerLoadSignature,'EXECUTE'])).rows[0].allowed,false)
    await db.query(`grant execute on function ${playerLoadSignature} to mud_writer`)
    assert.equal((await db.query('select has_function_privilege($1,$2,$3) allowed',['mud_writer',playerSaveSignature,'EXECUTE'])).rows[0].allowed,false)
    await db.query(`grant execute on function ${playerSaveSignature} to mud_writer`)
    assert.equal((await db.query('select has_function_privilege($1,$2,$3) allowed',['mud_writer',playerRecoverySignature,'EXECUTE'])).rows[0].allowed,false)
    await db.query(`grant execute on function ${playerRecoverySignature} to mud_writer`)
    {
      const saveId='a9260000-0000-0000-0000-000000000001',name='Savehero'
      const body=Buffer.from(player.subarray(16,-32));body.fill(0,7,87);body.write(name,7,'utf8')
      const initial=rebody(player,body),changed=playerGold(initial,101n)
      await db.query(`insert into public.game_characters(id,world_id,legacy_name,legacy_name_key,legacy_shard,lifecycle,storage_format,owner_user_id,claimed_at)
        values($1,$2,$3,$3,substr(encode(public.digest(convert_to($3,'UTF8'),'sha1'),'hex'),1,2),'active',1,$4,clock_timestamp())`,[saveId,world,name,actor])
      await db.query('insert into private.game_character_paired_snapshot_states values($1,0,$2,$3)',[saveId,initial,bank])
      const sql='select * from private.commit_player_snapshot($1,$2,$3,$4,$5,$6,$7,$8,$9)'
      const request=[world,name,writer,'1',saveId,'c9260000-0000-0000-0000-000000000001','0',sha(initial).toString('hex'),changed]
      playerRecoveryRequest=request
      const nativeSave=(args,expected,root)=>{
        assert.ok(process.env.PLAYER_SNAPSHOT_SAVE_NATIVE?.startsWith('/'))
        const extra=root?[process.execPath,fileURLToPath(new URL('../dist/player-pending-prepare-cli.js',import.meta.url)),root]:[]
        const result=spawnSync(process.env.PLAYER_SNAPSHOT_SAVE_NATIVE,[...args.slice(0,8).map(String),...extra],{
          input:args[8],env:{...process.env,PGPORT:port,PGPASSWORD:'bank-local-contract-password',PGOPTIONS:'',
            ASAN_OPTIONS:'detect_leaks=1:halt_on_error=1',UBSAN_OPTIONS:'halt_on_error=1'},timeout:5000,maxBuffer:8192,
        })
        assert.equal(result.status,0,result.stderr.toString());assert.equal(result.stdout.toString(),expected)
      }
      const state=async()=>(await db.query('select revision::text,player_payload,bank_payload from private.game_character_paired_snapshot_states where character_id=$1',[saveId])).rows[0]
      await assert.rejects(db.query(sql,request),e=>e.code==='P0001')
      // A well-formed payload/digest is not permission to save another identity.
      for (const [index,value] of [[1,'Missinghero'],[3,'2'],[4,id]]) {
        const mismatch=[...request];mismatch[index]=value
        await assert.rejects(login.query(sql,mismatch),e=>e.code==='P0001')
      }
      const renamedBody=Buffer.from(changed.subarray(16,-32))
      renamedBody.fill(0,7,87);renamedBody.write('Otherhero',7,'utf8')
      const renamed=[...request];renamed[8]=rebody(changed,renamedBody)
      await assert.rejects(login.query(sql,renamed),e=>e.code==='22023')
      assert.deepEqual(await state(),{revision:'0',player_payload:initial,bank_payload:bank})
      assert.equal((await db.query('select count(*)::int n from private.game_character_player_save_intents where character_id=$1',[saveId])).rows[0].n,0)
      const wrong=[...request];wrong[7]='0'.repeat(64)
      await assert.rejects(login.query(sql,wrong),e=>e.code==='40001')
      nativeSave(wrong,'-1 0\n')
      const invalidRevision=[...request];invalidRevision[6]='00'
      nativeSave(invalidRevision,'-2 0\n')
      invalidRevision[6]='9223372036854775806';nativeSave(invalidRevision,'-2 0\n')
      assert.equal((await state()).revision,'0')
      const pending=await mkdtemp(join(tmpdir(),'muhan-player-native-pending-'))
      try {
        nativeSave(request,'-3 0\n',join(pending,'missing'))
        const conflictRoot=await mkdtemp(join(pending,'conflict-'))
        const different=[...request.slice(0,8)];different[6]='1'
        await preparePlayerPending(conflictRoot,different,request[8])
        nativeSave(request,'-3 0\n',conflictRoot)
        assert.deepEqual(await state(),{revision:'0',player_payload:initial,bank_payload:bank})
        assert.equal((await db.query('select count(*)::int n from private.game_character_player_save_intents where character_id=$1',[saveId])).rows[0].n,0)
        await claimPlayerCharacterFence(pending,request.slice(0,8),request[8])
        await assert.rejects(releaseConfirmedPlayer(pending,request.slice(0,8),request[8],writer,'1',login),/unconfirmed/)
        assert.ok(process.env.PLAYER_SESSION_STORE_NATIVE?.startsWith('/'))
        await new Promise((resolve,reject)=>{
          const child=spawn(process.env.PLAYER_SESSION_STORE_NATIVE,
          [world,name,writer,'1',request[5],process.execPath,fileURLToPath(new URL('../dist/player-pending-prepare-cli.js',import.meta.url)),pending,'101'],{
            env:{...process.env,PGPORT:port,PGPASSWORD:'bank-local-contract-password',PGOPTIONS:'',
              ASAN_OPTIONS:'detect_leaks=1:halt_on_error=1',UBSAN_OPTIONS:'halt_on_error=1'},stdio:['pipe','pipe','pipe'],
          })
          let output='',errors='',releasing=false,failure
          const timer=setTimeout(()=>{failure=new Error('native adoption watchdog');child.kill('SIGKILL')},12000)
          child.stderr.on('data',b=>{errors=(errors+b).slice(-8192)})
          child.on('error',e=>{failure=e})
          child.stdout.on('data',b=>{
            output+=b
            if(output==='READY\n'&&!releasing) {
              releasing=true
              releaseConfirmedPlayer(pending,request.slice(0,8),request[8],writer,'1',login)
                .then(()=>child.stdin.end('R')).catch(e=>{failure=e;child.kill('SIGKILL')})
            }
          })
          child.on('close',code=>{clearTimeout(timer);if(failure||code!==0||!releasing) reject(failure??new Error(`native adoption ${code}: ${errors}`));else resolve()})
        })
        assert.deepEqual(await readPlayerPending(pending,request[5]),{args:request.slice(0,8),payload:request[8]})
        nativeSave(request,'2 1\n')
        assert.deepEqual(await readPlayerPending(pending,request[5]),{args:request.slice(0,8),payload:request[8]})
        assert.deepEqual(await readPlayerPending(pending,request[5]),{args:request.slice(0,8),payload:request[8]})
        // Retained resolved history no longer blocks a different operation's CLI.
        const bankArgs=[saveId,world,actor,session,'test-gateway',writer,'1','c9270000-0000-0000-0000-000000000001','1','deposit','1']
        const lengths=Buffer.alloc(8);lengths.writeUInt32BE(changed.length);lengths.writeUInt32BE(bank.length,4)
        const frame=Buffer.concat([lengths,changed,bank])
        const next=spawnSync(process.execPath,[fileURLToPath(new URL('../dist/money-pending-prepare-cli.js',import.meta.url)),'--prepare',pending,...bankArgs],{input:frame,timeout:5000})
        assert.equal(next.status,0,next.stderr.toString());assert.deepEqual(next.stdout,frame)
        await assert.rejects(releaseConfirmedPlayer(pending,request.slice(0,8),request[8],writer,'1',login))
      } finally {await rm(pending,{recursive:true,force:true})}
      assert.deepEqual((await login.query(sql,request)).rows,[{outcome:'EXACT_RETRY',committed_revision:'1'}])
      const after=await state();assert.deepEqual(after,{revision:'1',player_payload:changed,bank_payload:bank})
      await assert.rejects(login.query(sql,[...request.slice(0,8),playerGold(initial,102n)]),e=>e.code==='P0001')
      const stale=[...request];stale[5]='c9260000-0000-0000-0000-000000000002'
      await assert.rejects(login.query(sql,stale),e=>e.code==='40001')
      const bad=[...stale];bad[6]='1';bad[7]=sha(changed).toString('hex');bad[8]=Buffer.from(changed);bad[8][bad[8].length-1]^=1
      await assert.rejects(login.query(sql,bad),e=>e.code==='22023')
      nativeSave(bad,'-2 0\n')
      assert.deepEqual(await state(),after)
      const peer=new Client({connectionString:`postgresql://mud_writer_login:bank-local-contract-password@127.0.0.1:${port}/postgres`,connectionTimeoutMillis:2000,statement_timeout:3000})
      try {
        await peer.connect();await peer.query('set role mud_writer')
        const a=[...stale];a[6]='1';a[7]=sha(changed).toString('hex');a[8]=playerGold(changed,102n)
        const b=[...a];b[5]='c9260000-0000-0000-0000-000000000003';b[8]=playerGold(changed,103n)
        const outcomes=await Promise.allSettled([login.query(sql,a),peer.query(sql,b)])
        assert.equal(outcomes.filter(x=>x.status==='fulfilled').length,1)
        assert.equal(outcomes.find(x=>x.status==='rejected').reason.code,'40001')
        const latest=await state();assert.equal(latest.revision,'2');assert.deepEqual(latest.bank_payload,bank)
        assert.deepEqual((await login.query(sql,request)).rows,[{outcome:'EXACT_RETRY',committed_revision:'1'}])
        nativeSave(request,'2 1\n')
        assert.deepEqual(await state(),latest)
      } finally {await peer.end()}
      assert.equal((await db.query('select count(*)::int n from private.game_character_player_save_intents where character_id=$1',[saveId])).rows[0].n,2)
      const recovery=[...request,writer,'1'],stable=await state()
      assert.deepEqual((await login.query(playerRecoverySql,recovery)).rows,[{outcome:'CONFIRMED',committed_revision:'1'}])
      await assert.rejects(db.query(playerRecoverySql,recovery),e=>e.code==='P0001')
      const unknown=[...recovery];unknown[5]='c9270000-0000-0000-0000-000000000099'
      assert.deepEqual((await login.query(playerRecoverySql,unknown)).rows,[{outcome:'UNRESOLVED',committed_revision:null}])
      for(const [index,value] of [[3,'2'],[6,'1'],[7,'0'.repeat(64)],[8,playerGold(initial,999n)]]) {
        const mismatch=[...recovery];mismatch[index]=value
        await assert.rejects(login.query(playerRecoverySql,mismatch),e=>e.code==='P0001')
      }
      assert.deepEqual(await state(),stable)
      await assert.rejects(db.query('delete from private.game_character_player_save_intents where character_id=$1',[saveId]),e=>e.code==='P0001')
      console.log('GREEN general player snapshot save: bank preserved, immutable exact retry, invalid/stale rollback and one concurrent CAS winner')
    }
    const routeName=(await db.query('select legacy_name from public.game_characters where id=$1',[id])).rows[0].legacy_name
    const routed=(await login.query(playerRouteSql,[world,routeName,writer,1])).rows
    assert.equal(routed.length,1);assert.equal(routed[0].character_id,id);assert.equal(routed[0].owner_user_id,actor);assert.equal(routed[0].revision,'0')
    assert.equal(routed[0].player_hash,sha(player).toString('hex'));assert.equal(routed[0].bank_hash,sha(bank).toString('hex'))
    const nativeIdentity=nativeRoute([world,routeName,writer,1])
    assert.equal(nativeIdentity.status,0,nativeIdentity.stderr.toString())
    assert.equal(nativeIdentity.stdout.toString(),`${id} ${actor} 0 ${routed[0].player_hash} ${routed[0].bank_hash}\n`)
    const missingIdentity=nativeRoute([world,'Missinghero',writer,1])
    assert.equal(missingIdentity.status,1);assert.equal(missingIdentity.stdout.length,0)
    await assert.rejects(login.query(playerRouteSql,[world,'Missinghero',writer,1]),e=>e.code==='P0001')
    await assert.rejects(login.query(playerRouteSql,[world,routeName,writer,2]),e=>e.code==='P0001')
    await assert.rejects(db.query(playerRouteSql,[world,routeName,writer,1]),e=>e.code==='P0001')
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
      const routeStart=Date.now(),blockedRoute=nativeRoute([world,routeName,writer,1])
      assert.equal(blockedRoute.status,1);assert.equal(blockedRoute.stdout.length,0)
      assert.ok(Date.now()-routeStart<4000,'native route must stop before watchdog on held DB lock')
      const start=Date.now()
      const timeout=nativeRead([id,...authority],'-c lock_timeout=0 -c statement_timeout=0')
      assert.equal(timeout.status,1,'native reader must terminate on its own deadline')
      assert.equal(timeout.stdout.length,0)
      assert.ok(Date.now()-start>=1500,'probe must wait for the native deadline, not an immediate error')
      assert.ok(Date.now()-start<4500,'native deadline must precede the process watchdog')
    } finally { await db.query('rollback') }
  }
  const execute=(args)=>qualified?login.query(qualifiedSql,[args[0],...authority,...args.slice(1)]):db.query(commit,args)
  const nativeCommit=(args,overrideAuthority=authority,options='',pendingRoot='',coordinate=false,drift='')=>{
    const lengths=Buffer.alloc(8); lengths.writeUInt32BE(args[5].length); lengths.writeUInt32BE(args[6].length,4)
    return spawnSync(process.env.BANK_TRANSFER_NATIVE_COMMIT,[args[0],...overrideAuthority,...args.slice(1,5)].map(String),{
      input:Buffer.concat([lengths,args[5],args[6]]),timeout:5000,maxBuffer:1024,
      env:{...process.env,PGPORT:port,PGPASSWORD:'bank-local-contract-password',PGOPTIONS:options,
        BANK_TRANSFER_PENDING_ROOT:pendingRoot,BANK_TRANSFER_PENDING_NODE:process.execPath,
        BANK_TRANSFER_COORDINATE:coordinate?'1':'0',
        BANK_TRANSFER_LIVE_DRIFT:drift,
        BANK_TRANSFER_PENDING_CLI:new URL('../dist/money-pending-prepare-cli.js',import.meta.url).pathname,
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
      if(index===0) {
        const start=Date.now()
        const stalled=spawnSync(process.env.BANK_TRANSFER_NATIVE_PLANNER,[direction,'25',before.player_hash,before.bank_hash],{
          input:native.stdout,timeout:5000,maxBuffer:8192,
          env:{...process.env,BANK_TRANSFER_PLANNER:process.env.BANK_TRANSFER_STALLED_PLANNER},
        })
        assert.equal(stalled.status,1,stalled.stderr.toString()); assert.equal(stalled.stdout.length,0)
        assert.ok(Date.now()-start>=1500 && Date.now()-start<4500)
        assert.deepEqual(await read(),before)
        console.log('GREEN native Rust bridge terminates and reaps a stalled child before watchdog without output or DB mutation')
      }
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
      if(index===0) {
        const lengths=Buffer.alloc(8); lengths.writeUInt32BE(p.length); lengths.writeUInt32BE(b.length,4)
        const pendingRoot=await mkdtemp(join(tmpdir(),'muhan-money-pending-'))
        const pendingArgs=[id,...authority,...args.slice(1,5)].map(String)
        const pendingFrame=Buffer.concat([lengths,p,b])
        try {
        // No parent-side prepare: the real C sender must durably record its
        // request before committing, even when it never receives the DB reply.
        await assert.rejects(readMoneyPending(pendingRoot,args[1]),{code:'ENOENT'})
        await loseCommittedAck({port,binary:process.env.BANK_TRANSFER_NATIVE_COMMIT,
          pendingRoot,
          args:[id,...authority,...args.slice(1,5)],input:Buffer.concat([lengths,p,b]),
          committed:async()=>{
            const state=await read()
            return state.revision==='1' && state.player_payload.equals(p) && state.bank_payload.equals(b)
          },
        })
        assert.deepEqual(await readMoneyPending(pendingRoot,args[1]),{args:pendingArgs,frame:pendingFrame})
        assert.equal(await prepareMoneyPending(pendingRoot,pendingArgs,pendingFrame),'EXACT_RETRY')
        const changed=[...pendingArgs]; changed[10]='26'
        await assert.rejects(prepareMoneyPending(pendingRoot,changed,pendingFrame),/conflict/)
        const recover=()=>spawnSync(process.execPath,[new URL('./money-pending-replay.mjs',import.meta.url).pathname,pendingRoot,args[1]],{
          timeout:7000,maxBuffer:8192,
          env:{...process.env,PGPORT:port,PGPASSWORD:'bank-local-contract-password',PGOPTIONS:'',
            ASAN_OPTIONS:'detect_leaks=1:halt_on_error=1',UBSAN_OPTIONS:'halt_on_error=1'},
        })
        const recovered=recover()
        assert.equal(recovered.status,0,recovered.stderr.toString())
        assert.equal(recovered.stdout.toString(),'EXACT_RETRY 1\n')
        // A corrupted durable record must fail before it reaches the DB.
        await appendFile(join(pendingRoot,`${args[1]}.money-request`),'x')
        const corrupt=recover()
        assert.equal(corrupt.status,1); assert.equal(corrupt.stdout.length,0)
        } finally { await rm(pendingRoot,{recursive:true,force:true}) }
        assert.equal((await db.query('select count(*)::int count from private.game_character_money_transfer_intents where character_id=$1',[id])).rows[0].count,1)
        console.log('GREEN durable pending request survives sender exit; fresh process recovers exact retry and rejects corruption')
        console.log('GREEN commit durable while native acknowledgement is lost: UNKNOWN, then same-command reconnect retry')
      }
      const nativePending=await mkdtemp(join(tmpdir(),'muhan-native-pending-'))
      try {
        const unchanged=await read()
        const refused=nativeCommit(args,authority,'',join(nativePending,'missing'))
        assert.equal(refused.status,4,refused.stderr.toString()); assert.equal(refused.stdout.length,0)
        assert.deepEqual(await read(),unchanged)
        if(index===1) {
          for(const drift of ['gold','level']) {
            const rejected=nativeCommit(args,authority,'',nativePending,true,drift)
            assert.equal(rejected.status,4,rejected.stderr.toString()); assert.equal(rejected.stdout.length,0)
            assert.deepEqual(await read(),unchanged)
            await assert.rejects(readMoneyPending(nativePending,args[1]),{code:'ENOENT'})
          }
          const stale=[...args]; stale[2]=0
          const result=nativeCommit(stale,authority,'',nativePending,true)
          assert.equal(result.status,4,result.stderr.toString()); assert.equal(result.stdout.length,0)
          assert.deepEqual(await read(),unchanged)
          await assert.rejects(readMoneyPending(nativePending,args[1]),{code:'ENOENT'})
        }
        for(const expected of [index===0?'EXACT_RETRY':'COMMITTED','EXACT_RETRY']) {
          // The new withdrawal uses one C coordinator; its retry still uses
          // the immutable original request, never a newly computed plan.
          const result=nativeCommit(args,authority,'',nativePending,index===1&&expected==='COMMITTED')
          assert.equal(result.status,0,result.stderr.toString())
          assert.equal(result.stdout.toString(),`${expected} ${index+1}\n`)
        }
        const saved=await readMoneyPending(nativePending,args[1])
        assert.deepEqual(saved.args,[id,...authority,...args.slice(1,5)].map(String))
        const pl=saved.frame.readUInt32BE(0)
        assert.deepEqual(saved.frame.subarray(8,8+pl),p); assert.deepEqual(saved.frame.subarray(8+pl),b)
        const afterCommit=await read()
        const conflict=nativeCommit([...args.slice(0,4),26,p,b],authority,'',nativePending)
        assert.equal(conflict.status,4); assert.equal(conflict.stdout.length,0)
        assert.deepEqual(await read(),afterCommit)
        assert.deepEqual(await readMoneyPending(nativePending,args[1]),saved)
        console.log('GREEN native commit requires durable preparation; missing directory/conflict never reaches commit')
      } finally { await rm(nativePending,{recursive:true,force:true}) }
    } else {
      assert.equal((await execute(args)).rows[0].outcome,'COMMITTED')
      assert.equal((await execute(args)).rows[0].outcome,'EXACT_RETRY')
    }
    await assert.rejects(execute([...args.slice(0,3),direction,26,p,b]),e=>e.code==='P0001')
    await assert.rejects(execute([...args.slice(0,3),direction==='deposit'?'withdraw':'deposit',25,p,b]),e=>e.code==='P0001')
    const after=await read(); assert.equal(after.revision,String(index+1)); assert.deepEqual(after.player_payload,p); assert.deepEqual(after.bank_payload,b)
  }
  let final=await read(); assert.deepEqual(final.player_payload,player); assert.deepEqual(final.bank_payload,bank)
  await assert.rejects(execute([id,'c9190000-0000-0000-0000-000000000003',0,'deposit',25,player,bank]),e=>e.code==='40001')
  assert.deepEqual(await read(),final)
  assert.equal((await db.query('select count(*)::int count from private.game_character_money_transfer_intents where character_id=$1',[id])).rows[0].count,2)
  if(qualified) {
    const rows=(await db.query('select actor_user_id,session_id,writer_instance_id,writer_epoch::text from private.game_character_money_transfer_authorities where character_id=$1',[id])).rows
    assert.equal(rows.length,2)
    for(const row of rows) assert.deepEqual(row,{actor_user_id:actor,session_id:session,writer_instance_id:writer,writer_epoch:'1'})
    const amountRoot=await mkdtemp(join(tmpdir(),'muhan-bank-all-'))
    try {
      const depositAll=[id,'c9250000-0000-0000-0000-000000000001',2,'deposit','모두',playerGold(player,0n),bankGold(bank,150n)]
      const deposited=nativeCommit(depositAll,authority,'',amountRoot,true)
      assert.equal(deposited.status,0,deposited.stderr.toString()); assert.equal(deposited.stdout.toString(),'COMMITTED 3\n')
      const saved=await readMoneyPending(amountRoot,depositAll[1])
      assert.equal(saved.args[10],'100','persist concrete DB-derived amount, never all')
      const afterAll=await read()
      assert.deepEqual(afterAll.player_payload,depositAll[5]); assert.deepEqual(afterAll.bank_payload,depositAll[6])
      const retry=nativeCommit([...depositAll.slice(0,4),100,...depositAll.slice(5)],authority,'',amountRoot)
      assert.equal(retry.status,0,retry.stderr.toString()); assert.equal(retry.stdout.toString(),'EXACT_RETRY 3\n')
      const empty=[id,'c9250000-0000-0000-0000-000000000002',3,'deposit','모두',depositAll[5],depositAll[6]]
      const rejected=nativeCommit(empty,authority,'',amountRoot,true)
      assert.equal(rejected.status,4); assert.equal(rejected.stdout.length,0)
      await assert.rejects(readMoneyPending(amountRoot,empty[1]),{code:'ENOENT'})
      assert.deepEqual(await read(),afterAll)
      // Normal sequence must use verified release, not a fresh directory or
      // manual deletion. No reconcile grant escapes this disposable block.
      await db.query(`grant execute on function ${recoverySignature} to mud_writer`)
      try {
        await assert.rejects(releaseConfirmedMoney(amountRoot,saved.args,saved.frame,{query:async()=>({rows:[{outcome:'UNRESOLVED',committed_revision:null}]})}),/unconfirmed/)
        await releaseConfirmedMoney(amountRoot,saved.args,saved.frame,login)
        assert.deepEqual(await readMoneyPending(amountRoot,depositAll[1]),saved)
      } finally {await db.query(`revoke execute on function ${recoverySignature} from mud_writer`)}
      const withdraw=[id,'c9250000-0000-0000-0000-000000000003',3,'withdraw','000100냥',player,bank]
      const withdrawn=nativeCommit(withdraw,authority,'',amountRoot,true)
      assert.equal(withdrawn.status,0,withdrawn.stderr.toString()); assert.equal(withdrawn.stdout.toString(),'COMMITTED 4\n')
      assert.equal((await readMoneyPending(amountRoot,withdraw[1])).args[10],'100')
      final=await read(); assert.deepEqual(final.player_payload,player); assert.deepEqual(final.bank_payload,bank)
      console.log('GREEN native command amounts: DB-derived all, numeric durable intent, exact retry, empty-all rejection and Korean unit normalization')
    } finally {await rm(amountRoot,{recursive:true,force:true})}
    assert.equal((await db.query('select has_function_privilege($1,$2,$3) allowed',['mud_writer',recoverySignature,'EXECUTE'])).rows[0].allowed,false)
    await db.query(`grant execute on function ${recoverySignature} to mud_writer`)
    await db.query("update private.game_character_sessions set expires_at=clock_timestamp()-interval '1 millisecond' where character_id=$1",[id])
    await db.query('select private.seal_game_world_writer_epoch($1,$2,1)',[world,writer])
    await db.query("update private.game_character_writer_epochs set expires_at=clock_timestamp()-interval '1 millisecond' where world_id=$1",[world])
    const successor='b9240000-0000-0000-0000-000000000001'
    assert.equal((await db.query("select * from private.acquire_game_world_writer_epoch($1,$2,clock_timestamp()+interval '3 minutes')",[world,successor])).rows[0].writer_epoch,'2')
    {
      const old=[...playerRecoveryRequest,writer,'1'],current=[...playerRecoveryRequest,successor,'2']
      await assert.rejects(login.query(playerRecoverySql,old),e=>e.code==='P0001')
      assert.deepEqual((await login.query(playerRecoverySql,current)).rows,[{outcome:'CONFIRMED',committed_revision:'1'}])
      const playerState=async()=>(await db.query('select * from private.game_character_paired_snapshot_states where character_id=$1',[playerRecoveryRequest[4]])).rows
      const before=await playerState(),root=await mkdtemp(join(tmpdir(),'muhan-player-recovery-'))
      try {
        await claimPlayerCharacterFence(root,playerRecoveryRequest.slice(0,8),playerRecoveryRequest[8])
        // Historical confirmation is insufficient after the DB head advances.
        await assert.rejects(releaseConfirmedPlayer(root,playerRecoveryRequest.slice(0,8),playerRecoveryRequest[8],successor,'2',login),e=>e.code==='40001')
        const expected={confirmed:1,unresolved:0,invalid:0,errors:0,truncated:false}
        assert.deepEqual(await recoverPlayerPendingOnce(root,world,successor,'2',login),expected)
        // Fence-only crash evidence and its later request copy count once.
        await preparePlayerPending(root,playerRecoveryRequest.slice(0,8),playerRecoveryRequest[8])
        assert.deepEqual(await recoverPlayerPendingOnce(root,world,successor,'2',login),expected)
        await appendFile(join(root,`${playerRecoveryRequest[5]}.player-request`),'corrupt')
        assert.deepEqual(await recoverPlayerPendingOnce(root,world,successor,'2',login),{...expected,invalid:1})
        assert.deepEqual(await playerState(),before)
      } finally {await rm(root,{recursive:true,force:true})}
      console.log('GREEN player recovery: successor confirms old request, fence-only discovery deduplicates, corruption preserved, no state replay')
    }
    const offlineName=(await db.query('select legacy_name from public.game_characters where id=$1',[id])).rows[0].legacy_name
    await assert.rejects(login.query(playerRouteSql,[world,offlineName,writer,1]),e=>e.code==='P0001')
    const offlineRoute=(await login.query(playerRouteSql,[world,offlineName,successor,2])).rows
    assert.equal(offlineRoute.length,1);assert.equal(offlineRoute[0].character_id,id);assert.equal(offlineRoute[0].revision,'4')
    const nativeOffline=nativeRoute([world,offlineName,successor,2])
    assert.equal(nativeOffline.status,0,nativeOffline.stderr.toString())
    assert.equal(nativeOffline.stdout.toString(),`${id} ${actor} 4 ${offlineRoute[0].player_hash} ${offlineRoute[0].bank_hash}\n`)
    console.log('GREEN player route resolves DB identity/current revision without a live player session; superseded writer rejected')
    const original=[id,...authority,'c9190000-0000-0000-0000-000000000001',0,'deposit',25,playerGold(player,75n),bankGold(bank,75n)]
    await assert.rejects(login.query(qualifiedSql,original),e=>e.code==='P0001')
    const recoverySql='select * from private.reconcile_money_transfer($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)'
    const request=[...original,successor,2]
    assert.deepEqual((await login.query(recoverySql,request)).rows,[{outcome:'CONFIRMED',committed_revision:'1'}])
    for(const [index,value] of [[3,'f9240000-0000-0000-0000-000000000099'],[10,26],[14,1],[2,'e9240000-0000-0000-0000-000000000099']]) {
      const wrong=[...request]; wrong[index]=value
      await assert.rejects(login.query(recoverySql,wrong),e=>e.code==='P0001')
    }
    const missing=[...request]; missing[7]='c9240000-0000-0000-0000-000000000099'
    assert.deepEqual((await login.query(recoverySql,missing)).rows,[{outcome:'UNRESOLVED',committed_revision:null}])
    await assert.rejects(db.query(recoverySql,request),e=>e.code==='P0001')
    const restartRoot=await mkdtemp(join(tmpdir(),'muhan-money-restart-'))
    try {
      const header=Buffer.alloc(8); header.writeUInt32BE(original[11].length); header.writeUInt32BE(original[12].length,4)
      const frame=Buffer.concat([header,original[11],original[12]])
      const savedArgs=original.slice(0,11).map(String)
      await claimMoneyCharacterFence(restartRoot,savedArgs,frame)
      const fencePath=join(restartRoot,`${createHash('sha256').update(JSON.stringify([savedArgs[1],savedArgs[0]])).digest('hex')}.money-fence`)
      const fenceBefore=await readFile(fencePath)
      const restart=()=>spawnSync(process.execPath,[new URL('../dist/money-pending-recovery-cli.js',import.meta.url).pathname,'--once'],{
        timeout:10000,maxBuffer:8192,env:{...process.env,MONEY_PENDING_RECOVERY_ENABLED:'true',MONEY_PENDING_ROOT:restartRoot,
          MONEY_RECOVERY_WORLD:world,MONEY_RECOVERY_WRITER:successor,MONEY_RECOVERY_EPOCH:'2',
          MONEY_RECOVERY_DATABASE_URL:`postgresql://mud_writer_login:bank-local-contract-password@127.0.0.1:${port}/postgres`},
      })
      const first=restart()
      assert.equal(first.status,0,first.stderr.toString())
      assert.deepEqual(JSON.parse(first.stdout.toString()),{confirmed:1,unresolved:0,invalid:0,errors:0,truncated:false})
      await prepareMoneyPending(restartRoot,savedArgs,frame)
      const unknown=[...savedArgs]; unknown[7]='c9240000-0000-0000-0000-000000000088'
      await prepareMoneyPending(restartRoot,unknown,frame)
      const corrupt=[...savedArgs]; corrupt[7]='c9240000-0000-0000-0000-000000000089'
      await prepareMoneyPending(restartRoot,corrupt,frame)
      await appendFile(join(restartRoot,`${corrupt[7]}.money-request`),'x')
      const beforeFiles=await Promise.all([savedArgs[7],unknown[7],corrupt[7]].map(command=>readFile(join(restartRoot,`${command}.money-request`))))
      for(let attempt=0;attempt<2;attempt++) {
        const result=restart()
        assert.equal(result.status,1,result.stderr.toString())
        assert.deepEqual(JSON.parse(result.stdout.toString()),{confirmed:1,unresolved:1,invalid:1,errors:0,truncated:false})
      }
      assert.deepEqual(await Promise.all([savedArgs[7],unknown[7],corrupt[7]].map(command=>readFile(join(restartRoot,`${command}.money-request`)))),beforeFiles)
      assert.deepEqual(await readFile(fencePath),fenceBefore)
      console.log('GREEN restarted recovery CLI discovers durable requests: confirms old commit, preserves unknown/corrupt files without writes')
    } finally { await rm(restartRoot,{recursive:true,force:true}) }
    assert.deepEqual(await read(),final)
    assert.equal((await db.query('select count(*)::int count from private.game_character_money_transfer_intents where character_id=$1',[id])).rows[0].count,4)
    console.log('GREEN successor writer reconciles exact old commit after session/epoch expiry without replay or state changes')
    console.log('GREEN qualified writer login: gated pair read -> Rust result -> atomic commit and immutable command binding')
  }
  console.log('GREEN DB snapshots -> digest-bound Rust deposit/withdraw -> atomic DB pair and exact retry; full-byte roundtrip preserved')
} finally {
  if(login) await login.end()
  if(qualified) {
    await db.query(`revoke execute on function ${signature} from mud_writer`)
    await db.query(`revoke execute on function ${readSignature} from mud_writer`)
    await db.query(`revoke execute on function ${recoverySignature} from mud_writer`)
    await db.query(`revoke execute on function ${playerRouteSignature} from mud_writer`)
    await db.query(`revoke execute on function ${playerLoadSignature} from mud_writer`)
    await db.query(`revoke execute on function ${playerSaveSignature} from mud_writer`)
    await db.query(`revoke execute on function ${playerRecoverySignature} from mud_writer`)
    assert.equal((await db.query('select has_function_privilege($1,$2,$3) allowed',['mud_writer',signature,'EXECUTE'])).rows[0].allowed,false)
    assert.equal((await db.query('select has_function_privilege($1,$2,$3) allowed',['mud_writer',readSignature,'EXECUTE'])).rows[0].allowed,false)
    assert.equal((await db.query('select has_function_privilege($1,$2,$3) allowed',['mud_writer',recoverySignature,'EXECUTE'])).rows[0].allowed,false)
    assert.equal((await db.query('select has_function_privilege($1,$2,$3) allowed',['mud_writer',playerRouteSignature,'EXECUTE'])).rows[0].allowed,false)
    assert.equal((await db.query('select has_function_privilege($1,$2,$3) allowed',['mud_writer',playerLoadSignature,'EXECUTE'])).rows[0].allowed,false)
    assert.equal((await db.query('select has_function_privilege($1,$2,$3) allowed',['mud_writer',playerSaveSignature,'EXECUTE'])).rows[0].allowed,false)
    assert.equal((await db.query('select has_function_privilege($1,$2,$3) allowed',['mud_writer',playerRecoverySignature,'EXECUTE'])).rows[0].allowed,false)
  }
  await db.end()
}

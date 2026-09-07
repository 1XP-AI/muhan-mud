import assert from 'node:assert/strict'
import {mkdtemp,rm,readdir,readFile} from 'node:fs/promises'
import {tmpdir} from 'node:os'
import {join} from 'node:path'
import {spawn} from 'node:child_process'
import {fileURLToPath} from 'node:url'
import {claimMoneyCharacterFence,prepareMoneyPending,visitMoneyPending,resolveMoneyCharacterFence} from '../dist/money-pending-request.js'
import {recoverMoneyPendingOnce} from '../dist/money-pending-recovery.js'
const args=['a0000000-0000-4000-8000-000000000001','fence-world','a0000000-0000-4000-8000-000000000002',
 'a0000000-0000-4000-8000-000000000003','gateway','a0000000-0000-4000-8000-000000000004','1',
 'a0000000-0000-4000-8000-000000000005','0','deposit','25']
const frame=Buffer.alloc(8+48+55);frame.writeUInt32BE(48,0);frame.writeUInt32BE(55,4)
if(process.argv[2]==='--hold') {
 const request=[...args];request[7]=process.argv[4]
 await resolveMoneyCharacterFence(process.argv[3],request,frame,async()=>{
  console.log('HELD');setInterval(()=>{},1000);await new Promise(()=>{})
 })
} else if(process.argv[2]==='--child') {
 const request=[...args];request[7]=process.argv[4];request[0]=process.argv[5]||args[0]
 try {console.log(await claimMoneyCharacterFence(process.argv[3],request,frame))} catch {process.exitCode=2}
} else {
 const root=await mkdtemp(join(tmpdir(),'muhan-money-fence-'))
 const child=(command,character=args[0])=>new Promise((resolve,reject)=>{
  const p=spawn(process.execPath,[fileURLToPath(import.meta.url),'--child',root,command,character],{stdio:['ignore','pipe','inherit']})
  let output='';p.stdout.on('data',b=>{output+=b});p.on('error',reject);p.on('exit',code=>resolve({code,output}))
 })
 const prepare=command=>new Promise((resolve,reject)=>{
  const request=[...args];request[7]=command
  const p=spawn(process.execPath,[fileURLToPath(new URL('../dist/money-pending-prepare-cli.js',import.meta.url)),
   '--prepare',root,...request],{stdio:['pipe','pipe','inherit']})
  const chunks=[];p.stdout.on('data',b=>chunks.push(b));p.on('error',reject)
  p.on('close',code=>resolve({code,output:Buffer.concat(chunks)}));p.stdin.end(frame)
 })
 try {
  const commands=Array.from({length:8},(_,i)=>`b0000000-0000-4000-8000-${String(i+1).padStart(12,'0')}`)
  const results=await Promise.all(commands.map(c=>child(c)))
  assert.equal(results.filter(r=>r.code===0).length,1)
  const winner=results.findIndex(r=>r.code===0)
  const files=await readdir(root);assert.equal(files.length,1);assert.ok(files[0].endsWith('.money-fence'))
  const saved=await readFile(join(root,files[0]))
  assert.deepEqual(await child(commands[winner]),{code:0,output:'EXACT_RETRY\n'})
  assert.equal((await child(commands[(winner+1)%8])).code,2)
  assert.deepEqual(await readFile(join(root,files[0])),saved)
  const refused=await prepare(commands[(winner+1)%8])
  assert.equal(refused.code,1,'C preparation helper must enforce character reservation')
  assert.equal(refused.output.length,0)
  await assert.rejects(readFile(join(root,`${commands[(winner+1)%8]}.money-request`)),{code:'ENOENT'})
  const prepared=await prepare(commands[winner]);assert.equal(prepared.code,0);assert.deepEqual(prepared.output,frame)
  assert.equal((await child('d0000000-0000-4000-8000-000000000001','c0000000-0000-4000-8000-000000000001')).code,0)
  const requests=[]
  assert.equal(await visitMoneyPending(root,async r=>{requests.push(r)}),false)
  assert.equal(requests.length,2,'fence-only crash state must be discovered')
  assert.ok(requests.every(Boolean))
  for(const r of requests) await prepareMoneyPending(root,r.args,r.frame)
  const deduplicated=[]
  await visitMoneyPending(root,async r=>{deduplicated.push(r)})
  assert.equal(deduplicated.length,2,'fence plus identical request is one recovery operation')
  let queries=0
  const report=await recoverMoneyPendingOnce(root,args[1],args[5],'2',{query:async(sql,values)=>{
   assert.ok(sql.includes('private.reconcile_money_transfer'));assert.equal(values.length,15);queries++
   return {rows:[{outcome:'UNRESOLVED',committed_revision:null}]}
  }})
  assert.deepEqual(report,{confirmed:0,unresolved:2,invalid:0,errors:0,truncated:false})
  assert.equal(queries,2)
  assert.deepEqual(await readFile(join(root,files[0])),saved)
  console.log('GREEN durable character fence: one process wins, restart retries exactly, other command blocked, other character independent')
  const winning=requests.find(r=>r.args[0]===args[0])
  await assert.rejects(resolveMoneyCharacterFence(root,winning.args,frame,async()=>{throw new Error('unconfirmed')}),/unconfirmed/)
  assert.deepEqual(await readFile(join(root,files[0])),saved)
  let begin,finish
  const entered=new Promise(r=>{begin=r}),gate=new Promise(r=>{finish=r})
  const releasing=resolveMoneyCharacterFence(root,winning.args,frame,async()=>{begin();await gate})
  await entered
  assert.equal((await child(commands[(winner+1)%8])).code,2,'claim must not race release')
  finish();await releasing
  assert.deepEqual(await readFile(join(root,`${winning.args[7]}.money-request`)),saved)
  assert.equal((await child(commands[winner])).code,2,'resolved command cannot reacquire')
  assert.equal((await child(commands[(winner+1)%8])).code,0)
  await assert.rejects(resolveMoneyCharacterFence(root,winning.args,frame,async()=>{}),/different active/)
  const holder=spawn(process.execPath,[fileURLToPath(import.meta.url),'--hold',root,commands[(winner+1)%8]],{stdio:['ignore','pipe','inherit']})
  try {
   await new Promise((resolve,reject)=>{
    const timer=setTimeout(()=>reject(new Error('lock holder timeout')),5000)
    holder.stdout.once('data',b=>{clearTimeout(timer);assert.ok(b.toString().includes('HELD'));resolve()})
    holder.once('error',e=>{clearTimeout(timer);reject(e)})
   })
   assert.equal((await child(commands[(winner+1)%8])).code,2)
   const stopped=new Promise(resolve=>holder.once('exit',resolve));holder.kill('SIGKILL');await stopped
   assert.equal((await child(commands[(winner+1)%8])).code,0,'process death must release lock without deleting reservation')
  } finally {holder.kill('SIGKILL')}
  console.log('GREEN serialized release preserves history, rejects unconfirmed release and cannot remove a newer reservation')
 } finally {await rm(root,{recursive:true,force:true})}
}

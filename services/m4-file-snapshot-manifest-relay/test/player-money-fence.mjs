import assert from 'node:assert/strict'
import {mkdtemp,rm,readdir,readFile} from 'node:fs/promises'
import {join} from 'node:path'
import {tmpdir} from 'node:os'
import {spawn} from 'node:child_process'
import {fileURLToPath} from 'node:url'
import {claimPlayerCharacterFence} from '../dist/player-pending-request.js'
import {claimMoneyCharacterFence} from '../dist/money-pending-request.js'
const character='a0000000-0000-4000-8000-000000000001',writer='a0000000-0000-4000-8000-000000000002'
const payload=Buffer.alloc(48),frame=Buffer.alloc(8+48+55);frame.writeUInt32BE(48);frame.writeUInt32BE(55,4)
function args(kind,command,char=character) {
 return kind==='player'?['cross-world','Savehero',writer,'1',char,command,'0','a'.repeat(64)]:
  [char,'cross-world',writer,writer,'gateway',writer,'1',command,'0','deposit','1']
}
if(process.argv[2]==='--child') {
 const [, , ,root,kind,command,char]=process.argv
 try {console.log(await (kind==='player'?claimPlayerCharacterFence(root,args(kind,command,char),payload):claimMoneyCharacterFence(root,args(kind,command,char),frame)))}
 catch {process.exitCode=2}
} else {
 const root=await mkdtemp(join(tmpdir(),'muhan-cross-fence-'))
 const child=(kind,command,char=character)=>new Promise((resolve,reject)=>{
  const p=spawn(process.execPath,[fileURLToPath(import.meta.url),'--child',root,kind,command,char],{stdio:['ignore','pipe','inherit']})
  let output='';p.stdout.on('data',b=>{output+=b});p.on('error',reject);p.on('close',code=>resolve({code,output}))
 })
 try {
  const commands=Array.from({length:8},(_,i)=>`b0000000-0000-4000-8000-${String(i+1).padStart(12,'0')}`)
  const results=await Promise.all(commands.map((command,i)=>child(i%2?'player':'money',command)))
  assert.equal(results.filter(r=>r.code===0).length,1)
  const winner=results.findIndex(r=>r.code===0),kind=winner%2?'player':'money'
  const files=(await readdir(root)).filter(f=>f.endsWith('-fence'));assert.equal(files.length,1)
  const before=await readFile(join(root,files[0]))
  const retry=await child(kind,commands[winner]);assert.equal(retry.code,0);assert.match(retry.output,/EXACT_RETRY/)
  const opposite=await child(kind==='player'?'money':'player','c0000000-0000-4000-8000-000000000001');assert.equal(opposite.code,2)
  assert.deepEqual(await readFile(join(root,files[0])),before)
  // Check both directions deterministically on separate characters.
  for(const first of ['player','money']) {
   const char=first==='player'?'d0000000-0000-4000-8000-000000000001':'d0000000-0000-4000-8000-000000000002'
   assert.equal((await child(first,commands[0],char)).code,0)
   assert.equal((await child(first==='player'?'money':'player',commands[1],char)).code,2)
  }
  console.log('GREEN shared character lock: mixed-process single winner, restart exact retry, both cross-operation directions blocked')
 } finally {await rm(root,{recursive:true,force:true})}
}

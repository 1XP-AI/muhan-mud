import assert from 'node:assert/strict'
import {mkdtemp,rm,readFile} from 'node:fs/promises'
import {join} from 'node:path'
import {tmpdir} from 'node:os'
import {claimPlayerCharacterFence,resolvePlayerCharacterFence,visitPlayerPending} from '../dist/player-pending-request.js'
const args=['release-world','Savehero','a0000000-0000-4000-8000-000000000001','1',
 'a0000000-0000-4000-8000-000000000002','a0000000-0000-4000-8000-000000000003','0','a'.repeat(64)]
const payload=Buffer.alloc(48),root=await mkdtemp(join(tmpdir(),'muhan-player-release-'))
try {
 await claimPlayerCharacterFence(root,args,payload)
 await assert.rejects(resolvePlayerCharacterFence(root,args,payload,async()=>{throw new Error('unconfirmed')}),/unconfirmed/)
 assert.equal(await claimPlayerCharacterFence(root,args,payload),'EXACT_RETRY')
 let entered,release
 const started=new Promise(r=>{entered=r}),gate=new Promise(r=>{release=r})
 const resolving=resolvePlayerCharacterFence(root,args,payload,async()=>{entered();await gate})
 await started
 const next=[...args];next[5]='b0000000-0000-4000-8000-000000000003';next[6]='1'
 await assert.rejects(claimPlayerCharacterFence(root,next,payload))
 release();await resolving
 assert.deepEqual(await readFile(join(root,`${args[5]}.player-request`)),await readFile(join(root,`${args[5]}.player-resolved`)))
 const pending=[];await visitPlayerPending(root,async r=>pending.push(r),1000,true);assert.equal(pending.length,0)
 await assert.rejects(claimPlayerCharacterFence(root,args,payload))
 assert.equal(await claimPlayerCharacterFence(root,next,payload),'CLAIMED')
 await assert.rejects(resolvePlayerCharacterFence(root,args,payload,async()=>{}))
 assert.equal(await claimPlayerCharacterFence(root,next,payload),'EXACT_RETRY')
 console.log('GREEN player release: confirmation required, lock serializes next claim, history retained, stale release cannot remove successor')
} finally {await rm(root,{recursive:true,force:true})}

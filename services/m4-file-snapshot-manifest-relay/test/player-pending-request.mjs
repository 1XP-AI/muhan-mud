import assert from 'node:assert/strict'
import {mkdtemp,rm,readFile,appendFile} from 'node:fs/promises'
import {join} from 'node:path'
import {tmpdir} from 'node:os'
import {spawnSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'
import {preparePlayerPending,readPlayerPending} from '../dist/player-pending-request.js'
const args=['test-world','Savehero','a0000000-0000-4000-8000-000000000001','1',
 'a0000000-0000-4000-8000-000000000002','a0000000-0000-4000-8000-000000000003','0','a'.repeat(64)]
const payload=Buffer.from((await readFile(new URL('../../../tests/fixtures/player_snapshot_v1_canonical.hex',import.meta.url),'utf8')).trim(),'hex')
const root=await mkdtemp(join(tmpdir(),'muhan-player-pending-'))
try {
 assert.equal(await preparePlayerPending(root,args,payload),'PREPARED')
 const before=await readFile(join(root,`${args[5]}.player-request`))
 const cli=fileURLToPath(new URL('../dist/player-pending-prepare-cli.js',import.meta.url))
 const retry=spawnSync(process.execPath,[cli,'--prepare',root,...args],{input:payload,timeout:5000})
 assert.equal(retry.status,0,retry.stderr.toString());assert.deepEqual(retry.stdout,payload)
 assert.deepEqual(await readPlayerPending(root,args[5]),{args,payload})
 assert.deepEqual(await readFile(join(root,`${args[5]}.player-request`)),before)
 const conflict=[...args];conflict[6]='1'
 await assert.rejects(preparePlayerPending(root,conflict,payload))
 const race=[...args];race[5]='b0000000-0000-4000-8000-000000000003'
 const other=[...race];other[6]='1'
 const results=await Promise.allSettled([preparePlayerPending(root,race,payload),preparePlayerPending(root,other,payload)])
 assert.equal(results.filter(x=>x.status==='fulfilled').length,1)
 const loaded=await readPlayerPending(root,race[5]);assert.ok(['0','1'].includes(loaded.args[6]))
 const invalid=[...args];invalid[6]='00';await assert.rejects(preparePlayerPending(root,invalid,payload))
 await appendFile(join(root,`${args[5]}.player-request`),'corrupt')
 await assert.rejects(readPlayerPending(root,args[5]))
 const corrupt=spawnSync(process.execPath,[cli,'--prepare',root,...args],{input:payload,timeout:5000})
 assert.notEqual(corrupt.status,0);assert.equal(corrupt.stdout.length,0)
 console.log('GREEN player pending: immutable baseline, fresh-process retry, conflicting publication and corruption refusal')
} finally {await rm(root,{recursive:true,force:true})}

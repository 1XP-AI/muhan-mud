import {createHash} from 'node:crypto'
import {opendir} from 'node:fs/promises'
import {pendingDirectory,readPendingBytes,publishPendingBytes,publishPendingBytesAt,characterPendingLock,requirePendingAbsent} from './pending-record-store.js'
const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const hash=(body:string)=>createHash('sha256').update(body).digest('hex')
function encode(args:string[],payload:Buffer):Buffer {
  if(!Array.isArray(args)||args.length!==8||args.some(s=>typeof s!=='string'||!s.length||s.length>128||/[\x00-\x20\x7f]/.test(s))) throw new Error('invalid player arguments')
  if(!/^[-a-zA-Z0-9_.]{1,64}$/.test(args[0])||Buffer.byteLength(args[1])>14||[2,4,5].some(i=>!uuid.test(args[i]))) throw new Error('invalid player identity')
  for(const i of [3,6]) {
    if(!/^(0|[1-9][0-9]{0,18})$/.test(args[i])||BigInt(args[i])>(i===6?9223372036854775805n:9223372036854775807n)||(i===3&&args[i]==='0')) throw new Error('invalid player revision')
  }
  if(!/^[0-9a-f]{64}$/.test(args[7])||payload.length<48||payload.length>4194304) throw new Error('invalid player payload')
  // Transport digest only; native codec and SQL validate gameplay bytes.
  const body=JSON.stringify({version:1,kind:'player-save',args,payload:payload.toString('base64')})
  return Buffer.from(JSON.stringify({body,sha256:hash(body)}))
}
export async function preparePlayerPending(root:string,args:string[],payload:Buffer) {
  const bytes=encode(args,payload)
  return publishPendingBytes(root,`${args[5]}.player-request`,bytes)
}
// Durable reservation shares money's lock and world/character key. Publishing
// it before the request means a crash cannot admit a different operation.
// No release here: preserve until independent DB reconciliation is implemented.
export async function claimPlayerCharacterFence(root:string,args:string[],payload:Buffer):Promise<'CLAIMED'|'EXACT_RETRY'> {
  const bytes=encode(args,payload),key=hash(JSON.stringify([args[0],args[4]]))
  const dir=await pendingDirectory(root),base=`/proc/self/fd/${dir.fd}`
  let lock:Awaited<ReturnType<typeof characterPendingLock>>|undefined
  try {
    lock=await characterPendingLock(base,key);await dir.sync()
    await requirePendingAbsent(base,`${key}.money-fence`)
    const outcome=await publishPendingBytesAt(dir,`${key}.player-fence`,bytes)
    return outcome==='PREPARED'?'CLAIMED':'EXACT_RETRY'
  } finally {try {await lock?.close()} finally {await dir.close()}}
}
function decode(bytes:Buffer,command?:string):{args:string[],payload:Buffer} {
  const envelope=JSON.parse(bytes.toString('utf8'))
  if(typeof envelope.body!=='string'||hash(envelope.body)!==envelope.sha256) throw new Error('player digest mismatch')
  const value=JSON.parse(envelope.body)
  if(value.version!==1||value.kind!=='player-save'||typeof value.payload!=='string') throw new Error('invalid player record')
  const payload=Buffer.from(value.payload,'base64'),args=value.args
  if(!encode(args,payload).equals(bytes)||(command!==undefined&&args[5]!==command)) throw new Error('noncanonical player record')
  return {args,payload}
}
export async function readPlayerPending(root:string,command:string):Promise<{args:string[],payload:Buffer}> {
  if(!uuid.test(command)) throw new Error('invalid player command')
  const dir=await pendingDirectory(root)
  try {
    const bytes=await readPendingBytes(`/proc/self/fd/${dir.fd}`,`${command}.player-request`)
    return decode(bytes,command)
  } finally {await dir.close()}
}
export async function visitPlayerPending(root:string,visit:(request:{args:string[],payload:Buffer}|null)=>Promise<void>,limit=1000):Promise<boolean> {
  if(!Number.isInteger(limit)||limit<1||limit>1000) throw new Error('invalid player scan limit')
  const dir=await pendingDirectory(root),base=`/proc/self/fd/${dir.fd}`
  try {
    const entries=await opendir(base),seen=new Map<string,string>();let scanned=0
    for await(const entry of entries) {
      if(++scanned>limit) return true
      const fence=entry.name.endsWith('.player-fence')
      if(!fence&&!entry.name.endsWith('.player-request')) continue
      const key=entry.name.slice(0,-(fence?'.player-fence':'.player-request').length)
      let request=null
      try {
        if(fence?!/^[0-9a-f]{64}$/.test(key):!uuid.test(key)) throw new Error('invalid player filename')
        const bytes=await readPendingBytes(base,entry.name),found=decode(bytes,fence?undefined:key)
        if(fence&&key!==hash(JSON.stringify([found.args[0],found.args[4]]))) throw new Error('invalid player fence key')
        const digest=createHash('sha256').update(bytes).digest('hex'),command=found.args[5]
        if(seen.has(command)) {
          if(seen.get(command)===digest) continue
          throw new Error('conflicting player copies')
        }
        seen.set(command,digest);request=found
      } catch { /* preserve corrupt and conflicting recovery evidence */ }
      await visit(request)
    }
    return false
  } finally {await dir.close()}
}

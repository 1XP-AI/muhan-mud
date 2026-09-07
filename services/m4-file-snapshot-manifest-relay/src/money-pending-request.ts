// Durable transport request, not a second gameplay database. Retain until an
// independently confirmed commit/retry is reconciled; never rewrite on retry.
import {constants} from 'node:fs'
import {open,link,unlink,opendir} from 'node:fs/promises'
import {isAbsolute} from 'node:path'
import {createHash,randomUUID} from 'node:crypto'
const MAX=12*1024*1024
const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const hash=(body:string)=>createHash('sha256').update(body).digest('hex')
function validate(args:string[],frame:Buffer):void {
  if(!Array.isArray(args)||args.length!==11||args.some(s=>typeof s!=='string'||s.length<1||s.length>128||/[\x00-\x20\x7f]/.test(s))) throw new Error('invalid pending arguments')
  if([0,2,3,5,7].some(i=>!uuid.test(args[i]))||!/^[-a-zA-Z0-9_.]{1,64}$/.test(args[1])) throw new Error('invalid pending identity')
  for(const i of [6,8,10]) {
    if(!/^(0|[1-9][0-9]{0,18})$/.test(args[i])||BigInt(args[i])>9223372036854775807n||(i!==8&&args[i]==='0')) throw new Error('invalid pending number')
  }
  if(BigInt(args[8])>9223372036854775805n||!['deposit','withdraw'].includes(args[9])||frame.length<8) throw new Error('invalid pending transfer')
  const p=frame.readUInt32BE(0),b=frame.readUInt32BE(4)
  if(p<48||b<55||p>4194304||b>4194304||frame.length!==8+p+b) throw new Error('invalid pending frame')
}
function encode(args:string[],frame:Buffer):Buffer {
  validate(args,frame)
  const body=JSON.stringify({version:1,args,frame:frame.toString('base64')})
  return Buffer.from(JSON.stringify({body,sha256:hash(body)}))
}
function decode(bytes:Buffer,command:string):{args:string[],frame:Buffer} {
  const envelope=JSON.parse(bytes.toString('utf8'))
  if(typeof envelope.body!=='string'||hash(envelope.body)!==envelope.sha256) throw new Error('pending digest mismatch')
  const value=JSON.parse(envelope.body)
  if(value.version!==1||typeof value.frame!=='string') throw new Error('invalid pending record')
  const frame=Buffer.from(value.frame,'base64')
  validate(value.args,frame)
  if(value.args[7]!==command||!encode(value.args,frame).equals(bytes)) throw new Error('noncanonical pending record')
  return {args:value.args,frame}
}
async function directory(root:string) {
  if(process.platform!=='linux'||!isAbsolute(root)) throw new Error('pending store requires absolute Linux directory')
  const fd=await open(root,constants.O_RDONLY|constants.O_DIRECTORY|constants.O_NOFOLLOW)
  try {
    const stat=await fd.stat()
    if(!stat.isDirectory()||stat.uid!==process.getuid!()||(stat.mode&0o777)!==0o700) throw new Error('pending directory must be private and owned')
    return fd
  } catch(error) { await fd.close(); throw error }
}
async function readAt(base:string,command:string) {
  const fd=await open(`${base}/${command}.money-request`,constants.O_RDONLY|constants.O_NOFOLLOW)
  try {
    const stat=await fd.stat()
    if(!stat.isFile()||stat.uid!==process.getuid!()||(stat.mode&0o777)!==0o600||stat.nlink!==1||stat.size>MAX) throw new Error('invalid pending file')
    const bytes=Buffer.alloc(stat.size+1)
    let used=0
    while(used<bytes.length) {
      const {bytesRead}=await fd.read(bytes,used,bytes.length-used,null)
      if(bytesRead===0) break
      used+=bytesRead
    }
    if(used!==stat.size) throw new Error('pending file changed during read')
    const result=decode(bytes.subarray(0,used),command)
    return {bytes:bytes.subarray(0,used),result}
  } finally { await fd.close() }
}
export async function readMoneyPending(root:string,command:string) {
  if(!uuid.test(command)) throw new Error('invalid pending command')
  const dir=await directory(root)
  try { return (await readAt(`/proc/self/fd/${dir.fd}`,command)).result }
  finally { await dir.close() }
}
// Durable per-character reservation primitive. No release API: a DB-confirmed
// current-state recovery protocol must own release before runtime adoption.
// Unlike a directory scan, the exclusive link serializes competing processes.
export async function claimMoneyCharacterFence(root:string,args:string[],frame:Buffer):Promise<'CLAIMED'|'EXACT_RETRY'> {
  const bytes=encode(args,frame)
  const key=hash(JSON.stringify([args[1],args[0]]))
  const dir=await directory(root),base=`/proc/self/fd/${dir.fd}`
  const target=`${base}/${key}.money-fence`,temp=`${base}/.${key}.${randomUUID()}.tmp`
  let created=false
  try {
    const fd=await open(temp,constants.O_WRONLY|constants.O_CREAT|constants.O_EXCL|constants.O_NOFOLLOW,0o600)
    created=true
    try {await fd.writeFile(bytes);await fd.sync()} finally {await fd.close()}
    let outcome:'CLAIMED'|'EXACT_RETRY'='CLAIMED'
    try {await link(temp,target)} catch(error) {
      if((error as NodeJS.ErrnoException).code!=='EEXIST') throw error
      const existing=await open(target,constants.O_RDONLY|constants.O_NOFOLLOW)
      try {
        const stat=await existing.stat()
        // A contender can observe the winner's temporary second link. Refuse
        // conservatively; never wait on, replace, or delete its reservation.
        if(!stat.isFile()||stat.uid!==process.getuid!()||(stat.mode&0o777)!==0o600||stat.nlink!==1||stat.size!==bytes.length)
          throw new Error('character has pending money fence')
        const found=Buffer.alloc(bytes.length+1)
        let used=0
        while(used<found.length) {
          const result=await existing.read(found,used,found.length-used,null)
          if(!result.bytesRead) break
          used+=result.bytesRead
        }
        if(used!==bytes.length||!found.subarray(0,used).equals(bytes)) throw new Error('character has pending money fence')
        outcome='EXACT_RETRY'
      } finally {await existing.close()}
    }
    await unlink(temp);created=false;await dir.sync();return outcome
  } finally {
    if(created) await unlink(temp).catch(()=>{})
    await dir.close()
  }
}
// Bounded sequential visitor: never hold many multi-megabyte requests in memory.
// Keep one directory capability across discovery and every record read.
export async function visitMoneyPending(root:string,visit:(request:{args:string[],frame:Buffer}|null)=>Promise<void>,limit=1000):Promise<boolean> {
  if(!Number.isInteger(limit)||limit<1||limit>1000) throw new Error('invalid scan limit')
  const dir=await directory(root),base=`/proc/self/fd/${dir.fd}`
  try {
    const entries=await opendir(base)
    let scanned=0
    for await(const entry of entries) {
      if(++scanned>limit) return true
      if(!entry.name.endsWith('.money-request')) continue
      const command=entry.name.slice(0,-'.money-request'.length)
      let request=null
      try { if(uuid.test(command)) request=(await readAt(base,command)).result } catch { /* preserve invalid record */ }
      await visit(request)
    }
    return false
  } finally { await dir.close() }
}
export async function prepareMoneyPending(root:string,args:string[],frame:Buffer):Promise<'PREPARED'|'EXACT_RETRY'> {
  const bytes=encode(args,frame),command=args[7]
  const dir=await directory(root),base=`/proc/self/fd/${dir.fd}`
  const temp=`${base}/.${command}.${randomUUID()}.tmp`,target=`${base}/${command}.money-request`
  let created=false
  try {
    const fd=await open(temp,constants.O_WRONLY|constants.O_CREAT|constants.O_EXCL|constants.O_NOFOLLOW,0o600)
    created=true
    try { await fd.writeFile(bytes); await fd.sync() } finally { await fd.close() }
    let outcome:'PREPARED'|'EXACT_RETRY'='PREPARED'
    try { await link(temp,target) }
    catch(error) {
      if((error as NodeJS.ErrnoException).code!=='EEXIST') throw error
      if(!(await readAt(base,command)).bytes.equals(bytes)) throw new Error('pending command conflict')
      outcome='EXACT_RETRY'
    }
    await unlink(temp); created=false
    await dir.sync()
    return outcome
  } finally {
    if(created) await unlink(temp).catch(()=>{})
    await dir.close()
  }
}

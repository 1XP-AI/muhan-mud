import {createHash} from 'node:crypto'
import {pendingDirectory,readPendingBytes,publishPendingBytes} from './pending-record-store.js'
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
export async function readPlayerPending(root:string,command:string):Promise<{args:string[],payload:Buffer}> {
  if(!uuid.test(command)) throw new Error('invalid player command')
  const dir=await pendingDirectory(root)
  try {
    const bytes=await readPendingBytes(`/proc/self/fd/${dir.fd}`,`${command}.player-request`)
    const envelope=JSON.parse(bytes.toString('utf8'))
    if(typeof envelope.body!=='string'||hash(envelope.body)!==envelope.sha256) throw new Error('player digest mismatch')
    const value=JSON.parse(envelope.body)
    if(value.version!==1||value.kind!=='player-save'||typeof value.payload!=='string') throw new Error('invalid player record')
    const payload=Buffer.from(value.payload,'base64'),args=value.args
    if(!encode(args,payload).equals(bytes)||args[5]!==command) throw new Error('noncanonical player record')
    return {args,payload}
  } finally {await dir.close()}
}

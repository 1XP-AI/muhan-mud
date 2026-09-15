// Explicit C helper: only echo the exact frame after durable preparation.
// No database connection, credentials or automatic request cleanup.
import {prepareMoneyPending,claimMoneyCharacterFence,visitMoneyPending} from './money-pending-request.js'
import {visitPlayerPending} from './player-pending-request.js'
async function main() {
  const [mode,root,...args]=process.argv.slice(2)
  if(mode!=='--prepare'||!root||args.length!==11) throw new Error('invalid preparation arguments')
  const chunks:Buffer[]=[]
  let length=0
  for await(const chunk of process.stdin) {
    const bytes=Buffer.from(chunk)
    length+=bytes.length
    if(length>8388616) throw new Error('oversize pending frame')
    chunks.push(bytes)
  }
  const frame=Buffer.concat(chunks)
  // Legacy request-only stores must be drained or adopted exactly, never
  // bypassed by adding a new command UUID. Old unfenced senders must be stopped
  // before adopting this protocol; the scan alone is not the race arbiter.
  let conflict=false
  const truncated=await visitMoneyPending(root,async request=>{
    if(!request) {conflict=true;return}
    if(request.args[0]===args[0]&&request.args[1]===args[1]
       &&(JSON.stringify(request.args)!==JSON.stringify(args)||!request.frame.equals(frame))) conflict=true
  },1000,true)
  const playersTruncated=await visitPlayerPending(root,async request=>{
    if(!request||(request.args[0]===args[1]&&request.args[4]===args[0])) conflict=true
  },1000,true)
  if(conflict||truncated||playersTruncated) throw new Error('pending recovery required before preparation')
  // Exclusive publication is the process-race arbiter. A crash after this
  // point leaves a complete recoverable fence even without a request file.
  await claimMoneyCharacterFence(root,args,frame)
  await prepareMoneyPending(root,args,frame)
  process.stdout.write(frame)
}
main().catch(()=>{process.exitCode=1})

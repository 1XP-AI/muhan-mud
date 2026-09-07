// New senders must share one private pending directory for both operations.
import {preparePlayerPending,claimPlayerCharacterFence,visitPlayerPending,verifyPlayerResolved,resolvePlayerCharacterFence} from './player-pending-request.js'
import {visitMoneyPending} from './money-pending-request.js'
async function main() {
  const [mode,root,...args]=process.argv.slice(2)
  if(!['--prepare','--verify-resolved','--record-verified-release'].includes(mode)||!root||args.length!==8) throw new Error('invalid player preparation')
  const chunks:Buffer[]=[];let length=0
  for await(const chunk of process.stdin) {
    const bytes=Buffer.from(chunk);length+=bytes.length
    if(length>4194304) throw new Error('oversize player payload')
    chunks.push(bytes)
  }
  const payload=Buffer.concat(chunks)
  if(mode==='--record-verified-release') {
    // Internal filesystem recorder, NOT a DB verifier or public recovery API.
    // Native caller has just checked exact history/current state under its
    // writer authority; this helper preserves the shared reservation protocol.
    await resolvePlayerCharacterFence(root,args,payload,async()=>{})
    process.stdout.write(payload);return
  }
  if(mode==='--verify-resolved') {
    await verifyPlayerResolved(root,args,payload);process.stdout.write(payload);return
  }
  let conflict=false
  const playersTruncated=await visitPlayerPending(root,async r=>{
    if(!r) {conflict=true;return}
    if(r.args[0]===args[0]&&r.args[4]===args[4]
       &&(JSON.stringify(r.args)!==JSON.stringify(args)||!r.payload.equals(payload))) conflict=true
  },1000,true)
  const moneyTruncated=await visitMoneyPending(root,async r=>{
    if(!r||(r.args[1]===args[0]&&r.args[0]===args[4])) conflict=true
  },1000,true)
  if(conflict||playersTruncated||moneyTruncated) throw new Error('pending recovery required')
  await claimPlayerCharacterFence(root,args,payload)
  await preparePlayerPending(root,args,payload)
  process.stdout.write(payload)
}
main().catch(()=>{process.exitCode=1})

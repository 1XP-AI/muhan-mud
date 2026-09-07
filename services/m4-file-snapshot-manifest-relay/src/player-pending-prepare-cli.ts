// Internal durable record helper, not per-character authority/fencing.
import {preparePlayerPending} from './player-pending-request.js'
async function main() {
  const [mode,root,...args]=process.argv.slice(2)
  if(mode!=='--prepare'||!root||args.length!==8) throw new Error('invalid player preparation')
  const chunks:Buffer[]=[];let length=0
  for await(const chunk of process.stdin) {
    const bytes=Buffer.from(chunk);length+=bytes.length
    if(length>4194304) throw new Error('oversize player payload')
    chunks.push(bytes)
  }
  const payload=Buffer.concat(chunks)
  await preparePlayerPending(root,args,payload)
  process.stdout.write(payload)
}
main().catch(()=>{process.exitCode=1})

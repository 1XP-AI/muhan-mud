// Explicit C helper: only echo the exact frame after durable preparation.
// No database connection, credentials or automatic request cleanup.
import {prepareMoneyPending} from './money-pending-request.js'
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
  await prepareMoneyPending(root,args,frame)
  process.stdout.write(frame)
}
main().catch(()=>{process.exitCode=1})

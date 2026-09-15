// Fresh-process disposable recovery probe. No command parameters arrive from
// the parent other than the filename's command identifier.
import {readMoneyPending} from '../dist/money-pending-request.js'
import {spawnSync} from 'node:child_process'
try {
  if(process.env.BANK_PAYLOAD_LOCAL_DISPOSABLE!=='1'||process.platform!=='linux') throw new Error('disposable only')
  const [root,command]=process.argv.slice(2)
  const request=await readMoneyPending(root,command)
  const result=spawnSync(process.env.BANK_TRANSFER_NATIVE_COMMIT,request.args,{
    input:request.frame,timeout:5000,maxBuffer:8192,env:process.env,
  })
  if(result.status!==0) throw new Error('pending replay not confirmed')
  process.stdout.write(result.stdout)
} catch { process.exitCode=1 }

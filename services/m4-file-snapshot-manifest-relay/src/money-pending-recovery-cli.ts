// Explicit single-pass reconciliation only. No writes, replay, acknowledgement
// deletion or authority acquisition. Provision the recovery lease separately.
import {createRequire} from 'node:module'
import {recoverMoneyPendingOnce} from './money-pending-recovery.js'
async function main() {
  if(process.env.MONEY_PENDING_RECOVERY_ENABLED!=='true'||process.argv.slice(2).join(' ')!=='--once') return 2
  const {MONEY_PENDING_ROOT:root,MONEY_RECOVERY_WORLD:world,MONEY_RECOVERY_WRITER:writer,MONEY_RECOVERY_EPOCH:epoch,MONEY_RECOVERY_DATABASE_URL:url}=process.env
  if(!root||!world||!writer||!epoch||!url) return 2
  const {Client}=createRequire(import.meta.url)('pg')
  const db=new Client({connectionString:url,connectionTimeoutMillis:2000,query_timeout:2500,
    statement_timeout:2000,lock_timeout:1000})
  try {
    await db.connect(); await db.query('set role mud_writer')
    const report=await recoverMoneyPendingOnce(root,world,writer,epoch,db)
    process.stdout.write(JSON.stringify(report)+'\n')
    return report.errors||report.invalid||report.unresolved||report.truncated?1:0
  } finally { await db.end() }
}
main().then(code=>{process.exitCode=code}).catch(()=>{console.error('money pending recovery failed');process.exitCode=1})

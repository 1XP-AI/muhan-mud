import {visitPlayerPending} from './player-pending-request.js'
import type {RecoveryQuery} from './money-pending-recovery.js'
// Read-only bounded discovery, including fence-only interrupted preparation.
// CONFIRMED is historical evidence, not authority to mutate live state/release.
export async function recoverPlayerPendingOnce(root:string,world:string,writer:string,epoch:string,db:RecoveryQuery) {
  if(!/^[-a-zA-Z0-9_.]{1,64}$/.test(world)||!(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/).test(writer)
     ||!(/^[1-9][0-9]{0,18}$/).test(epoch)||BigInt(epoch)>9223372036854775807n) throw new Error('invalid player recovery writer')
  const report={confirmed:0,unresolved:0,invalid:0,errors:0,truncated:false}
  report.truncated=await visitPlayerPending(root,async request=>{
    if(!request||request.args[0]!==world) {report.invalid++;return}
    const {args,payload}=request
    try {
      const result=await db.query('select * from private.reconcile_player_snapshot($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)',[...args,payload,writer,epoch])
      if(result.rows.length!==1) throw new Error('invalid player recovery response')
      const row=result.rows[0]
      if(row.outcome==='CONFIRMED'&&row.committed_revision===(BigInt(args[6])+1n).toString()) report.confirmed++
      else if(row.outcome==='UNRESOLVED'&&row.committed_revision===null) report.unresolved++
      else throw new Error('invalid player recovery response')
    } catch {report.errors++}
  })
  return report
}

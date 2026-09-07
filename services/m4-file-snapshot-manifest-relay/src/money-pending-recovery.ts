import {visitMoneyPending} from './money-pending-request.js'
export interface RecoveryQuery {
  query(sql:string,values?:unknown[]):Promise<{rows:Record<string,unknown>[]}>
}
export async function recoverMoneyPendingOnce(root:string,world:string,writer:string,epoch:string,db:RecoveryQuery) {
  if(!/^[-a-zA-Z0-9_.]{1,64}$/.test(world)||!(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/).test(writer)
     ||!(/^[1-9][0-9]{0,18}$/).test(epoch)||BigInt(epoch)>9223372036854775807n) throw new Error('invalid recovery writer')
  const report={confirmed:0,unresolved:0,invalid:0,errors:0,truncated:false}
  report.truncated=await visitMoneyPending(root,async request=>{
    if(!request||request.args[1]!==world) { report.invalid++; return }
    const {args,frame}=request,pl=frame.readUInt32BE(0)
    try {
      const result=await db.query('select * from private.reconcile_money_transfer($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)',
        [...args,frame.subarray(8,8+pl),frame.subarray(8+pl),writer,epoch])
      if(result.rows.length!==1) throw new Error('invalid recovery response')
      const row=result.rows[0]
      if(row.outcome==='CONFIRMED'&&row.committed_revision===(BigInt(args[8])+1n).toString()) report.confirmed++
      else if(row.outcome==='UNRESOLVED'&&row.committed_revision===null) report.unresolved++
      else throw new Error('invalid recovery response')
    } catch { report.errors++ }
  })
  return report
}

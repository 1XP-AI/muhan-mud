import {resolveMoneyCharacterFence} from './money-pending-request.js'
import type {RecoveryQuery} from './money-pending-recovery.js'
// Explicit, caller-owned live lease and source adoption required. This does not
// acquire authority, update gameplay, replay a transaction or release UNKNOWN.
export async function releaseConfirmedMoney(root:string,args:string[],frame:Buffer,db:RecoveryQuery):Promise<void> {
  await resolveMoneyCharacterFence(root,args,frame,async()=>{
    const pl=frame.readUInt32BE(0),player=frame.subarray(8,8+pl),bank=frame.subarray(8+pl)
    const confirmed=await db.query('select * from private.reconcile_money_transfer($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)',
      [...args,player,bank,args[5],args[6]])
    if(confirmed.rows.length!==1||confirmed.rows[0].outcome!=='CONFIRMED'||confirmed.rows[0].committed_revision!==(BigInt(args[8])+1n).toString()) throw new Error('unconfirmed money reservation')
    const current=await db.query('select * from private.read_qualified_money_transfer_state($1,$2,$3,$4,$5,$6,$7)',args.slice(0,7))
    const row=current.rows[0]
    if(current.rows.length!==1||row.revision!==(BigInt(args[8])+1n).toString()
       ||!Buffer.isBuffer(row.player_payload)||!row.player_payload.equals(player)
       ||!Buffer.isBuffer(row.bank_payload)||!row.bank_payload.equals(bank)) throw new Error('money source adoption required')
  })
}

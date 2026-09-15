import {createHash} from 'node:crypto'
import {resolvePlayerCharacterFence} from './player-pending-request.js'
import type {RecoveryQuery} from './money-pending-recovery.js'
// Caller still owns live baseline adoption. No gameplay mutation or new lease.
// Recovery writer can differ from the immutable request's original writer.
export async function releaseConfirmedPlayer(root:string,args:string[],payload:Buffer,writer:string,epoch:string,db:RecoveryQuery):Promise<void> {
  await resolvePlayerCharacterFence(root,args,payload,async()=>{
    const revision=(BigInt(args[6])+1n).toString()
    const confirmed=await db.query('select * from private.reconcile_player_snapshot($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)',[...args,payload,writer,epoch])
    if(confirmed.rows.length!==1||confirmed.rows[0].outcome!=='CONFIRMED'||confirmed.rows[0].committed_revision!==revision) throw new Error('unconfirmed player reservation')
    const current=await db.query('select * from private.read_player_paired_snapshot($1,$2,$3,$4,$5,$6)',[args[0],args[1],writer,epoch,args[4],revision])
    const row=current.rows[0]
    if(current.rows.length!==1||!Buffer.isBuffer(row.payload)||!row.payload.equals(payload)
       ||row.payload_hash!==createHash('sha256').update(payload).digest('hex')) throw new Error('player source adoption required')
  })
}

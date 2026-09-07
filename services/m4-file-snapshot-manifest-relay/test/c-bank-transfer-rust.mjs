import assert from 'node:assert/strict'
import {spawnSync} from 'node:child_process'
import {createHash} from 'node:crypto'
const c=process.env.BANK_TRANSFER_C_ENCODER, rust=process.env.BANK_TRANSFER_PLANNER
assert.ok(c?.startsWith('/') && rust?.startsWith('/'))
const encoded=spawnSync(c,['--frame'],{maxBuffer:9*1024*1024,timeout:5000})
assert.equal(encoded.status,0,encoded.stderr?.toString())
function transfer(frame,direction,badDigest=false) {
  const p=frame.readUInt32BE(0),b=frame.readUInt32BE(4)
  assert.equal(frame.length,8+p+b)
  const digest=bytes=>createHash('sha256').update(bytes).digest('hex')
  return spawnSync(rust,[direction,'25',badDigest?'0'.repeat(64):digest(frame.subarray(8,8+p)),digest(frame.subarray(8+p))],{
    input:frame,maxBuffer:9*1024*1024,timeout:5000,
  })
}
const rejected=transfer(encoded.stdout,'deposit',true)
assert.equal(rejected.status,1); assert.equal(rejected.stdout.length,0)
const deposit=transfer(encoded.stdout,'deposit')
assert.equal(deposit.status,0,deposit.stderr?.toString())
assert.notDeepEqual(deposit.stdout,encoded.stdout)
const withdraw=transfer(deposit.stdout,'withdraw')
assert.equal(withdraw.status,0,withdraw.stderr?.toString())
assert.deepEqual(withdraw.stdout,encoded.stdout)
console.log('GREEN real C paired frame -> Rust deposit/withdraw -> exact original bytes; wrong digest emits nothing')

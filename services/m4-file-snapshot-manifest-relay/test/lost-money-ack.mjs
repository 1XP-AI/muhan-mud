// Disposable loopback-only fault injection. Never used by the application.
import assert from 'node:assert/strict'
import {createServer,createConnection} from 'node:net'
import {spawn} from 'node:child_process'

export async function loseCommittedAck({port,binary,args,input,committed}) {
  assert.equal(process.env.BANK_PAYLOAD_LOCAL_DISPOSABLE,'1')
  assert.equal(process.platform,'linux')
  assert.ok(/^[1-9][0-9]{0,4}$/.test(String(port)) && Number(port)<=65535)
  const sockets=new Set()
  let suppress=false,dropped=0,connections=0,child,timer
  const proxy=createServer(client=>{
    connections++
    const upstream=createConnection({host:'127.0.0.1',port:Number(port)})
    sockets.add(client); sockets.add(upstream)
    let seen=Buffer.alloc(0)
    client.on('data',chunk=>{
      if(!suppress) {
        seen=Buffer.concat([seen,chunk])
        if(seen.includes(Buffer.from('private.commit_qualified_money_transfer'))) suppress=true
        seen=seen.subarray(Math.max(0,seen.length-128))
      }
      upstream.write(chunk)
    })
    upstream.on('data',chunk=>{
      if(suppress) dropped+=chunk.length
      else client.write(chunk)
    })
    for(const [socket,peer] of [[client,upstream],[upstream,client]]) {
      socket.on('error',()=>peer.destroy())
      socket.on('close',()=>{ sockets.delete(socket); peer.destroy() })
    }
  })
  try {
    await new Promise((resolve,reject)=>{ proxy.once('error',reject); proxy.listen(0,'127.0.0.1',resolve) })
    let output='',errors=''
    child=spawn(binary,args.map(String),{
      env:{...process.env,PGPORT:String(proxy.address().port),PGPASSWORD:'bank-local-contract-password',
        PGSSLMODE:'disable',PGOPTIONS:'',ASAN_OPTIONS:'detect_leaks=1:halt_on_error=1',UBSAN_OPTIONS:'halt_on_error=1'},
      stdio:['pipe','pipe','pipe'],
    })
    const exited=new Promise((resolve,reject)=>{
      child.once('error',reject)
      child.once('close',(code,signal)=>{ clearTimeout(timer); resolve({code,signal}) })
      timer=setTimeout(()=>{ child.kill('SIGKILL'); reject(new Error('native acknowledgement-loss watchdog expired')) },5000)
    })
    // Attach rejection immediately while observing the independent DB connection.
    exited.catch(()=>{})
    child.stdout.on('data',b=>{ output+=b.toString(); if(output.length>1024) child.kill('SIGKILL') })
    child.stderr.on('data',b=>{ errors+=b.toString(); if(errors.length>8192) child.kill('SIGKILL') })
    child.stdin.on('error',()=>{})
    child.stdin.end(input)
    const deadline=Date.now()+1500
    let durable=false
    while(Date.now()<deadline && child.exitCode===null) {
      if(await committed()) { durable=true; break }
      await new Promise(resolve=>setTimeout(resolve,20))
    }
    assert.equal(durable,true,'DB must confirm commit while native client still awaits the suppressed reply')
    const result=await exited
    assert.equal(connections,1); assert.equal(suppress,true); assert.ok(dropped>0)
    assert.equal(result.code,3,errors); assert.equal(result.signal,null)
    assert.equal(output,''); assert.equal(errors,'')
  } finally {
    clearTimeout(timer)
    if(child && child.exitCode===null) child.kill('SIGKILL')
    for(const socket of sockets) socket.destroy()
    await new Promise(resolve=>proxy.close(resolve))
  }
}

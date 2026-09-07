// Owned immutable transport records. No gameplay authority or automatic cleanup.
import {constants} from 'node:fs'
import {open,link,unlink} from 'node:fs/promises'
import {isAbsolute} from 'node:path'
import {randomUUID} from 'node:crypto'
const MAX=12*1024*1024
export async function pendingDirectory(root:string) {
  if(process.platform!=='linux'||!isAbsolute(root)) throw new Error('pending store requires absolute Linux directory')
  const fd=await open(root,constants.O_RDONLY|constants.O_DIRECTORY|constants.O_NOFOLLOW)
  try {
    const stat=await fd.stat()
    if(!stat.isDirectory()||stat.uid!==process.getuid!()||(stat.mode&0o777)!==0o700) throw new Error('pending directory must be private and owned')
    return fd
  } catch(error) {await fd.close();throw error}
}
function filename(name:string) {
  if(!/^[a-z0-9.-]{1,128}$/.test(name)||name.startsWith('.')) throw new Error('invalid record name')
}
export async function readPendingBytes(base:string,name:string):Promise<Buffer> {
  filename(name)
  const fd=await open(`${base}/${name}`,constants.O_RDONLY|constants.O_NOFOLLOW)
  try {
    const stat=await fd.stat()
    if(!stat.isFile()||stat.uid!==process.getuid!()||(stat.mode&0o777)!==0o600||stat.nlink!==1||stat.size>MAX) throw new Error('invalid pending file')
    const bytes=Buffer.alloc(stat.size+1);let used=0
    while(used<bytes.length) {
      const {bytesRead}=await fd.read(bytes,used,bytes.length-used,null)
      if(!bytesRead) break
      used+=bytesRead
    }
    if(used!==stat.size) throw new Error('pending file changed during read')
    return bytes.subarray(0,used)
  } finally {await fd.close()}
}
export async function publishPendingBytes(root:string,name:string,bytes:Buffer):Promise<'PREPARED'|'EXACT_RETRY'> {
  filename(name)
  if(bytes.length<1||bytes.length>MAX) throw new Error('invalid record size')
  const dir=await pendingDirectory(root),base=`/proc/self/fd/${dir.fd}`
  const temp=`${base}/.${randomUUID()}.tmp`;let created=false
  try {
    const fd=await open(temp,constants.O_WRONLY|constants.O_CREAT|constants.O_EXCL|constants.O_NOFOLLOW,0o600)
    created=true
    try {await fd.writeFile(bytes);await fd.sync()} finally {await fd.close()}
    let outcome:'PREPARED'|'EXACT_RETRY'='PREPARED'
    try {await link(temp,`${base}/${name}`)} catch(error) {
      if((error as NodeJS.ErrnoException).code!=='EEXIST') throw error
      if(!(await readPendingBytes(base,name)).equals(bytes)) throw new Error('pending command conflict')
      outcome='EXACT_RETRY'
    }
    await unlink(temp);created=false;await dir.sync();return outcome
  } finally {
    if(created) await unlink(temp).catch(()=>{})
    await dir.close()
  }
}

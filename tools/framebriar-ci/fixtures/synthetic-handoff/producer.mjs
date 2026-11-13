// Test fixture only. No real SDK, credentials, network or build system.
import fs from 'node:fs'
import {spawn,spawnSync} from 'node:child_process'
const [root,mode]=process.argv.slice(2)
if(mode==='mutator'){
 process.on('SIGTERM',()=>{})
 fs.writeFileSync(`${root}/unexpected.txt`,'synthetic background writer')
 setInterval(()=>fs.writeFileSync(`${root}/unexpected.txt`,'synthetic background writer'),5)
}else{
 fs.writeFileSync(`${root}/index.html`,'<p>synthetic preview</p>\n')
 fs.writeFileSync(`${root}/fake-sdk.txt`,'FAKE SDK: public synthetic fixture only\n')
 if(mode==='encoded')fs.writeFileSync(`${root}/fake-sdk.txt`,Buffer.from('FAKE PRIVATE SOURCE CANARY').toString('base64'))
 if(mode==='diagnostics')fs.writeFileSync(`${root}/diagnostics.json`,'FAKE DIAGNOSTIC CANARY: never publish')
 if(mode==='fifo'){fs.unlinkSync(`${root}/fake-sdk.txt`);const r=spawnSync('/usr/bin/mkfifo',[`${root}/fake-sdk.txt`],{env:{},stdio:'ignore'});if(r.status!==0)process.exit(7)}
 if(mode==='symlink'){fs.unlinkSync(`${root}/fake-sdk.txt`);fs.symlinkSync('index.html',`${root}/fake-sdk.txt`)}
 if(mode==='hardlink'){fs.unlinkSync(`${root}/fake-sdk.txt`);fs.linkSync(`${root}/index.html`,`${root}/fake-sdk.txt`)}
 if(mode==='extra')fs.writeFileSync(`${root}/unexpected.txt`,'extra')
 if(mode==='oversize')fs.writeFileSync(`${root}/fake-sdk.txt`,'x'.repeat(4097))
 if(mode==='missing')fs.unlinkSync(`${root}/fake-sdk.txt`)
 if(mode==='background'){
  process.on('SIGTERM',()=>{})
  spawn(process.execPath,[import.meta.filename,root,'mutator'],{env:{},stdio:'ignore'})
  const timer=setInterval(()=>{if(fs.existsSync(`${root}/unexpected.txt`)){clearInterval(timer);process.send({ready:true})}},5)
 }else if(mode!=='timeout')process.send({ready:true})
 setInterval(()=>{},1000) // Parent must terminate this group before inspection.
}

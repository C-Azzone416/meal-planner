import {test} from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import {execFileSync,spawnSync} from 'node:child_process'
import {validatePrebuild} from './prebuild-policy.mjs'
import {sha256} from './policy.mjs'
import {gzipSync,brotliCompressSync,deflateSync} from 'node:zlib'
const sha='a'.repeat(40),hash='b'.repeat(64)
const metadata=()=>({migration_readiness:{source_sha:sha,tree_sha:sha,policy:'no-migration-sources-v1'},source_sha:sha,target:'preview',enabled:false,screenshots:false,tar_sha256:hash,artifact_id:5,artifact_digest:`sha256:${hash}`,artifact_name:'framebriar-preview-output-12-1'})
const artifacts=()=>[{id:5,name:'framebriar-preview-output-12-1',digest:`sha256:${hash}`,expired:false,size_in_bytes:100,workflow_run:{id:12}}]
test('prebuild requires exact target SHA, artifact identity and disabled capability',()=>{
 validatePrebuild(metadata(),sha,12,1,artifacts())
 for(const change of [v=>v.source_sha='c'.repeat(40),v=>v.target='production',v=>v.enabled=true,v=>v.screenshots=true,v=>v.artifact_id=6,v=>v.tar_sha256='bad',v=>v.artifact_digest=`sha256:${'c'.repeat(64)}`]) {
  const value=metadata();change(value);assert.throws(()=>validatePrebuild(value,sha,12,1,artifacts()))
 }
 assert.throws(()=>validatePrebuild(metadata(),sha,12,2,artifacts()))
 assert.throws(()=>validatePrebuild(metadata(),sha,12,1,[...artifacts(),...artifacts()]))
})
function fixture(fn) {
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'prebuild-test-')), source=path.join(root,'source'),directory=path.join(root,'artifact')
 fs.mkdirSync(path.join(source,'.vercel/output/static'),{recursive:true})
 fs.mkdirSync(path.join(source,'.vercel/output/functions/api/feedback/status.func'),{recursive:true})
 fs.mkdirSync(directory)
 fs.writeFileSync(path.join(source,'.vercel/project.json'),JSON.stringify({projectId:'synthetic-project',orgId:'synthetic-org'}))
 fs.writeFileSync(path.join(source,'.vercel/output/static/framebriar-build.json'),JSON.stringify({enabled:false,environment:'preview',origin:'',appId:'meal-planner-preview',buildSha:sha,screenshots:false}))
 fs.writeFileSync(path.join(source,'.vercel/output/static/app.js'),`${sha} meal-planner-preview`)
 fs.writeFileSync(path.join(source,'.vercel/output/functions/api/feedback/status.func/bootstrap'),`${sha} preview meal-planner-preview`)
 fs.writeFileSync(path.join(source,'.vercel/output/functions/api/feedback/status.func/.vc-config.json'),'{}')
 const bin=path.join(root,'bin');fs.mkdirSync(bin)
 const tar=process.platform==='darwin'?execFileSync('which',['gtar'],{encoding:'utf8'}).trim():execFileSync('which',['tar'],{encoding:'utf8'}).trim()
 fs.symlinkSync(tar,path.join(bin,'tar'))
 const env={...process.env,PATH:`${bin}:${process.env.PATH}`,TARGET_SHA:sha,PREBUILD_DIRECTORY:directory,SEALED_PREBUILD_JSON:path.join(root,'sealed.json'),VERCEL_PROJECT_ID:'synthetic-project',VERCEL_ORG_ID:'synthetic-org'}
 const pack=()=>execFileSync(tar,['-cf',path.join(directory,'build-output.tar'),'.vercel/output','.vercel/project.json'],{cwd:source})
 const run=()=>spawnSync(process.execPath,['tools/framebriar-ci/seal-prebuild.mjs'],{env,encoding:'utf8'})
 const check=()=>spawnSync(process.execPath,['tools/framebriar-ci/artifact-tools/check-artifact.mjs',path.join(source,'.vercel/output/static')],{env:{...env,FRAMEBRIAR_BUILD_SHA:sha,FRAMEBRIAR_ENVIRONMENT:'preview',FRAMEBRIAR_APP_ID:'meal-planner-preview',FRAMEBRIAR_ENABLED:'0',FRAMEBRIAR_SCREENSHOTS:'0',FRAMEBRIAR_ALLOWED_ORIGIN:''},encoding:'utf8'})
 try {pack();fn({source,directory,env,pack,run,check,root})}finally{fs.rmSync(root,{recursive:true,force:true})}
}
test('clean sealer checks real tar bytes without executing them',()=>fixture(f=>{
 const r=f.run();assert.equal(r.status,0,r.stderr)
 const value=JSON.parse(fs.readFileSync(f.env.SEALED_PREBUILD_JSON,'utf8'))
 assert.equal(value.source_sha,sha)
 assert.equal(value.tar_sha256,sha256(fs.readFileSync(path.join(f.directory,'build-output.tar'))))
}))
test('sealer rejects checksum mismatch, wrong project, wrong target and source maps',()=>{
 fixture(f=>{f.env.EXPECTED_PREBUILD_JSON=path.join(f.root,'expected.json');fs.writeFileSync(f.env.EXPECTED_PREBUILD_JSON,JSON.stringify(metadata()));assert.notEqual(f.run().status,0)})
 fixture(f=>{f.env.VERCEL_PROJECT_ID='different';assert.notEqual(f.run().status,0)})
 fixture(f=>{const p=path.join(f.source,'.vercel/output/static/framebriar-build.json');const c=JSON.parse(fs.readFileSync(p));c.environment='production';fs.writeFileSync(p,JSON.stringify(c));f.pack();assert.notEqual(f.run().status,0)})
 fixture(f=>{fs.writeFileSync(path.join(f.source,'.vercel/output/static/app.js.map'),'synthetic');f.pack();assert.notEqual(f.run().status,0)})
})
test('sealer rejects hidden runtime files, dependencies and extra download files',()=>{
 fixture(f=>{fs.writeFileSync(path.join(f.source,'.vercel/output/.env'),'synthetic');f.pack();assert.notEqual(f.run().status,0)})
 fixture(f=>{fs.mkdirSync(path.join(f.source,'.vercel/output/node_modules'));f.pack();assert.notEqual(f.run().status,0)})
 fixture(f=>{fs.writeFileSync(path.join(f.directory,'unexpected'),'synthetic');assert.notEqual(f.run().status,0)})
})

test('sealer rejects inline and external JS/CSS source-map directives in real archives',()=>{
 const payload=Buffer.from(JSON.stringify({version:3,sources:['synthetic.ts'],sourcesContent:['synthetic source'],mappings:''})).toString('base64')
 for(const [name,directive] of [
  ['inline.js',`//# sourceMappingURL=data:application/json;base64,${payload}`],
  ['legacy.js',`//@ sourceMappingURL=data:application/json;charset=utf-8;base64,${payload}`],
  ['inline.css',`/*# sourceMappingURL=data:application/json;base64,${payload} */`],
  ['legacy.css',`/*@ sourceMappingURL=data:application/json;base64,${payload} */`],
  ['external.mjs','//# sourceMappingURL=original.js.map'],
  ['external.css','/*# sourceMappingURL = original.css.map */'],
 ]) fixture(f=>{fs.writeFileSync(path.join(f.source,'.vercel/output/static',name),directive);f.pack();assert.notEqual(f.run().status,0,name)})
})

const inlineMap = '/*# sourceMappingURL=data:application/json;base64,eyJ2ZXJzaW9uIjozfQ== */'
test('pre-upload artifact check and clean seal reject compressed assets even without deployment',()=>{
 for(const [name,bytes] of [
  ['style.css.gz',gzipSync(inlineMap)], ['style.css.br',brotliCompressSync(inlineMap)],
  ['sdk.tgz',gzipSync('raw package archive')], ['renamed.css',gzipSync(inlineMap)],
  ['renamed.deflate',deflateSync(inlineMap)], ['extensionless',deflateSync(inlineMap)],
  ['style.MAP',Buffer.from('{}')],
 ]) fixture(f=>{
  const staticRoot=path.join(f.source,'.vercel/output/static')
  fs.writeFileSync(path.join(staticRoot,name),bytes)
  const env={...f.env,FRAMEBRIAR_BUILD_SHA:sha,FRAMEBRIAR_ENVIRONMENT:'preview',FRAMEBRIAR_APP_ID:'meal-planner-preview',FRAMEBRIAR_ENABLED:'0',FRAMEBRIAR_SCREENSHOTS:'0',FRAMEBRIAR_ALLOWED_ORIGIN:''}
  const check=spawnSync(process.execPath,['tools/framebriar-ci/artifact-tools/check-artifact.mjs',staticRoot],{env,encoding:'utf8'})
  assert.notEqual(check.status,0,`artifact checker accepted ${name}`)
  f.pack();assert.notEqual(f.run().status,0,`clean seal accepted ${name}`)
  assert.equal(fs.existsSync(f.env.SEALED_PREBUILD_JSON),false)
 })
})
test('clean seal scans function assets and rejects encoded rewritten routes',()=>{
 fixture(f=>{
  fs.writeFileSync(path.join(f.source,'.vercel/output/functions/api/feedback/status.func/helper.js'),inlineMap)
  f.pack();assert.notEqual(f.run().status,0)
 })
 for(const encoding of ['gzip','br','deflate']) fixture(f=>{
  // An extensionless Brotli/deflate asset must not evade the guard by being
  // reached through a Vercel route rather than a conventional .br/.gz filename.
  fs.writeFileSync(path.join(f.source,'.vercel/output/static/opaque'),brotliCompressSync(inlineMap))
  fs.writeFileSync(path.join(f.source,'.vercel/output/config.json'),JSON.stringify({version:3,routes:[{src:'/style.css',dest:'/opaque',headers:{'CoNtEnT-EnCoDiNg':encoding}}]}))
  f.pack();assert.notEqual(f.run().status,0,encoding)
 })
 fixture(f=>{
  fs.writeFileSync(path.join(f.source,'.vercel/output/config.json'),JSON.stringify({version:3,routes:[{src:'/style.css',headers:[{key:'Content-Encoding',value:'br'}]}]}))
  f.pack();assert.notEqual(f.run().status,0)
 })
})

test('clean seal rejects private SDK manifests outside node_modules',()=>fixture(f=>{
 const vendor=path.join(f.source,'.vercel/output/static/vendor/sdk')
 fs.mkdirSync(vendor,{recursive:true})
 fs.writeFileSync(path.join(vendor,'package.json'),JSON.stringify({name:'@masra91/framebriar',version:'0.4.1'}))
 fs.writeFileSync(path.join(vendor,'index.js'),'export const secret=1')
 f.pack();assert.notEqual(f.run().status,0)
}))

// Real archives exercise both gates, not just the shared policy function.
const unicodeMap = `${inlineMap}\nbody { color: red }`
const utf16le = Buffer.from(unicodeMap, 'utf16le')
const utf16be = Buffer.from(utf16le).swap16()
const utf32le = Buffer.alloc(unicodeMap.length * 4)
for (let i = 0; i < unicodeMap.length; i++) utf32le.writeUInt32LE(unicodeMap.charCodeAt(i), i * 4)
const utf32be = Buffer.from(utf32le).swap32()
for (const [encoding, bom, bytes] of [
 ['UTF-16LE', 'fffe', utf16le], ['UTF-16BE', 'feff', utf16be],
 ['UTF-32LE', 'fffe0000', utf32le], ['UTF-32BE', '0000feff', utf32be],
]) {
 for (const name of ['style.css', 'script.js', 'opaque']) {
  test(`both artifact gates reject ${encoding} BOM in ${name}`,()=>fixture(f=>{
   fs.writeFileSync(path.join(f.source,'.vercel/output/static',name),Buffer.concat([Buffer.from(bom,'hex'),bytes]))
   assert.notEqual(f.check().status,0)
   f.pack();assert.notEqual(f.run().status,0)
   assert.equal(fs.existsSync(f.env.SEALED_PREBUILD_JSON),false)
  }))
 }
 for (const name of ['style.css', 'opaque']) {
  test(`both artifact gates reject ${encoding} ${name} without BOM`,()=>fixture(f=>{
   fs.writeFileSync(path.join(f.source,'.vercel/output/static',name),bytes)
   assert.notEqual(f.check().status,0)
   f.pack();assert.notEqual(f.run().status,0)
   assert.equal(fs.existsSync(f.env.SEALED_PREBUILD_JSON),false)
  }))
 }
 test(`clean seal rejects unaligned ${encoding} marker inside binary function`,()=>fixture(f=>{
  fs.appendFileSync(path.join(f.source,'.vercel/output/functions/api/feedback/status.func/bootstrap'),Buffer.concat([Buffer.from([0x7f]),bytes]))
  f.pack();assert.notEqual(f.run().status,0)
  assert.equal(fs.existsSync(f.env.SEALED_PREBUILD_JSON),false)
 }))
}
test('both gates reject malformed UTF-8 CSS',()=>fixture(f=>{
 fs.writeFileSync(path.join(f.source,'.vercel/output/static/style.css'),Buffer.from([0xc0,0xaf]))
 assert.notEqual(f.check().status,0)
 f.pack();assert.notEqual(f.run().status,0)
}))
test('both gates accept valid non-ASCII UTF-8 text, BOM and binary assets',()=>fixture(f=>{
 fs.writeFileSync(path.join(f.source,'.vercel/output/static/style.css'),'\ufeff/* Café 🍴 */ body { color: red }')
 fs.writeFileSync(path.join(f.source,'.vercel/output/static/icon.png'),Buffer.from('89504e470d0a1a0a0000000d4948445200000001','hex'))
 // A compiled Go function is binary, not a UTF-8 text file.
 fs.appendFileSync(path.join(f.source,'.vercel/output/functions/api/feedback/status.func/bootstrap'),Buffer.from([0,0xff,0x80,0]))
 const check=f.check();assert.equal(check.status,0,check.stderr)
 f.pack();const seal=f.run();assert.equal(seal.status,0,seal.stderr)
}))

const shiftedMapMarker = [...'sourceMappingURL'].map((letter, index) => `${index % 2 ? '\x1b(B' : '\x1b(J'}${letter}`).join('') + '\x1b(B'
const iso2022Css = Buffer.from(`@charset "iso-2022-jp";\nbody{} ${inlineMap.replace('sourceMappingURL', shiftedMapMarker)}`, 'ascii')
test('both gates reject the browser-decodable ISO-2022-JP source map repro',()=>fixture(f=>{
 const decoded = new TextDecoder('iso-2022-jp', {fatal: true}).decode(iso2022Css)
 assert.ok(decoded.includes(inlineMap), 'Fixture must decode to the original inline map')
 fs.writeFileSync(path.join(f.source,'.vercel/output/static/style.css'),iso2022Css)
 fs.writeFileSync(path.join(f.source,'.vercel/output/config.json'),JSON.stringify({version:3,routes:[{src:'/style.css',headers:{'Content-Type':'text/css; charset=iso-2022-jp'}}]}))
 assert.notEqual(f.check().status,0,'Pre-upload checker accepted ISO-2022-JP map')
 f.pack();assert.notEqual(f.run().status,0,'Clean sealer accepted ISO-2022-JP map')
 assert.equal(fs.existsSync(f.env.SEALED_PREBUILD_JSON),false)
}))
for (const name of ['style.css', 'opaque', 'image.png']) {
 test(`both gates reject stateful encoding shifts in ${name} without declarations`,()=>fixture(f=>{
  fs.writeFileSync(path.join(f.source,'.vercel/output/static',name),Buffer.from(inlineMap.replace('sourceMappingURL',shiftedMapMarker),'ascii'))
  assert.notEqual(f.check().status,0)
  f.pack();assert.notEqual(f.run().status,0)
 }))
}
for (const [name, text] of [
 ['style.css', '@charset "iso-2022-jp"; body{}'],
 ['opaque', '@charset "windows-1252"; body{}'],
 ['page.html', '<meta charset="iso-2022-jp">'],
 ['page.html', '<meta http-equiv="Content-Type" content="text/html; charset=shift_jis">'],
 ['script.html', '<script charset="iso-2022-jp" src="/opaque"></script>'],
 ['style.html', '<link charset="iso-2022-jp" href="/opaque" rel="stylesheet">'],
 ['image.svg', '<?xml version="1.0" encoding="shift_jis"?><svg/>'],
 ['style.css', '/* state control: \x1b */ body{}'],
]) {
 test(`both gates reject non-UTF-8 declaration or control in ${name}: ${JSON.stringify(text)}`,()=>fixture(f=>{
  fs.writeFileSync(path.join(f.source,'.vercel/output/static',name),text)
  assert.notEqual(f.check().status,0)
  f.pack();assert.notEqual(f.run().status,0)
 }))
}
for (const header of [
 'text/css; charset=iso-2022-jp', 'text/css; ChArSeT = "shift_jis"',
 'text/css; charset=utf-8; charset=iso-2022-jp', 'text/css; charset=',
 'text/css; charset=unicode', 'text/css\r\nContent-Encoding: gzip',
 'text/css\n', 'text/css\u2028',
]) {
 for (const style of ['map','pairs','override']) {
  test(`clean seal rejects ${style} routing Content-Type ${JSON.stringify(header)} with clean assets`,()=>fixture(f=>{
   const headers = style === 'map' ? {'CoNtEnT-TyPe':header} : [{key:'CONTENT-TYPE',value:header}]
   const config = style === 'override' ? {version:3,overrides:{opaque:{path:'style.css',contentType:header}}} : {version:3,routes:[{src:'/style.css',dest:'/opaque',headers}]}
   fs.writeFileSync(path.join(f.source,'.vercel/output/config.json'),JSON.stringify(config))
   fs.writeFileSync(path.join(f.source,'.vercel/output/static/opaque'),'body{}')
   f.pack();assert.notEqual(f.run().status,0)
  }))
 }
}
test('both gates accept UTF-8 declarations, ordinary whitespace and UTF-8 route headers',()=>fixture(f=>{
 fs.writeFileSync(path.join(f.source,'.vercel/output/static/style.css'),'@charset "UTF-8";\n\tbody { color: red }\r\n\f')
 fs.writeFileSync(path.join(f.source,'.vercel/output/static/index.html'),'<meta charset="utf-8"><meta http-equiv="Content-Type" content="text/html; charset=utf-8">')
 fs.writeFileSync(path.join(f.source,'.vercel/output/static/image.svg'),'<?xml version="1.0" encoding="UTF-8"?><svg/>')
 fs.writeFileSync(path.join(f.source,'.vercel/output/config.json'),JSON.stringify({version:3,overrides:{'image.svg':{contentType:'image/svg+xml; charset=utf-8'}},routes:[
  {src:'/style.css',headers:{'Content-Type':'text/css; charset="UTF-8"','Cache-Control':'public, max-age=3600'}},
  {src:'/index.html',headers:{'content-type':'text/html; charset=utf-8'}},
  {src:'/image.svg',headers:{'Content-Type':'image/svg+xml'}},
 ]}))
 const check=f.check();assert.equal(check.status,0,check.stderr)
 f.pack();const seal=f.run();assert.equal(seal.status,0,seal.stderr)
}))

const headerMapData = `data:application/json;base64,${Buffer.from(JSON.stringify({version:3,sources:['synthetic.css'],sourcesContent:['synthetic private source'],names:[],mappings:'AAAA'})).toString('base64')}`
for (const key of ['SourceMap','X-SourceMap']) {
 for (const form of ['map','pairs','set','append','finite']) {
  test(`clean seal rejects ${key} inline map through ${form} response headers`,()=>fixture(f=>{
   const route={src:'/style.css'}
   if(form==='map')route.headers={[key]:headerMapData}
   else if(form==='pairs')route.headers=[{key,value:headerMapData}]
   else route.transforms=[{type:'response.headers',op:form==='append'?'append':'set',target:{key:form==='finite'?{inc:[key]}:key},args:headerMapData}]
   fs.writeFileSync(path.join(f.source,'.vercel/output/config.json'),JSON.stringify({version:3,routes:[route]}))
   fs.writeFileSync(path.join(f.source,'.vercel/output/static/style.css'),'body{}')
   f.pack();assert.notEqual(f.run().status,0)
   assert.equal(fs.existsSync(f.env.SEALED_PREBUILD_JSON),false)
  }))
 }
}
test('clean seal accepts valid UTF-8 response-header transform args',()=>fixture(f=>{
 fs.writeFileSync(path.join(f.source,'.vercel/output/config.json'),JSON.stringify({version:3,routes:[{src:'/style.css',transforms:[{type:'response.headers',op:'set',target:{key:'Content-Type'},args:'text/css; charset=utf-8'}]}]}))
 fs.writeFileSync(path.join(f.source,'.vercel/output/static/style.css'),'body{}')
 f.pack();const result=f.run();assert.equal(result.status,0,result.stderr)
}))
test('clean seal rejects non-UTF-8 transform args even with a misleading target.value',()=>fixture(f=>{
 fs.writeFileSync(path.join(f.source,'.vercel/output/config.json'),JSON.stringify({version:3,routes:[{src:'/style.css',transforms:[{type:'response.headers',op:'set',target:{key:'Content-Type',value:'text/css; charset=utf-8'},args:'text/css; charset=iso-2022-jp'}]}]}))
 f.pack();assert.notEqual(f.run().status,0)
}))
test('clean seal accepts incidental native binary shifts while retaining provenance',()=>fixture(f=>{
 fs.appendFileSync(path.join(f.source,'.vercel/output/functions/api/feedback/status.func/bootstrap'),Buffer.from([0,0xff,0x1b,0x28,0x42,0,0x1b,0x24,0x42]))
 f.pack();const result=f.run();assert.equal(result.status,0,result.stderr)
}))


test('both artifact gates reject valid Wasm with a source-map custom section',()=>fixture(f=>{
 const leb=value=>{const bytes=[];do{const byte=value%128;value=Math.floor(value/128);bytes.push(byte|(value?128:0))}while(value);return Buffer.from(bytes)}
 const name=Buffer.from('sourceMappingURL'),url=Buffer.from(headerMapData)
 const payload=Buffer.concat([leb(name.length),name,leb(url.length),url])
 const bytes=Buffer.concat([Buffer.from([0,97,115,109,1,0,0,0,0]),leb(payload.length),payload])
 assert.equal(WebAssembly.validate(bytes),true)
 fs.writeFileSync(path.join(f.source,'.vercel/output/static/opaque'),bytes)
 assert.notEqual(f.check().status,0)
 f.pack();assert.notEqual(f.run().status,0)
 assert.equal(fs.existsSync(f.env.SEALED_PREBUILD_JSON),false)
}))

// Both the pre-upload build job and clean reporter execute seal-prebuild.mjs.
// Exercise their actual archive boundary with schema-realistic ISR artifacts.
function addPrerender(f, metadata) {
 const base=path.join(f.source,'.vercel/output/functions/blog/post')
 fs.mkdirSync(`${base}.func`,{recursive:true})
 fs.writeFileSync(path.join(`${base}.func`,'.vc-config.json'),JSON.stringify({runtime:'nodejs22.x',handler:'index.js'}))
 fs.writeFileSync(path.join(`${base}.func`,'index.js'),'module.exports = (req, res) => res.end("synthetic");')
 fs.writeFileSync(`${base}.prerender-fallback.html`,'<!doctype html><p>Synthetic fallback</p>')
 fs.writeFileSync(`${base}.prerender-config.json`,JSON.stringify({expiration:60,initialStatus:200,fallback:{type:'FileFsRef',fsPath:'post.prerender-fallback.html',contentType:'text/html; charset=utf-8'},...metadata}))
}
for (const [label, metadata] of [
 ['inline SourceMap',{initialHeaders:{SourceMap:headerMapData}}],
 ['inline X-SourceMap',{initialHeaders:{'x-SOURCEMAP':headerMapData}}],
 ['external SourceMap',{initialHeaders:{SourceMap:'/source-map'}}],
 ['Shift_JIS Content-Type',{initialHeaders:{'content-type':'text/html; charset=Shift_JIS'}}],
 ['ISO-2022-JP Content-Type',{initialHeaders:{'Content-Type':'text/html; charset=iso-2022-jp'}}],
 ['Content-Encoding',{initialHeaders:{'Content-Encoding':'br'}}],
 ['fallback Content-Type',{fallback:{type:'FileFsRef',fsPath:'post.prerender-fallback.html',contentType:'text/html; charset=Shift_JIS'}}],
]) {
 test(`shared pre-upload and clean sealer reject prerender ${label}`,()=>fixture(f=>{
  addPrerender(f,metadata);f.pack()
  assert.notEqual(f.run().status,0)
  assert.equal(fs.existsSync(f.env.SEALED_PREBUILD_JSON),false)
 }))
}
test('shared pre-upload and clean sealer accept UTF-8 prerender and ordinary JSON data',()=>fixture(f=>{
 addPrerender(f,{initialHeaders:{'Content-Type':'text/html; charset=utf-8','Cache-Control':'public, max-age=60'}})
 const data=JSON.stringify({headers:[{column:'Recipe'}],contentType:'Recipe',transforms:'mash',initialHeaders:{'X-SourceMap':headerMapData,'Content-Type':'text/html; charset=Shift_JIS'}})
 for(const file of ['static/recipes.json','static/config.json','static/page.prerender-config.json','functions/blog/post.func/config.json','functions/blog/post.func/nested/data.prerender-config.json']) {
  const full=path.join(f.source,'.vercel/output',file);fs.mkdirSync(path.dirname(full),{recursive:true});fs.writeFileSync(full,data)
 }
 fs.writeFileSync(path.join(f.source,'.vercel/output/config.json'),JSON.stringify({version:3,routes:[{src:'/blog/post',headers:{'Content-Type':'text/html; charset=utf-8'}}],flags:JSON.parse(data)}))
 assert.equal(f.check().status,0)
 f.pack();const result=f.run();assert.equal(result.status,0,result.stderr)
 assert.equal(JSON.parse(fs.readFileSync(f.env.SEALED_PREBUILD_JSON)).source_sha,sha)
}))


for(const [relative, metadata] of [
 ['config.json',{version:3,routes:[{src:'/x',headers:{'X-SourceMap':headerMapData}}]}],
 ['config.json',{version:3,routes:[{src:'/x',transforms:[{type:'response.headers',op:'set',target:{key:'Content-Type'},args:'text/html; charset=Shift_JIS'}]}]}],
 ['config.json',{version:3,overrides:{x:{contentType:'text/html; charset=Shift_JIS'}}}],
 ['functions/page.prerender-config.json',{expiration:60,initialHeaders:{'X-SourceMap':headerMapData}}],
 ['functions/page.prerender-config.json',{expiration:60,fallback:{fsPath:'page.html',contentType:'text/html; charset=Shift_JIS'}}],
]) {
 test(`shared sealer rejects unsafe service metadata ${relative} ${JSON.stringify(metadata).slice(0,70)}`,()=>fixture(f=>{
  const base=path.join(f.source,'.vercel/output/services/web')
  fs.mkdirSync(path.join(base,'functions/page.func'),{recursive:true})
  fs.writeFileSync(path.join(base,'functions/page.func/.vc-config.json'),'{}')
  fs.writeFileSync(path.join(base,relative),JSON.stringify(metadata))
  f.pack();assert.notEqual(f.run().status,0)
  assert.equal(fs.existsSync(f.env.SEALED_PREBUILD_JSON),false)
 }))
}
test('shared sealer accepts safe service metadata without interpreting service data JSON',()=>fixture(f=>{
 const base=path.join(f.source,'.vercel/output/services/web')
 fs.mkdirSync(path.join(base,'functions/page.func'),{recursive:true});fs.mkdirSync(path.join(base,'static'))
 fs.writeFileSync(path.join(base,'functions/page.func/.vc-config.json'),'{}')
 fs.writeFileSync(path.join(base,'config.json'),JSON.stringify({version:3,routes:[{src:'/x',headers:{'Content-Type':'text/html; charset=utf-8'}}]}))
 fs.writeFileSync(path.join(base,'functions/page.prerender-config.json'),JSON.stringify({expiration:60,initialHeaders:{'Content-Type':'text/html; charset=utf-8'}}))
 const data=JSON.stringify({headers:[],contentType:'Recipe',initialHeaders:{'X-SourceMap':headerMapData}})
 fs.writeFileSync(path.join(base,'static/config.json'),data)
 fs.writeFileSync(path.join(base,'functions/page.func/page.prerender-config.json'),data)
 f.pack();const result=f.run();assert.equal(result.status,0,result.stderr)
}))


test('shared sealer explicitly rejects nested service declarations and layouts',()=>{
 for(const relative of ['services/web/config.json','services/web/services/api/config.json']) fixture(f=>{
  const full=path.join(f.source,'.vercel/output',relative)
  fs.mkdirSync(path.dirname(full),{recursive:true})
  fs.writeFileSync(full,JSON.stringify({version:3,services:[{name:'api'}]}))
  f.pack();assert.notEqual(f.run().status,0)
  assert.equal(fs.existsSync(f.env.SEALED_PREBUILD_JSON),false)
 })
})

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
 try {pack();fn({source,directory,env,pack,run,root})}finally{fs.rmSync(root,{recursive:true,force:true})}
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
  fs.writeFileSync(path.join(f.source,'.vercel/output/config.json'),JSON.stringify({headers:[{source:'/style.css',headers:[{key:'Content-Encoding',value:'br'}]}]}))
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

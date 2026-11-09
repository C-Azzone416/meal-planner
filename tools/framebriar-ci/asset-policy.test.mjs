import {test} from 'node:test'
import assert from 'node:assert/strict'
import {inspectAsset, inspectBuildOutputAsset} from './artifact-tools/asset-policy.mjs'

const inspectConfig = config => inspectBuildOutputAsset('config.json', Buffer.from(JSON.stringify(config)))
const transform = (key, args, op = 'set') => ({type:'response.headers', op, target:{key}, ...(args === undefined ? {} : {args})})
const config = transforms => ({version:3, routes:[{src:'/app.css', transforms}]})
const sourceMap = `data:application/json;base64,${Buffer.from(JSON.stringify({version:3,sources:['synthetic.css'],sourcesContent:['synthetic private source'],names:[],mappings:'AAAA'})).toString('base64')}`

// Deterministic representation of incidental ELF debug bytes, not an exemption
// for an ELF magic prefix. Real unstripped feedback binaries are also sealed in
// the local cross-build regression pass.
const debugBytes = Buffer.concat([Buffer.from([0x7f,0x45,0x4c,0x46,0,0xff]), Buffer.from('\x1b(B\x1b(J\x1b(I\x1b$B\x1b$@\x1b$(D\n@charset "iso-2022-jp";')])
test('native debug bytes do not trigger text charset enforcement', () => assert.doesNotThrow(() => inspectAsset('bootstrap',debugBytes)))
test('binary-looking prefix does not exempt named text assets', () => assert.throws(() => inspectAsset('style.css',debugBytes)))
test('binary assets still reject raw source-map directives', () => assert.throws(() => inspectAsset('bootstrap',Buffer.concat([debugBytes,Buffer.from('\n/*# sourceMappingURL='+sourceMap+' */')]))))
test('binary assets still reject ISO-2022-JP encoded source-map directives', () => {
 const shifted=[...'sourceMappingURL'].map((c,i)=>`${i%2?'\x1b(B':'\x1b(J'}${c}`).join('')+'\x1b(B'
 assert.throws(() => inspectAsset('bootstrap',Buffer.concat([debugBytes,Buffer.from('\x1b(B\n/*# '+shifted+'='+sourceMap+' */')])))
})
for (const key of ['SourceMap','sOuRcEmAp','X-SourceMap','x-SOURCEMAP']) {
 for (const value of [sourceMap, '/opaque-source-data']) {
  test(`response header map rejects ${key}: ${value === sourceMap ? 'inline' : 'external'}`, () => {
   assert.throws(() => inspectConfig({routes:[{src:'/app.css',headers:{[key]:value}}]}))
  })
  test(`unsupported response header pair arrays reject ${key}: ${value === sourceMap ? 'inline' : 'external'}`, () => {
   assert.throws(() => inspectConfig({routes:[{src:'/app.css',headers:[{key,value}]}]}))
  })
  for (const op of ['set','append']) {
   test(`${op} response transform rejects ${key}: ${value === sourceMap ? 'inline' : 'external'}`, () => {
    for (const target of [key,{eq:key},{inc:['X-Ordinary',key]}]) assert.throws(() => inspectConfig(config([transform(target,value,op)])))
   })
  }
 }
}
for (const args of ['text/css; charset=utf-8',['text/css; charset="UTF-8"']]) {
 for (const op of ['set','append']) {
  test(`${op} Content-Type transform accepts UTF-8 args ${JSON.stringify(args)}`, () => {
   for (const target of ['Content-Type',{eq:'CONTENT-TYPE'},{inc:['Content-Type']}]) {
    assert.doesNotThrow(() => inspectConfig(config([transform(target,args,op)])))
   }
  })
 }
}
for (const args of ['text/css; charset=iso-2022-jp',undefined,[],['text/css','text/html'],{value:'text/css'}]) {
 test(`Content-Type transform rejects unsafe args ${JSON.stringify(args)}`, () => {
  const t=transform('Content-Type',args);t.target.value='text/css; charset=utf-8'
  assert.throws(() => inspectConfig(config([t])))
 })
}
test('response transforms reject content encoding, ambiguous targets and injected header lines', () => {
 for (const t of [
  transform('Content-Encoding','br'), transform({eq:'Content-Encoding'},'gzip'),
  transform({pre:'Source'},sourceMap),transform('bad header',sourceMap),
  transform('X-Ordinary',`ok\r\nSourceMap: ${sourceMap}`),
 ]) assert.throws(() => inspectConfig(config([t])))
})
test('response-header deletion and unrelated transforms remain valid', () => {
 assert.doesNotThrow(() => inspectConfig(config([
  transform('Content-Type',undefined,'delete'),transform('SourceMap',undefined,'delete'),
  transform({pre:'X-'},undefined,'delete'),transform('Cache-Control',['public','max-age=3600'],'append'),
  {type:'request.headers',op:'set',target:{key:'Content-Type'},args:'application/json'},
  {type:'request.path',op:'set',args:'/new-path'},
 ])))
})
test('request predicates are not mistaken for response-header pairs', () => {
 assert.doesNotThrow(() => inspectConfig({version:3,routes:[{src:'/app.css',has:[{type:'header',key:'Content-Type',value:{eq:'application/json'}}],missing:[{type:'header',key:'Content-Encoding'}],headers:{'Content-Type':'text/css; charset=utf-8'}}]}))
})

test('ordinary legal header tokens remain supported', () => assert.doesNotThrow(() => inspectConfig({routes:[{src:'/app.css',headers:{'X_Custom.Header':'ok'}}]})))


const uleb = value => {
 const bytes=[]
 do { const byte=value%128; value=Math.floor(value/128); bytes.push(byte | (value ? 128 : 0)) } while(value)
 return Buffer.from(bytes)
}
const wasmHeader=Buffer.from([0,97,115,109,1,0,0,0])
function wasmSection(name, value) {
 const label=Buffer.from(name), payload=Buffer.concat([uleb(label.length),label,Buffer.from(value)])
 return Buffer.concat([wasmHeader,Buffer.from([0]),uleb(payload.length),payload])
}
for(const name of ['module.wasm','opaque','renamed.png']) {
 test(`Wasm source-map custom section rejects regardless of filename: ${name}`,()=>{
  for(const url of [sourceMap,'/external-map']) {
   const raw=Buffer.from(url), bytes=wasmSection('sourceMappingURL',Buffer.concat([uleb(raw.length),raw]))
   assert.equal(WebAssembly.validate(bytes),true)
   assert.throws(()=>inspectAsset(name,bytes),/Wasm source-map/)
  }
 })
}
test('ordinary Wasm and unrelated custom-section data remain supported',()=>{
 for(const bytes of [wasmHeader,wasmSection('producer','sourceMappingURL')]) {
  assert.equal(WebAssembly.validate(bytes),true)
  assert.doesNotThrow(()=>inspectAsset('module.wasm',bytes))
 }
})
test('Wasm section parsing rejects truncated and overflowing lengths',()=>{
 for(const suffix of [[0,128],[0,255,255,255,255,127],[0,3,5,97,98],[0,127,1,97]]) {
  assert.throws(()=>inspectAsset('module.wasm',Buffer.concat([wasmHeader,Buffer.from(suffix)])))
 }
})

const inspectPrerender = config => inspectBuildOutputAsset('functions/blog/post.prerender-config.json', Buffer.from(JSON.stringify(config)))
for (const key of ['SourceMap','sOuRcEmAp','X-SourceMap','x-SOURCEMAP']) {
 for (const url of [sourceMap,'/external-source-map']) {
  test(`prerender initialHeaders rejects ${key} ${url === sourceMap ? 'inline' : 'external'}`, () => {
   assert.throws(() => inspectPrerender({expiration:60,initialHeaders:{[key]:url}}), /Source-map response headers/)
  })
 }
}
for (const charset of ['Shift_JIS','iso-2022-jp','utf-16','windows-1252']) {
 test(`prerender headers and fallback reject ${charset}`, () => {
  const contentType=`text/html; charset=${charset}`
  assert.throws(() => inspectPrerender({initialHeaders:{'cOnTeNt-TyPe':contentType}}), /UTF-8 contract/)
  assert.throws(() => inspectPrerender({fallback:{type:'FileFsRef',fsPath:'post.prerender-fallback.html',contentType}}), /UTF-8 contract/)
 })
}
for (const encoding of ['br','gzip','deflate']) {
 test(`prerender initialHeaders rejects ${encoding} content encoding`, () => {
  assert.throws(() => inspectPrerender({initialHeaders:{'CoNtEnT-EnCoDiNg':encoding}}), /Encoded routes/)
 })
}
test('prerender accepts UTF-8 headers, ordinary metadata and each fallback shape', () => {
 for (const fallback of [undefined,null,'post.prerender-fallback.html',{type:'FileFsRef',fsPath:'post.prerender-fallback.html',contentType:'text/html; charset="UTF-8"'}]) {
  assert.doesNotThrow(() => inspectPrerender({expiration:60,staleExpiration:300,initialStatus:200,initialHeaders:{'Content-Type':'text/html; charset=utf-8','Cache-Control':'public, max-age=60'},fallback,initialMetadata:{compute:'static',htmlSize:123}}))
 }
})
test('prerender rejects malformed header containers, values and line injection', () => {
 for (const initialHeaders of [null,[],['X-Ordinary'],42,'text/plain',{'Content-Type':[]},{'X-Ordinary':false},{'X-Ordinary':'ok\r\nX-SourceMap: /map'},{'bad header':'x'}]) {
  assert.throws(() => inspectPrerender({initialHeaders}))
 }
})
test('routing schemas reject malformed containers instead of skipping policy', () => {
 for (const c of [null,[],{routes:{}},{routes:[null]},{routes:[{headers:[]}]},{routes:[{transforms:{}}]},{overrides:[]},{overrides:{x:null}},{overrides:{x:{contentType:[]}}}]) {
  assert.throws(() => inspectConfig(c))
 }
 for(const c of [null,[],{fallback:[]},{fallback:1},{fallback:{contentType:[]}}]) assert.throws(() => inspectPrerender(c))
})
const ordinaryData = {
 headers:[{column:'recipe title'}],contentType:'Recipe',transforms:'mash',
 initialHeaders:{'X-SourceMap':sourceMap,'Content-Type':'text/html; charset=Shift_JIS'},
 routes:[{headers:{'SourceMap':sourceMap},transforms:[transform('Content-Type','text/css; charset=Shift_JIS')]}],
 overrides:{x:{contentType:'Recipe'}},fallback:{contentType:'Recipe'},
}
for(const name of ['config.json','static/config.json','static/page.prerender-config.json','static/nested/.vercel/output/config.json','functions/api/recipe.func/data.json','functions/api/recipe.func/config.json','functions/api/recipe.func/nested/page.prerender-config.json','.vercel/project.json']) {
 test(`ordinary JSON is not deployment metadata: ${name}`, () => {
  const bytes=Buffer.from(JSON.stringify(ordinaryData))
  assert.doesNotThrow(() => inspectAsset(name,bytes))
  if(name!=='config.json') assert.doesNotThrow(() => inspectBuildOutputAsset(name,bytes))
 })
}
test('only schema-defined fields in real deployment metadata are interpreted', () => {
 assert.doesNotThrow(() => inspectConfig({version:3,flags:ordinaryData,framework:{version:'1',description:ordinaryData},routes:[{src:'/x',has:[{type:'header',key:'Content-Encoding',value:'br'}],metadata:ordinaryData}],overrides:{x:{contentType:'text/plain',metadata:ordinaryData}}}))
 assert.doesNotThrow(() => inspectPrerender({initialMetadata:ordinaryData,experimentalBypassFor:[{type:'header',key:'Content-Encoding',value:'br'}],fallback:{fsPath:'page.html',metadata:ordinaryData}}))
})
test('schema scoping does not disable byte-policy or private-package checks', () => {
 for(const name of ['static/data.json','functions/api/recipe.func/data.json']) {
  assert.throws(() => inspectBuildOutputAsset(name,Buffer.from(JSON.stringify({note:'sourceMappingURL=/map'}))), /Source-map directive/)
  assert.throws(() => inspectBuildOutputAsset(name,Buffer.from(JSON.stringify({name:'@masra91/framebriar'}))), /Raw private SDK/)
 }
})


for(const prefix of ['', 'services/web/', 'services/api/']) {
 for(const [label, relative, value, reason] of [
  ['route header','config.json',{routes:[{headers:{'X-SourceMap':sourceMap}}]},/Source-map response headers/],
  ['route transform','config.json',config([transform('Content-Type','text/html; charset=Shift_JIS')]),/UTF-8 contract/],
  ['override','config.json',{overrides:{x:{contentType:'text/html; charset=Shift_JIS'}}},/UTF-8 contract/],
  ['prerender header','functions/nested/page.prerender-config.json',{initialHeaders:{SourceMap:sourceMap}},/Source-map response headers/],
  ['fallback media type','functions/nested/page.prerender-config.json',{fallback:{fsPath:'page.html',contentType:'text/html; charset=Shift_JIS'}},/UTF-8 contract/],
 ]) {
  test(`metadata scope ${prefix || 'root/'}${label} is enforced`,()=>{
   assert.throws(()=>inspectBuildOutputAsset(prefix+relative,Buffer.from(JSON.stringify(value))),reason)
  })
 }
 test(`metadata scope ${prefix || 'root/'} supports safe metadata and ignores static/function data fields`,()=>{
  assert.doesNotThrow(()=>inspectBuildOutputAsset(prefix+'config.json',Buffer.from(JSON.stringify({version:3,routes:[{headers:{'Content-Type':'text/html; charset=utf-8'}}],flags:ordinaryData}))))
  assert.doesNotThrow(()=>inspectBuildOutputAsset(prefix+'functions/page.prerender-config.json',Buffer.from(JSON.stringify({initialHeaders:{'Content-Type':'text/html'},fallback:{contentType:'text/html; charset=utf-8',fsPath:'page.html'}}))))
  for(const relative of ['static/config.json','static/services/web/config.json','static/data.prerender-config.json','functions/page.func/config.json','functions/page.func/data.prerender-config.json']) {
   assert.doesNotThrow(()=>inspectBuildOutputAsset(prefix+relative,Buffer.from(JSON.stringify(ordinaryData))))
  }
 })
}


test('nested service layouts and declarations fail explicitly',()=>{
 for(const key of ['services','experimentalServices','experimentalServicesV2']) {
  assert.throws(()=>inspectBuildOutputAsset('services/web/config.json',Buffer.from(JSON.stringify({version:3,[key]:[]}))),/Nested service declarations/)
 }
 assert.throws(()=>inspectBuildOutputAsset('services/web/services/api/config.json',Buffer.from('{}')),/Nested service output/)
 for(const name of ['services/web/static/services/api/config.json','services/web/functions/x.func/services/api/config.json']) {
  assert.doesNotThrow(()=>inspectBuildOutputAsset(name,Buffer.from(JSON.stringify(ordinaryData))))
 }
})
test('cache predicates and outbound prerender headers are not response assignments',()=>{
 assert.doesNotThrow(()=>inspectConfig({version:3,routes:[{src:'/x',has:[{type:'header',key:'Content-Encoding',value:'br'}]}],afterCacheBypass:[{hasAll:[{key:'SourceMap',value:sourceMap}]}]}))
 assert.doesNotThrow(()=>inspectPrerender({chain:{headers:{'Content-Type':'text/html; charset=Shift_JIS',SourceMap:sourceMap}},experimentalBypassFor:[{type:'header',key:'Content-Encoding',value:'br'}]}))
})

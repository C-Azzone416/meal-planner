import {test} from 'node:test'
import assert from 'node:assert/strict'
import {inspectAsset} from './artifact-tools/asset-policy.mjs'

const inspectConfig = config => inspectAsset('config.json', Buffer.from(JSON.stringify(config)))
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
  test(`response header pairs reject ${key}: ${value === sourceMap ? 'inline' : 'external'}`, () => {
   assert.throws(() => inspectConfig({headers:[{source:'/app.css',headers:[{key,value}]}]}))
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

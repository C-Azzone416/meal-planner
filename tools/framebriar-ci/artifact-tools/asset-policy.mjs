import assert from 'node:assert/strict'

// UTF-16/32 encode this ASCII marker with NULs between characters in either
// byte order. Match it at any alignment, even if a route renames a no-BOM file.
const wideSourceMap = new RegExp([...'sourceMappingURL'].join('\\x00{1,3}'), 'i')
// ISO-2022-JP is ASCII-byte-valid UTF-8 but its shifts change browser decoding.
// Decode suspicious opaque bytes for map detection; a shift alone may be an
// ordinary instruction or debug-section byte sequence in a native executable.
const statefulShift = /\x1b(?:\([BJI]|\$[@B]|\$\(D)/
const textExtension = /\.(?:[cm]?js|css|json|html?|svg|xml|txt|webmanifest)$/i
const markupExtension = /\.(?:html?|svg|xml)$/i
const contentType = /^[!#$%&'*+.^_`|~0-9a-z-]+\/[!#$%&'*+.^_`|~0-9a-z-]+(?:[ \t]*;[ \t]*charset[ \t]*=[ \t]*(?:utf-8|"utf-8"))?[ \t]*$/i

// Precompressed assets are not part of the reviewed build contract. Reject them
// rather than scanning compressed bytes as UTF-8 (or expanding attacker-sized data).
export function inspectAsset(name, bytes) {
  assert.ok(!/\.(?:map|gz|gzip|br|tar|tgz|zip|bz2|xz|zst|zstd|deflate|zz|7z|rar)$/i.test(name), 'Source maps and compressed assets must not be exported')
  const signatures = ['1f8b', '504b0304', '504b0506', '504b0708', '425a68', 'fd377a585a00', '28b52ffd', '377abcaf271c', '526172211a07']
  assert.ok(!signatures.some(hex => bytes.subarray(0, hex.length / 2).equals(Buffer.from(hex, 'hex'))), 'Compressed asset signature in artifact')
  // RFC 1950 zlib-wrapped deflate has a method/window byte and mod-31 header.
  assert.ok(!(bytes.length >= 2 && (bytes[0] & 15) === 8 && (bytes[0] >> 4) <= 7 && ((bytes[0] << 8) + bytes[1]) % 31 === 0), 'Deflate asset in artifact')
  assert.ok(bytes.subarray(257,262).toString('ascii') !== 'ustar', 'Archive container in artifact')
  if (bytes.subarray(0,4).equals(Buffer.from([0,97,115,109]))) inspectWasm(bytes)
  // The output contract supports UTF-8 text only. CSS honors UTF-16 BOMs,
  // so scanning those bytes as UTF-8 would miss otherwise readable source maps.
  // Reject Unicode-wide BOMs even on renamed/extensionless files and require
  // strict UTF-8 without NULs for textual assets. Binary Go/image/font files
  // still use the byte-oriented marker scan below.
  const wideBoms = ['fffe', 'feff', '0000feff']
  assert.ok(!wideBoms.some(hex => bytes.subarray(0, hex.length / 2).equals(Buffer.from(hex, 'hex'))), 'Artifact text must be UTF-8')
  const byteText = bytes.toString('latin1')
  assert.ok(!wideSourceMap.test(byteText), 'Encoded source-map marker in artifact')
  if (statefulShift.test(byteText)) {
    inspectMarkers(new TextDecoder('iso-2022-jp').decode(bytes))
  }
  let text = bytes.toString('utf8')
  let readableText = false
  try {
    const decoded = new TextDecoder('utf-8', {fatal: true}).decode(bytes)
    readableText = !/[\x00-\x08\x0b\x0e-\x1f\x7f]/.test(decoded)
  } catch { /* Native binaries need not be UTF-8. */ }
  if (textExtension.test(name)) {
    text = new TextDecoder('utf-8', {fatal: true}).decode(bytes)
    // HT, LF, FF and CR are normal source whitespace. Other ASCII controls,
    // notably ESC/SO/SI, are not supported in served text.
    assert.ok(!/[\x00-\x08\x0b\x0e-\x1f\x7f]/.test(text), 'Text artifacts contain unsupported control bytes')
  }
  if (textExtension.test(name) || readableText) inspectEncodingDeclarations(name, text)
  inspectMarkers(text)
  if (/\.json$/i.test(name)) {
    const value = JSON.parse(text)
    assert.ok(value?.name !== '@masra91/framebriar', 'Raw private SDK package manifest in artifact')
  }
  return text
}

function inspectMarkers(text) {
  assert.ok(!/sourceMappingURL\s*=/i.test(text), 'Source-map directive in artifact')
  assert.ok(!/FRAMEBRIAR_FAKE_SERVICE|FRAMEBRIAR_TEST_SECRET|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|-----BEGIN (?:RSA |EC )?PRIVATE KEY-----/.test(text), 'Forbidden credential or fixture marker in artifact')
}

// Deployment semantics are path- and schema-specific. The ordinary asset
// scanner must not treat application data named headers/contentType as config.
// The caller supplies a path relative to .vercel/output, never just a basename.
export function inspectBuildOutputAsset(outputPath, bytes) {
  const text = inspectAsset(outputPath, bytes)
  let parts = outputPath.split('/')
  // Vercel service outputs contain one standard Build Output subtree each.
  // Strip exactly this reserved prefix, never a similarly named static asset
  // or a path inside a function bundle. Services cannot nest.
  const serviceScope = parts[0] === 'services' && parts.length > 2
  if (serviceScope) {
    parts = parts.slice(2)
    assert.notEqual(parts[0], 'services', 'Nested service output is unsupported')
  }
  if (parts.length === 1 && parts[0] === 'config.json') {
    const config = JSON.parse(text)
    if (serviceScope) {
      object(config, 'Service config')
      for (const key of ['services', 'experimentalServices', 'experimentalServicesV2']) {
        assert.ok(!Object.hasOwn(config, key), 'Nested service declarations are unsupported')
      }
    }
    inspectOutputConfig(config)
  }
  else {
    // Vercel stops directory traversal at each .func bundle. JSON files inside
    // that bundle are function data, not sibling prerender declarations.
    if (parts[0] === 'functions' && parts.at(-1).endsWith('.prerender-config.json') &&
        !parts.slice(1, -1).some(part => part.endsWith('.func'))) {
      inspectPrerenderConfig(JSON.parse(text))
    }
  }
  return text
}

function object(value, label) {
  assert.ok(value && typeof value === 'object' && !Array.isArray(value), `${label} must be an object`)
}

// Only these schema-defined properties affect response headers. Do not recurse
// into unrelated flags, request predicates, metadata, or arbitrary JSON values.
// https://vercel.com/docs/build-output-api/configuration
function inspectOutputConfig(config) {
  object(config, 'Build Output config')
  if (Object.hasOwn(config, 'routes')) {
    assert.ok(Array.isArray(config.routes), 'Routes must be an array')
    for (const route of config.routes) {
      object(route, 'Route')
      if (Object.hasOwn(route, 'headers')) inspectHeaderMap(route.headers)
      if (Object.hasOwn(route, 'transforms')) {
        assert.ok(Array.isArray(route.transforms), 'Transforms must be an array')
        for (const transform of route.transforms) inspectTransform(transform)
      }
    }
  }
  if (Object.hasOwn(config, 'overrides')) {
    object(config.overrides, 'Overrides')
    for (const override of Object.values(config.overrides)) {
      object(override, 'Override')
      if (Object.hasOwn(override, 'contentType')) inspectContentType(override.contentType)
    }
  }
}

// Prerender metadata is separate from .vc-config.json and config.json.
// initialHeaders supplies cached response headers; serialized fallback objects
// can also select a media type. String fallback references introduce no header.
function inspectPrerenderConfig(config) {
  object(config, 'Prerender config')
  if (Object.hasOwn(config, 'initialHeaders')) inspectHeaderMap(config.initialHeaders)
  if (config.fallback !== undefined && config.fallback !== null && typeof config.fallback !== 'string') {
    object(config.fallback, 'Prerender fallback')
    if (Object.hasOwn(config.fallback, 'contentType')) inspectContentType(config.fallback.contentType)
  }
}

function inspectHeaderMap(headers) {
  object(headers, 'Response headers')
  for (const [key, value] of Object.entries(headers)) inspectHeader(key, value)
}

function inspectHeader(key, value) {
  assert.ok(typeof key === 'string' && key.length > 0 && !/[^!#$%&'*+.^_`|~0-9a-z-]/i.test(key), 'Response header name must be a fixed token')
  assert.ok(typeof value === 'string' && !/[\x00-\x08\x0a-\x1f\x7f]/.test(value), 'Response header value must be a string without controls')
  const name = key.toLowerCase()
  assert.ok(!['sourcemap', 'x-sourcemap'].includes(name), 'Source-map response headers must not be exported')
  // Brotli/raw deflate have no reliable magic. Block advertised encodings too.
  assert.notEqual(name, 'content-encoding', 'Encoded routes must not be exported')
  if (name === 'content-type') inspectContentType(value)
}

function inspectTransform(transform) {
  assert.ok(transform && typeof transform === 'object', 'Invalid transform')
  if (transform.type !== 'response.headers') return
  assert.ok(['set', 'append', 'delete'].includes(transform.op), 'Unsupported response-header operation')
  // Deletion does not introduce a value or alter text decoding. Vercel permits
  // selector objects here, including broad prefix/suffix matches.
  if (transform.op === 'delete') return
  const key = transform.target?.key
  let names
  if (typeof key === 'string') names = [key]
  else if (key && Object.keys(key).length === 1 && typeof key.eq === 'string') names = [key.eq]
  else if (key && Object.keys(key).length === 1 && Array.isArray(key.inc)) names = key.inc
  else throw new Error('Mutating response headers require a fixed or finite target')
  assert.ok(names.length > 0, 'Missing response-header target')
  const values = Array.isArray(transform.args) ? transform.args : [transform.args]
  assert.ok(values.length > 0, 'Missing response-header args')
  for (const name of names) {
    if (typeof name === 'string' && name.toLowerCase() === 'content-type') {
      assert.equal(values.length, 1, 'Content-Type must have one value')
    }
    for (const value of values) inspectHeader(name, value)
  }
}

function inspectContentType(value) {
  assert.ok(typeof value === 'string' && !/[^\t\x20-\x7e]/.test(value) && contentType.test(value), 'Routing Content-Type must use the UTF-8 contract')
}

function inspectEncodingDeclarations(name, text) {
  // CSS's byte-level @charset is honored before parsing and also works for
  // extensionless rewrites. Do not let it select a decoder unlike our scanner.
  for (const match of text.matchAll(/@charset\s+(?:"([^"]*)"|'([^']*)'|([^\s;]+))/gi)) {
    assertUtf8(match[1] ?? match[2] ?? match[3])
  }
  if (!markupExtension.test(name)) return
  // Covers HTML meta/script/link charset, legacy meta Content-Type parameters,
  // and XML declarations. Conservatively reject encoded/ambiguous labels too.
  for (const match of text.matchAll(/\b(?:charset|encoding)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s;>"']+))/gi)) {
    assertUtf8(match[1] ?? match[2] ?? match[3])
  }
}

function assertUtf8(label) {
  const normalized = label.replace(/^[\t\n\f\r ]+|[\t\n\f\r ]+$/g, '').toLowerCase()
  assert.equal(normalized, 'utf-8', 'Text declarations must specify UTF-8')
}

// ECMA-426 also links Wasm maps via a custom section, with no '=' delimiter.
// Parse bounded section/name lengths without compiling or executing the module;
// do not reject ordinary data strings that happen to mention the section name.
function inspectWasm(bytes) {
  assert.ok(bytes.subarray(4,8).equals(Buffer.from([1,0,0,0])), 'Unsupported Wasm version')
  function u32(start, limit) {
    let value = 0
    for (let i = 0; i < 5; i++) {
      assert.ok(start + i < limit, 'Truncated Wasm length')
      const byte = bytes[start + i]
      assert.ok(i < 4 || byte <= 15, 'Invalid Wasm length')
      value += (byte & 127) * 2 ** (7 * i)
      if (byte < 128) return {value, next:start + i + 1}
    }
    throw new Error('Invalid Wasm length')
  }
  let offset = 8
  while (offset < bytes.length) {
    const id = bytes[offset++]
    const size = u32(offset, bytes.length)
    const end = size.next + size.value
    assert.ok(end <= bytes.length, 'Truncated Wasm section')
    if (id === 0) {
      const name = u32(size.next, end)
      assert.ok(name.next + name.value <= end, 'Truncated Wasm section name')
      const label = new TextDecoder('utf-8', {fatal:true}).decode(bytes.subarray(name.next, name.next + name.value))
      assert.notEqual(label.toLowerCase(), 'sourcemappingurl', 'Wasm source-map section must not be exported')
    }
    offset = end
  }
}

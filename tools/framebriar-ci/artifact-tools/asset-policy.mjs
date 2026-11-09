import assert from 'node:assert/strict'

// UTF-16/32 encode this ASCII marker with NULs between characters in either
// byte order. Match it at any alignment, even if a route renames a no-BOM file.
const wideSourceMap = new RegExp([...'sourceMappingURL'].join('\\x00{1,3}'), 'i')
// ISO-2022-JP is ASCII-byte-valid UTF-8 but its shifts change browser decoding.
// Reject those shifts regardless of extension, including rewritten opaque assets.
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
  // The output contract supports UTF-8 text only. CSS honors UTF-16 BOMs,
  // so scanning those bytes as UTF-8 would miss otherwise readable source maps.
  // Reject Unicode-wide BOMs even on renamed/extensionless files and require
  // strict UTF-8 without NULs for textual assets. Binary Go/image/font files
  // still use the byte-oriented marker scan below.
  const wideBoms = ['fffe', 'feff', '0000feff']
  assert.ok(!wideBoms.some(hex => bytes.subarray(0, hex.length / 2).equals(Buffer.from(hex, 'hex'))), 'Artifact text must be UTF-8')
  const byteText = bytes.toString('latin1')
  assert.ok(!wideSourceMap.test(byteText), 'Encoded source-map marker in artifact')
  assert.ok(!statefulShift.test(byteText), 'Stateful text encoding must not be exported')
  let text = bytes.toString('utf8')
  if (textExtension.test(name)) {
    text = new TextDecoder('utf-8', {fatal: true}).decode(bytes)
    // HT, LF, FF and CR are normal source whitespace. Other ASCII controls,
    // notably ESC/SO/SI, are not supported in served text.
    assert.ok(!/[\x00-\x08\x0b\x0e-\x1f\x7f]/.test(text), 'Text artifacts contain unsupported control bytes')
  }
  inspectEncodingDeclarations(name, text)
  assert.ok(!/sourceMappingURL\s*=/i.test(text), 'Source-map directive in artifact')
  assert.ok(!/FRAMEBRIAR_FAKE_SERVICE|FRAMEBRIAR_TEST_SECRET|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|-----BEGIN (?:RSA |EC )?PRIVATE KEY-----/.test(text), 'Forbidden credential or fixture marker in artifact')
  if (/\.json$/i.test(name)) {
    const value = JSON.parse(text)
    assert.ok(value?.name !== '@masra91/framebriar', 'Raw private SDK package manifest in artifact')
    inspectHeaders(value)
  }
  return text
}

// Brotli and raw deflate have no reliable magic. Reject any advertised encoding
// as well as their conventional extensions, including rewritten/extensionless routes.
function inspectHeaders(value) {
  if (!value || typeof value !== 'object') return
  // Vercel's header maps, {key,value} entries and static contentType overrides
  // obey the same contract: a bare media type or one explicit UTF-8 charset;
  // other parameters, duplicate charsets and control bytes fail closed.
  if (typeof value.key === 'string' && value.key.toLowerCase() === 'content-type') inspectContentType(value.value)
  for (const [key, child] of Object.entries(value)) {
    if (['content-type', 'contenttype'].includes(key.toLowerCase())) inspectContentType(child)
    assert.ok(key.toLowerCase() !== 'content-encoding', 'Encoded routes must not be exported')
    assert.ok(!(key.toLowerCase() === 'key' && typeof child === 'string' && child.toLowerCase() === 'content-encoding'), 'Encoded routes must not be exported')
    inspectHeaders(child)
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

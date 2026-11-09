import assert from 'node:assert/strict'

// UTF-16/32 encode this ASCII marker with NULs between characters in either
// byte order. Match it at any alignment, even if a route renames a no-BOM file.
const wideSourceMap = new RegExp([...'sourceMappingURL'].join('\\x00{1,3}'), 'i')

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
  assert.ok(!wideSourceMap.test(bytes.toString('latin1')), 'Encoded source-map marker in artifact')
  let text = bytes.toString('utf8')
  if (/\.(?:[cm]?js|css|json|html?|svg|xml|txt|webmanifest)$/i.test(name)) {
    text = new TextDecoder('utf-8', {fatal: true}).decode(bytes)
    assert.ok(!text.includes('\0'), 'Text artifacts must not contain NUL bytes')
  }
  assert.ok(!/sourceMappingURL\s*=/i.test(text), 'Source-map directive in artifact')
  assert.ok(!/FRAMEBRIAR_FAKE_SERVICE|FRAMEBRIAR_TEST_SECRET|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|-----BEGIN (?:RSA |EC )?PRIVATE KEY-----/.test(text), 'Forbidden credential or fixture marker in artifact')
  if (/\.json$/i.test(name)) {
    const value = JSON.parse(text)
    assert.ok(value?.name !== '@masra91/framebriar', 'Raw private SDK package manifest in artifact')
    rejectContentEncoding(value)
  }
  return text
}

// Brotli and raw deflate have no reliable magic. Reject any advertised encoding
// as well as their conventional extensions, including rewritten/extensionless routes.
function rejectContentEncoding(value) {
  if (!value || typeof value !== 'object') return
  for (const [key, child] of Object.entries(value)) {
    assert.ok(key.toLowerCase() !== 'content-encoding', 'Encoded routes must not be exported')
    assert.ok(!(key.toLowerCase() === 'key' && typeof child === 'string' && child.toLowerCase() === 'content-encoding'), 'Encoded routes must not be exported')
    rejectContentEncoding(child)
  }
}

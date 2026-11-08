import assert from 'node:assert/strict'

// Precompressed assets are not part of the reviewed build contract. Reject them
// rather than scanning compressed bytes as UTF-8 (or expanding attacker-sized data).
export function inspectAsset(name, bytes) {
  assert.ok(!/\.(?:map|gz|gzip|br|tar|tgz|zip|bz2|xz|zst|zstd|deflate|zz|7z|rar)$/i.test(name), 'Source maps and compressed assets must not be exported')
  const signatures = ['1f8b', '504b0304', '504b0506', '504b0708', '425a68', 'fd377a585a00', '28b52ffd', '377abcaf271c', '526172211a07']
  assert.ok(!signatures.some(hex => bytes.subarray(0, hex.length / 2).equals(Buffer.from(hex, 'hex'))), 'Compressed asset signature in artifact')
  // RFC 1950 zlib-wrapped deflate has a method/window byte and mod-31 header.
  assert.ok(!(bytes.length >= 2 && (bytes[0] & 15) === 8 && (bytes[0] >> 4) <= 7 && ((bytes[0] << 8) + bytes[1]) % 31 === 0), 'Deflate asset in artifact')
  assert.ok(bytes.subarray(257,262).toString('ascii') !== 'ustar', 'Archive container in artifact')
  const text = bytes.toString('utf8')
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

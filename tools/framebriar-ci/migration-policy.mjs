import assert from 'node:assert/strict'
// No database access and no migration-ready override. Only an immutable, complete
// Git tree with no migration/schema sources can pass this interim policy.
export function migrationReadiness(tree, sourceSha, treeSha) {
  assert.match(sourceSha ?? '', /^[a-f0-9]{40}$/)
  assert.match(treeSha ?? '', /^[a-f0-9]{40}$/)
  assert.equal(tree.truncated, false, 'Incomplete source inventory')
  assert.ok(Array.isArray(tree.tree))
  const seen = new Set()
  for (const item of tree.tree) {
    assert.ok(typeof item.path === 'string' && item.path.length > 0)
    assert.ok(!seen.has(item.path), 'Duplicate source path'); seen.add(item.path)
    assert.ok(!item.path.startsWith('/') && !item.path.split('/').some(p => !p || p === '.' || p === '..'))
    // Symlinks and gitlinks could hide sources outside this immutable inventory.
    assert.ok((item.type === 'tree' && item.mode === '040000') ||
      (item.type === 'blob' && ['100644', '100755'].includes(item.mode)), 'Opaque source entry')
    assert.match(item.sha ?? '', /^[a-f0-9]{40}$/)
    assert.ok(!/(^|\/)(migrations?|migrate)(\/|$)|\.sql$|(^|\/)schema\.prisma$/i.test(item.path),
      'Migration readiness is unverified; no migration-ready bypass is permitted')
  }
  return {source_sha: sourceSha, tree_sha: treeSha, policy: 'no-migration-sources-v1'}
}
export function validateReadiness(value, sourceSha) {
  assert.deepEqual(Object.keys(value).sort(), ['policy', 'source_sha', 'tree_sha'])
  assert.equal(value.source_sha, sourceSha)
  assert.match(value.tree_sha ?? '', /^[a-f0-9]{40}$/)
  assert.equal(value.policy, 'no-migration-sources-v1')
}

import fs from 'node:fs'
import { execFileSync } from 'node:child_process'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

const sdk = '@masra91/framebriar'
const groups = ['dependencies', 'devDependencies', 'optionalDependencies', 'peerDependencies']
function object(value, label) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(`Invalid ${label}`)
  return value
}
function declarations(value, label) {
  let found = false
  for (const group of groups) {
    if (!Object.hasOwn(value, group)) continue
    const entries = object(value[group], `${label}.${group}`)
    for (const [name, version] of Object.entries(entries)) {
      if (typeof version !== 'string' || !version.trim()) throw new Error(`Invalid ${label}.${group} version`)
      if (name === sdk || version.includes(sdk)) found = true
    }
  }
  return found
}

// Validate all entries before deciding. Missing or malformed data must never
// select the public-install fallback, even when another entry names the SDK.
export function classifyDependency(packageText, lockText) {
  const pkg = object(JSON.parse(packageText), 'package')
  const lock = object(JSON.parse(lockText), 'lock')
  if (![2, 3].includes(lock.lockfileVersion)) throw new Error('Unsupported lockfile version')
  const packages = object(lock.packages, 'lock.packages')
  if (!Object.hasOwn(packages, '')) throw new Error('Missing lock root package')
  let found = declarations(pkg, 'package')
  for (const [name, entry] of Object.entries(packages)) {
    object(entry, `lock.packages.${name}`)
    if (declarations(entry, `lock.packages.${name}`)) found = true
    if (name === `node_modules/${sdk}` || name.endsWith(`/node_modules/${sdk}`)) found = true
    for (const field of ['name', 'resolved', 'version']) {
      if (!Object.hasOwn(entry, field)) continue
      if (typeof entry[field] !== 'string') throw new Error(`Invalid lock package ${field}`)
      if (entry[field].includes(sdk) || entry[field].includes(encodeURIComponent(sdk))) found = true
    }
  }
  function legacyDependencies(entries) {
    object(entries, 'lock.dependencies')
    for (const [name, entry] of Object.entries(entries)) {
      object(entry, 'legacy dependency')
      if (name === sdk) found = true
      for (const field of ['version', 'resolved']) {
        if (!Object.hasOwn(entry, field)) continue
        if (typeof entry[field] !== 'string') throw new Error(`Invalid legacy ${field}`)
        if (entry[field].includes(sdk)) found = true
      }
      if (Object.hasOwn(entry, 'dependencies')) legacyDependencies(entry.dependencies)
    }
  }
  if (Object.hasOwn(lock, 'dependencies')) legacyDependencies(lock.dependencies)
  return found
}

// CI and Preview must classify the same committed paths, not follow a checkout
// symlink or silently read untracked/replaced manifests. Keep the entire path
// policy here, alongside dependency parsing, so the workflows cannot diverge.
export function classifyCheckout(root) {
  const git = args => execFileSync('git', ['-C', root, ...args], {maxBuffer: 8 * 1024 * 1024})
  if (fs.realpathSync(root) !== fs.realpathSync(git(['rev-parse', '--show-toplevel']).toString('utf8').trim())) {
    throw new Error('Manifest root must be the repository root')
  }
  if (!fs.lstatSync(path.join(root, 'web')).isDirectory()) throw new Error('web must be a regular directory')
  const manifests = ['package.json', 'package-lock.json'].map(file => {
    const relative = `web/${file}`
    const entry = git(['ls-tree', '-z', 'HEAD', '--', relative]).toString('utf8')
    const match = /^100644 blob ([0-9a-f]{40})\t([^\0]+)\0$/.exec(entry)
    if (!match || match[2] !== relative) {
      throw new Error('Manifest must be a committed regular non-executable file')
    }
    const local = path.join(root, relative)
    if (!fs.lstatSync(local).isFile()) throw new Error('Manifest must be a regular file')
    const committed = git(['cat-file', 'blob', match[1]])
    if (!fs.readFileSync(local).equals(committed)) throw new Error('Manifest differs from committed source')
    return committed.toString('utf8')
  })
  return classifyDependency(...manifests)
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const result = classifyCheckout(process.argv[2] || '.')
  fs.appendFileSync(process.env.GITHUB_OUTPUT, `private=${result}\n`)
}

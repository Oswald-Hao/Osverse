import { createHash } from 'node:crypto'
import { readFileSync, readdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const directory = fileURLToPath(new URL('../src/assets/tool-icons/', import.meta.url))
const expected = new Map([
  ['cc-switch.png', '8e24e5968cac75da1698ae5c2cf2d5f542625bc9d2060b9a052f0348f9cf8795'],
  ['cockpit-tools.png', 'adf31f26511a09fd60f76e099fd13e4761880138bb942153046c2ce261a9071c'],
])

const names = readdirSync(directory).sort()
if (names.join('\n') !== [...expected.keys()].sort().join('\n')) {
  throw new Error(`unexpected tool icon asset set: ${names.join(', ')}`)
}

for (const [name, digest] of expected) {
  const actual = createHash('sha256').update(readFileSync(new URL(`../src/assets/tool-icons/${name}`, import.meta.url))).digest('hex')
  if (actual !== digest) throw new Error(`${name} SHA-256 mismatch: ${actual}`)
}

console.log(`tool icon assets PASS: ${expected.size} byte-exact upstream files`)

import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'
import { test } from 'node:test'

test('explicit migration constraint identifiers fit PostgreSQL name storage', async () => {
  const directory = new URL('../migrations/', import.meta.url)
  const violations = []
  for (const filename of (await readdir(directory)).filter(name => name.endsWith('.sql'))) {
    const sql = await readFile(new URL(filename, directory), 'utf8')
    for (const [, name] of sql.matchAll(/\bconstraint\s+([a-zA-Z_][a-zA-Z_0-9]*)/gi)) {
      if (Buffer.byteLength(name) > 63) violations.push(`${filename}: ${name}`)
    }
  }
  assert.deepEqual(violations, [], 'constraint names must not be silently truncated')
})

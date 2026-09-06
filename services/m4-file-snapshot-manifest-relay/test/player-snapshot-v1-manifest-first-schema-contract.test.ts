import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'

const migrationPath = fileURLToPath(new URL('../../../supabase/migrations/20260914000000_m4_file_snapshot_manifest.sql', import.meta.url))
const e2ePath = fileURLToPath(new URL('./player-snapshot-v1-manifest-first-pg17-e2e.ts', import.meta.url))

const [migration, e2e] = await Promise.all([
  readFile(migrationPath, 'utf8'),
  readFile(e2ePath, 'utf8'),
])

assert.match(migration, /receipt_request_sha256 text not null/)
assert.match(migration, /file_post_sha256 text not null/)
assert.match(e2e, /m\.receipt_request_sha256 = a\.receipt_request_sha256/)
assert.match(e2e, /m\.file_post_sha256 = a\.source_post_sha256/)
assert.doesNotMatch(e2e, /m\.(?:request_sha256|post_sha256) = a\.(?:receipt_request_sha256|source_post_sha256)/)

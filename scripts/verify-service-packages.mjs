// Read-only packaging smoke: no database connections or CLI execution.
import assert from 'node:assert/strict';
import { readFile, access } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

const modules = new Map([
  ['@muhan/onboarding-reconciler', 'reconciler.js'],
  ['@muhan/character-inventory-importer', 'postgres-store.js'],
  ['@muhan/m4-file-snapshot-manifest-relay', 'store.js'],
]);
assert.ok(process.argv.length > 2, 'provide one or more deployed package directories');
for (const directory of process.argv.slice(2)) {
  const manifest = resolve(directory, 'package.json');
  const pkg = JSON.parse(await readFile(manifest, 'utf8'));
  assert.ok(modules.has(pkg.name), 'unsupported service package');
  const runtime = resolve(directory, 'dist', modules.get(pkg.name));
  // Resolve relative to the compiled file, as production does, not this script.
  assert.equal(typeof createRequire(runtime)('pg').Pool, 'function');
  await import(pathToFileURL(runtime).href);
  for (const command of Object.values(pkg.scripts ?? {})) {
    const match = /^node (dist\/[\w.-]+\.js)$/.exec(command);
    if (match) await access(resolve(directory, match[1]));
  }
  console.log(`${pkg.name}: compiled module, pg and declared runtime CLIs present`);
}

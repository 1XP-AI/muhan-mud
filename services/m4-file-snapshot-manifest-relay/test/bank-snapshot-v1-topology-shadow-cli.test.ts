import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { test } from 'node:test'
import {
  main as topologyShadowMain,
  type BankSnapshotV1TopologyShadowCliDependencies,
} from '../src/bank-snapshot-v1-topology-shadow-cli.js'

const databaseUrl = 'postgresql://mud_writer_login@localhost/postgres'

function environment(): NodeJS.ProcessEnv {
  return {
    M4_BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_ENABLED: 'true',
    M4_BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_OUTBOX_DIR: '/secure/outbox',
    M4_BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_DATABASE_URL: databaseUrl,
  }
}

function dependencies(calls: string[], output: string[], result = {
  visited: 2, valid: 2, delivered: 2, recorded: 1, exactRetry: 1,
  invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0,
}): BankSnapshotV1TopologyShadowCliDependencies {
  return {
    createStore: (url) => {
      calls.push(`store:${url}`)
      return {
        recordBankSnapshotV1TopologyShadow: async () => 'RECORDED' as const,
        close: async () => { calls.push('close') },
      }
    },
    relay: async (path, store, filesystem) => {
      calls.push(`relay:${path}:${store ? 'store' : 'missing'}:${filesystem === undefined ? 'missing' : 'node'}`)
      return result
    },
    writeStdout: (line) => { output.push(line) },
  }
}

test('bank topology shadow CLI requires exact --once plus its dedicated opt-in and narrow configuration before writer construction', async () => {
  for (const [args, env] of [
    [[], environment()],
    [['--once', '--again'], environment()],
    [['--once'], { ...environment(), M4_BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_ENABLED: undefined }],
    [['--once'], { ...environment(), M4_BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_ENABLED: '1' }],
    [['--once'], { ...environment(), M4_BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_OUTBOX_DIR: 'relative' }],
    [['--once'], { ...environment(), M4_BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_OUTBOX_DIR: '/secure\0outbox' }],
    [['--once'], { ...environment(), M4_BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_DATABASE_URL: undefined }],
  ] as const) {
    const calls: string[] = []; const output: string[] = []
    await assert.rejects(() => topologyShadowMain(env, args, dependencies(calls, output)))
    assert.deepEqual(calls, [])
    assert.deepEqual(output, [])
  }
})

test('bank topology shadow CLI invokes only the existing one-shot relay, reports its aggregate safe result, and closes its store', async () => {
  const calls: string[] = []; const output: string[] = []
  assert.equal(await topologyShadowMain(environment(), ['--once'], dependencies(calls, output)), 0)
  assert.deepEqual(calls, [
    `store:${databaseUrl}`,
    'relay:/secure/outbox:store:node',
    'close',
  ])
  assert.deepEqual(output, [
    '{"visited":2,"valid":2,"delivered":2,"recorded":1,"exactRetry":1,"invalid":0,"conflict":0,"retryable":0,"unknown":0,"ioError":0}\n',
  ])
})

test('bank topology shadow CLI exits nonzero for every unsafe aggregate outcome while still producing one closed result', async () => {
  for (const unsafe of ['invalid', 'conflict', 'retryable', 'unknown', 'ioError'] as const) {
    const calls: string[] = []; const output: string[] = []
    assert.equal(await topologyShadowMain(environment(), ['--once'], dependencies(calls, output, {
      visited: 1, valid: 0, delivered: 0, recorded: 0, exactRetry: 0,
      invalid: unsafe === 'invalid' ? 1 : 0, conflict: unsafe === 'conflict' ? 1 : 0,
      retryable: unsafe === 'retryable' ? 1 : 0, unknown: unsafe === 'unknown' ? 1 : 0,
      ioError: unsafe === 'ioError' ? 1 : 0,
    })), 1, unsafe)
    assert.equal(output.length, 1, unsafe)
    assert.equal(output[0]!.split('\n').filter(Boolean).length, 1, unsafe)
    assert.equal(calls.at(-1), 'close', unsafe)
  }
})

test('bank topology CLI is an explicit package entrypoint and remains absent from default relay and image behavior', async () => {
  const [packageJson, relayCli, image] = await Promise.all([
    readFile(new URL('../package.json', import.meta.url), 'utf8'),
    readFile(new URL('../src/cli.ts', import.meta.url), 'utf8'),
    readFile(new URL('../Dockerfile', import.meta.url), 'utf8'),
  ])
  assert.equal(JSON.parse(packageJson).scripts['bank-topology-shadow'], 'node dist/bank-snapshot-v1-topology-shadow-cli.js')
  assert.doesNotMatch(relayCli, /bank-snapshot-v1-topology-shadow-cli/i)
  assert.doesNotMatch(image, /bank-snapshot-v1-topology-shadow-cli/i)
})

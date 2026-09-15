import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'
import fixture from '../../../tests/fixtures/onboarding_protocol_v1.json' with { type: 'json' }
import {
  OnboardingProtocolError,
  OnboardingControlLineParser,
  OnboardingControlDemultiplexer,
  assertLegalOnboardingTransition,
  canTransitionOnboardingState,
  createOnboardingTicket,
  formatOnboardingEvidenceControl,
  onboardingProtocolLimits,
  parseOnboardingAuthFrame,
  sanitizeOnboardingLogMetadata,
} from '../src/onboarding-protocol.js'
import { decodeLegacyIdentityEvidenceV1, lowerHexToBytes } from '../src/evidence-codec/legacy-identity-evidence-v1.js'

const actor = '11111111-1111-4111-8111-111111111111'
const correlation = '22222222-2222-4222-8222-222222222222'
const token = 'jwt-token-that-must-never-appear-in-an-error'
const testDirectory = fileURLToPath(new URL('.', import.meta.url))
const evidenceFixturePath = resolve(testDirectory, '../../../tests/fixtures/legacy_identity_evidence_wire_v1_ok.hex')

const evidence = {
  outcome: 'ok', canonicalization: 'normalized', canonicalName: 'Alice', legacyShard: '35',
  playerFileSha256: '18f8d2eb4a387bbc1e37ec099a7326805739bc9c99ecf0f14b808a5bcb65bf49', storageFormat: 'player-v1'
} as const

test('parses the strict first onboarding auth frame', () => {
  assert.deepEqual(parseOnboardingAuthFrame(JSON.stringify({
    type: 'onboarding-auth', accessToken: token, mode: 'provision', correlationId: correlation,
  })), { type: 'onboarding-auth', accessToken: token, mode: 'provision', correlationId: correlation })
})

test('rejects unknown keys, noncanonical UUIDs, oversized tokens, and binary first frames', () => {
  const valid = { type: 'onboarding-auth', accessToken: token, mode: 'claim', correlationId: correlation }
  assert.throws(() => parseOnboardingAuthFrame(JSON.stringify({ ...valid, extra: true })), OnboardingProtocolError)
  assert.throws(() => parseOnboardingAuthFrame(JSON.stringify({ ...valid, correlationId: correlation.replace('2', 'A') })), OnboardingProtocolError)
  assert.throws(() => parseOnboardingAuthFrame(JSON.stringify({ ...valid, accessToken: 'x'.repeat(8193) })), OnboardingProtocolError)
  assert.throws(() => parseOnboardingAuthFrame(Buffer.from(JSON.stringify(valid))), OnboardingProtocolError)
  assert.throws(() => parseOnboardingAuthFrame(JSON.stringify({ ...valid, mode: 'other' })), OnboardingProtocolError)
})

test('creates both exact MUD1O fixture tickets with configurable clock, TTL, and nonce', () => {
  for (const vector of fixture.vectors) {
    const ticket = createOnboardingTicket({
      mode: vector.mode === 'P' ? 'provision' : 'claim', userId: vector.userId, correlationId: vector.correlationId,
    }, fixture.syntheticSecret, {
      nowMs: (vector.expiresAt * 1000) - 15000,
      ttlMs: 15000,
      nonce: vector.nonce,
    })
    assert.equal(ticket.toString('ascii'), vector.ticket)
  }
})

test('rejects invalid ticket inputs without reflecting secrets', () => {
  assert.throws(() => createOnboardingTicket({
    mode: 'provision', userId: actor, correlationId: correlation,
  }, token, { nowMs: 1_700_000_000_000, ttlMs: 15_000, nonce: 'bad' }), (error: unknown) => {
    assert.ok(error instanceof OnboardingProtocolError)
    assert.ok(!String(error).includes(token))
    return true
  })
})

test('parses fragmented and coalesced bounded C control lines', () => {
  const parser = new OnboardingControlLineParser()
  assert.deepEqual(parser.push('MUD1O RES'), [])
  assert.deepEqual(parser.push('ERVE|6162\nMUD1O SAVED|11111111-1111-4111-8111-111111111111|'.replace('11111111-1111-4111-8111-111111111111', actor)), [
    { type: 'RESERVE', nameHex: '6162' },
  ])
  assert.deepEqual(parser.push('a'.repeat(64) + '|legacy-v1\nMUD1O COMMIT\n'), [
    { type: 'SAVED', characterId: actor, fileSha256: 'a'.repeat(64), storageFormat: 'legacy-v1' },
    { type: 'COMMIT' },
  ])
  assert.throws(() => parser.push('MUD1O WHAT\n'), OnboardingProtocolError)
  assert.throws(() => parser.push('MUD1O ' + 'x'.repeat(2048)), OnboardingProtocolError)
})

test('parses claim challenge controls and keeps ALLOW private to the C lane', () => {
  const parser = new OnboardingControlLineParser()
  const sha = 'b'.repeat(64)
  assert.deepEqual(parser.push(`MUD1O CHALLENGE|416c696365|${sha}\nMUD1O ALLOW\n`), [
    { type: 'CHALLENGE', nameHex: '416c696365', fileSha256: sha },
    { type: 'ALLOW' },
  ])
  const demux = new OnboardingControlDemultiplexer()
  const result = demux.push(Buffer.from(`MUD1O CHALLENGE|416c696365|${sha}\n`))
  assert.deepEqual(result.controls, [{ type: 'CHALLENGE', nameHex: '416c696365', fileSha256: sha }])
  assert.deepEqual(result.game, [])
})

test('formats and parses the exact feature-gated V1 evidence fixture', async () => {
  const wireHex = (await readFile(evidenceFixturePath, 'utf8')).trim()
  const decoded = decodeLegacyIdentityEvidenceV1(lowerHexToBytes(wireHex))
  const record = Buffer.from(`MUD1O EVIDENCE|1|${wireHex}\n`, 'ascii')
  assert.deepEqual(formatOnboardingEvidenceControl(decoded), record)
  const parser = new OnboardingControlLineParser({ evidenceEnabled: true })
  assert.deepEqual(parser.push(record), [{ type: 'EVIDENCE', version: 1, evidence: decoded }])
  const demultiplexer = new OnboardingControlDemultiplexer({ evidenceEnabled: true })
  assert.deepEqual(demultiplexer.push(record).controls, [{ type: 'EVIDENCE', version: 1, evidence: decoded }])
})

test('accepts a fragmented maximum-sized evidence record and a coalesced ordinary control', () => {
  const maximum = { ...evidence, canonicalName: 'FourteenByteID' }
  const record = formatOnboardingEvidenceControl(maximum)
  assert.equal(record.length, 240)
  const parser = new OnboardingControlLineParser({ evidenceEnabled: true })
  assert.deepEqual(parser.push(record.subarray(0, 19)), [])
  assert.deepEqual(parser.push(Buffer.concat([record.subarray(19), Buffer.from('MUD1O COMMIT\n', 'ascii')])), [
    { type: 'EVIDENCE', version: 1, evidence: maximum }, { type: 'COMMIT' },
  ])
})

test('keeps evidence disabled by default and rejects noncanonical evidence record variants', () => {
  const record = formatOnboardingEvidenceControl(evidence)
  const disabled = new OnboardingControlLineParser()
  assert.throws(() => disabled.push(record), OnboardingProtocolError)

  const text = record.toString('ascii')
  const hexStart = 'MUD1O EVIDENCE|1|'.length
  const replaceFirstHex = (value: string): string => `${text.slice(0, hexStart)}${value}${text.slice(hexStart + 1)}`
  const invalid = [
    text.replace('|1|', '|2|'),
    replaceFirstHex('A'),
    replaceFirstHex('g'),
    `${text.slice(0, -2)}\n`,
    text.replace('|1|', '|1| '),
    `${text.slice(0, -1)}00\n`,
    `${text.slice(0, -1)}\r\n`,
    `${text.slice(0, -1)}\t\n`,
    replaceFirstHex('0'),
  ]
  // The final case crosses the dedicated 240-byte line boundary using a canonical max record.
  invalid.push(`${formatOnboardingEvidenceControl({ ...evidence, canonicalName: 'FourteenByteID' }).toString('ascii').slice(0, -1)}00\n`)
  for (const value of invalid) {
    const parser = new OnboardingControlLineParser({ evidenceEnabled: true })
    assert.throws(() => parser.push(value), OnboardingProtocolError)
  }
  for (const value of [Buffer.from('MUD1O EVIDENCE|1|00\0\n', 'binary'), Buffer.from('MUD1O EVIDENCE|1|00\x01\n', 'binary')]) {
    const parser = new OnboardingControlLineParser({ evidenceEnabled: true })
    assert.throws(() => parser.push(value), OnboardingProtocolError)
  }
})

test('demultiplexes fragmented private controls without leaking them into game bytes', () => {
  const parser = new OnboardingControlDemultiplexer()
  assert.deepEqual(parser.push(Buffer.from('prompt MUD1')).game.map(String), ['prompt '])
  const result = parser.push(Buffer.from(`O SAVED|${actor}|${'a'.repeat(64)}|player-v1\nnext`))
  assert.deepEqual(result.controls, [{ type: 'SAVED', characterId: actor, fileSha256: 'a'.repeat(64), storageFormat: 'player-v1' }])
  assert.deepEqual(result.game.map(String), ['next'])
})

test('enforces explicit onboarding state transitions', () => {
  assert.equal(canTransitionOnboardingState('CHALLENGE', 'ALLOW'), true)
  assert.equal(canTransitionOnboardingState('ALLOW', 'VERIFIED'), true)
  assert.equal(canTransitionOnboardingState('RESERVE', 'RESERVED'), true)
  assert.equal(canTransitionOnboardingState('VERIFIED', 'CLAIMED'), true)
  assert.equal(canTransitionOnboardingState('SAVED', 'COMMIT'), true)
  assert.equal(canTransitionOnboardingState('ERR', 'ABORT'), true)
  assert.equal(canTransitionOnboardingState('RESERVED', 'COMMIT'), false)
  assert.equal(canTransitionOnboardingState('RESERVED', 'VERIFIED'), false)
  assert.equal(canTransitionOnboardingState('VERIFIED', 'ALLOW'), false)
  assert.equal(canTransitionOnboardingState('RESERVED', 'CLAIMED'), false)
  assert.equal(canTransitionOnboardingState('CLAIMED', 'SAVED'), false)
  assert.throws(() => assertLegalOnboardingTransition('RESERVED', 'COMMIT'), OnboardingProtocolError)
})

test('shares the C ticket and control bounds', () => {
  assert.equal(onboardingProtocolLimits.maxTicketLineBytes, 256)
  assert.equal(onboardingProtocolLimits.maxControlLineBytes, 256)
  assert.equal(onboardingProtocolLimits.maxEvidenceControlLineBytes, 240)
  assert.equal(onboardingProtocolLimits.maxStorageFormatBytes, 32)
})

test('log metadata is allowlisted and never returns secrets', () => {
  const safe = sanitizeOnboardingLogMetadata({
    mode: 'claim', correlationId: correlation, phase: 'verifying', accessToken: token,
    ticket: 'MUD1O|secret', hmacSecret: 'hmac', gamePassword: 'password', nested: { token },
    reasonCode: token,
  })
  assert.deepEqual(safe, { mode: 'claim', correlationId: correlation, phase: 'verifying' })
  assert.ok(!JSON.stringify(safe).includes(token))
})

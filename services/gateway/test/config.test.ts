import assert from 'node:assert/strict'
import test from 'node:test'
import { ConfigError, loadConfig } from '../src/config.js'

test('production requires exact origins and active authentication', () => {
  assert.throws(
    () => loadConfig({ NODE_ENV: 'production', SUPABASE_URL: 'https://project.supabase.co' }),
    ConfigError
  )
  assert.throws(
    () => loadConfig({ NODE_ENV: 'production', AUTH_DISABLED: 'true', ALLOWED_ORIGINS: 'https://mud.example.com' }),
    /AUTH_DISABLED/
  )
})

test('test-only disabled authentication produces a bounded gateway config', () => {
  const config = loadConfig({
    NODE_ENV: 'test',
    AUTH_DISABLED: 'true',
    ALLOWED_ORIGINS: 'http://localhost:3000,https://preview.example.com',
    MAX_CONNECTIONS: '2',
    MUD_PORT: '4100'
  })
  assert.equal(config.authDisabled, true)
  assert.equal(config.mudPort, 4100)
  assert.equal(config.maxConnections, 2)
  assert.deepEqual([...config.allowedOrigins], ['http://localhost:3000', 'https://preview.example.com'])
})

test('origins cannot contain paths or wildcard-like values', () => {
  assert.throws(
    () => loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', ALLOWED_ORIGINS: 'https://mud.example.com/ws' }),
    /exact origins/
  )
})

test('an internal Auth URL does not change the public JWT issuer', () => {
  const config = loadConfig({
    NODE_ENV: 'production',
    SUPABASE_URL: 'https://mud.example.com',
    SUPABASE_AUTH_URL: 'http://muhan-auth:9999',
    ALLOWED_ORIGINS: 'https://mud.example.com'
  })

  assert.equal(config.supabaseAuthUrl, 'http://muhan-auth:9999')
  assert.equal(config.jwtIssuer, 'https://mud.example.com/auth/v1')
})

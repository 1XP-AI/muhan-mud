import assert from 'node:assert/strict'
import { createHmac, randomUUID, timingSafeEqual } from 'node:crypto'

const base = process.env.AUTH_SMOKE_URL
const secret = process.env.AUTH_SMOKE_JWT_SECRET
let stage = 'configuration'
try {
  assert.equal(base, 'http://127.0.0.1:9999')
  assert(secret && secret.length >= 32)
  const email = `mud-${randomUUID()}@example.test`
  const password = `Local-only-${randomUUID()}`
  const request = async (path, body, token) => {
    const response = await fetch(`${base}${path}`, {
      method: body === undefined ? 'GET' : 'POST',
      headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      signal: AbortSignal.timeout(10000),
    })
    const data = await response.json().catch(() => ({}))
    return { status: response.status, ok: response.ok, data }
  }
  const verify = (token, userId) => {
    const [header, body, signature] = token.split('.')
    assert.equal(JSON.parse(Buffer.from(header, 'base64url')).alg, 'HS256')
    const expected = createHmac('sha256', secret).update(`${header}.${body}`).digest()
    const actual = Buffer.from(signature, 'base64url')
    assert(actual.length === expected.length && timingSafeEqual(actual, expected))
    const claims = JSON.parse(Buffer.from(body, 'base64url'))
    assert.equal(claims.sub, userId)
    assert.equal(claims.aud, 'authenticated')
    assert.equal(claims.role, 'authenticated')
    assert(claims.exp > Date.now() / 1000)
  }
  stage = 'signup'
  const signup = await request('/signup', { email, password })
  assert(signup.ok)
  const userId = signup.data.user?.id ?? signup.data.id
  assert.match(userId, /^[0-9a-f-]{36}$/)
  stage = 'wrong-password'
  assert.equal((await request('/token?grant_type=password', { email, password: `${password}-wrong` })).ok, false)
  stage = 'password-login'
  const login = await request('/token?grant_type=password', { email, password })
  assert(login.ok)
  verify(login.data.access_token, userId)
  stage = 'user-identity'
  const user = await request('/user', undefined, login.data.access_token)
  assert(user.ok && user.data.id === userId)
  stage = 'refresh'
  const refresh = await request('/token?grant_type=refresh_token', { refresh_token: login.data.refresh_token })
  assert(refresh.ok)
  verify(refresh.data.access_token, userId)
  assert(refresh.data.refresh_token !== login.data.refresh_token)
  stage = 'logout'
  assert((await request('/logout', {}, refresh.data.access_token)).ok)
  stage = 'revoked-refresh'
  assert.equal((await request('/token?grant_type=refresh_token', { refresh_token: refresh.data.refresh_token })).ok, false)
  process.stdout.write('real-auth-smoke: signup password-login JWT user refresh logout passed\n')
} catch {
  process.stderr.write(`real-auth-smoke: failed stage=${stage}\n`)
  process.exitCode = 1
}

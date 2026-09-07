import assert from 'node:assert/strict'
import test from 'node:test'
import { createRealAuthFixture } from './real-auth-fixture.js'

const id = '10000000-0000-4000-8000-000000000001'
test('real Auth fixture uses signup and retains server-issued identity', async () => {
  const result = await createRealAuthFixture('http://127.0.0.1:9999', 'web-claim@example.test', async (url, init) => {
    assert.equal(url, 'http://127.0.0.1:9999/signup')
    assert.equal(init?.method, 'POST')
    assert.deepEqual(JSON.parse(String(init?.body)), { email: 'web-claim@example.test', password: 'web-stack-password' })
    return Response.json({ user: { id }, access_token: 'header.body.signature' })
  })
  assert.deepEqual(result, { userId: id, accessToken: 'header.body.signature' })
})
test('real Auth fixture rejects nonlocal targets before sending a request', async () => {
  let calls = 0
  await assert.rejects(createRealAuthFixture('https://example.com', 'web-claim@example.test', async () => { calls++; return Response.json({}) }))
  assert.equal(calls, 0)
})
for (const body of [{}, { user: { id: '../invalid' }, access_token: 'a.b.c' }, { user: { id } }])
  test('real Auth fixture rejects incomplete signup evidence', async () => {
    await assert.rejects(createRealAuthFixture('http://127.0.0.1:9999', 'web-claim@example.test', async () => Response.json(body)))
  })
test('real Auth fixture fails without exposing server error bodies', async () => {
  await assert.rejects(createRealAuthFixture('http://127.0.0.1:9999', 'web-claim@example.test', async () => Response.json({ secret: 'do-not-print' }, { status: 500 })), error => error instanceof Error && !error.message.includes('do-not-print'))
})

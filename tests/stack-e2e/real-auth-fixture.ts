import assert from 'node:assert/strict'

/** Synthetic users in the opt-in loopback-only GoTrue container. */
export async function createRealAuthFixture(authUrl: string, email: string,
  request: typeof fetch = fetch): Promise<{ userId: string; accessToken: string }> {
  assert.equal(authUrl, 'http://127.0.0.1:9999', 'real Auth fixture must remain local')
  assert.match(email, /^web-(provision|claim)@example\.test$/)
  const response = await request(`${authUrl}/signup`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password: 'web-stack-password' }),
    signal: AbortSignal.timeout(10000),
  })
  assert(response.ok, 'real Auth fixture signup failed')
  const body = await response.json() as { user?: { id?: unknown }; access_token?: unknown }
  assert(typeof body.user?.id === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(body.user.id), 'real Auth fixture missing user identity')
  assert(typeof body.access_token === 'string' && body.access_token.split('.').length === 3, 'real Auth fixture missing access token')
  return { userId: body.user.id, accessToken: body.access_token }
}

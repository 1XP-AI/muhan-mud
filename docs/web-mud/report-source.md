# Web MUD research source ledger

Canonical research notes for the 2026-09-01 architecture decision. This file records claim provenance; the user-facing plan is `architecture-plan.md`.

| Claim | Source | Evidence / scope | Confidence |
|---|---|---|---|
| Supabase Auth JWT integrates with Postgres RLS | [Auth](https://supabase.com/docs/guides/auth), [RLS](https://supabase.com/docs/guides/database/postgres/row-level-security) | Browser identity and row authorization; not an authoritative game rules engine | High |
| Broadcast is preferred for scalable secured fan-out; Presence is slow-changing state | [Database changes](https://supabase.com/docs/guides/realtime/subscribing-to-database-changes), [Presence](https://supabase.com/docs/guides/realtime/presence) | Applies to client presentation events and online state | High |
| Realtime plan limits materially constrain per-tick fan-out | [Realtime limits](https://supabase.com/docs/guides/realtime/limits), [Settings](https://supabase.com/docs/guides/realtime/settings) | Free 200 connections/100 events per second; Pro 500/500 at access date | High |
| Edge Functions accept WebSockets but have hard worker limits | [WebSockets](https://supabase.com/docs/guides/functions/websockets), [Limits](https://supabase.com/docs/guides/functions/limits), [Timeout troubleshooting](https://supabase.com/docs/guides/troubleshooting/edge-functions-worker-timeouts-and-websocket-drops) | `waitUntil` cannot extend the 150s Free / 400s paid wall-clock ceiling | High |
| Supabase JWTs can be verified with project JWKS | [JWT](https://supabase.com/docs/guides/auth/jwts) | Verify signature, issuer, audience, expiration and subject; HS256 projects need Auth-server validation | High |
| Vercel now has WebSocket Functions, but connections remain function-duration and instance bound | [WebSocket support](https://vercel.com/kb/guide/do-vercel-serverless-functions-support-websocket-connections), [chat example](https://vercel.com/kb/guide/real-time-chat-websockets) | Newer June 2026 material supersedes an older limits page, but feature is still beta and shared state is external | High, with first-party doc conflict |
| Vercel container functions are stateless and time-bound | [Docker versus Render](https://vercel.com/kb/guide/docker-on-vercel-vs-render), [Dockerfile announcement](https://vercel.com/blog/dockerfile-on-vercel) | Suitable for HTTP services, not a single forever-running file-backed MUD world | High |
| xterm.js is a frontend emulator and accepts byte-safe UTF-8 output | [README](https://github.com/xtermjs/xterm.js/), [Encoding](https://xtermjs.org/docs/guides/encoding/) | Requires a real backend transport; `Uint8Array` input is streaming UTF-8 | High |
| xterm integrations must treat terminal data and WebSockets as untrusted | [Security](https://xtermjs.org/docs/guides/security/) | WSS, authentication, origin policy, no demo attach server, no unsafe DOM transfer | High |
| Legacy MUD is a single-process TCP loop with file-backed mutable state | `src/main.c`, `src/io.c`, `src/files2.c`, `src/player_path.c` | Repository static audit and existing smoke tests | High |

## Material disagreement

Vercel's older canonical limits page says Functions cannot act as WebSocket servers, while June 2026 first-party KB articles and examples document Fluid Compute WebSocket support. The decision uses the newer evidence but does not depend on the beta because the MUD still requires an unbounded single-writer process and persistent filesystem.

## Research stop condition

Research stopped after the authoritative product boundaries, current limits, authentication path, protocol safety concerns and legacy runtime requirements were supported by primary sources, and three independent lanes converged on the same topology. Further provider comparison would not change the MVP boundary; the concrete always-on host remains a deployment-time choice.

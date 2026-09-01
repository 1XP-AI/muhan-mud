# Web MUD runtime deployment

The C MUD and WebSocket gateway are separate long-running services.  The C
server is the single authority for the game world and player files; it is not
an HTTP service and must never have TCP port 4000 published publicly.

## Local Compose

Prerequisites are Docker with Linux/amd64 emulation available when the host is
not amd64, and a working `services/gateway/` implementation.

Create a local `.env` file (do not commit it):

```dotenv
GATEWAY_PORT=8080
NODE_ENV=development
ALLOWED_ORIGINS=http://localhost:3000
SUPABASE_URL=https://example.supabase.co
SUPABASE_PUBLISHABLE_KEY=sb_publishable_example
SUPABASE_JWT_ISSUER=https://example.supabase.co/auth/v1
SUPABASE_JWT_AUDIENCE=authenticated
SUPABASE_INTERNAL_REST_URL=http://supabase-postgrest:3000
SUPABASE_SERVICE_ROLE_KEY=service_role_example
MUD_ADMISSION_SECRET=replace-with-32-to-512-printable-ascii-bytes
GATEWAY_INSTANCE_ID=gateway-1
```

Then build and start the runtime:

```bash
docker compose up --build
```

Only the gateway is published: its readiness endpoint is
`http://localhost:8080/healthz` and its WebSocket endpoint is
`ws://localhost:8080/ws` (use WSS behind production TLS).
The `mud` service has `expose: 4000` for the internal `mud-net` network only;
there is intentionally no host `4000:4000` mapping.

On first run, `mud-init` copies the image's immutable seed tree to the
`muhan-web-mud-data` named volume and gives it to the non-root `muhan` runtime
user (UID/GID 10001).  That includes `rooms`, `objmon`, `player`, `post`,
`help`, `log`, `resources_utf8`, `resources_manifest`, and the runtime
binaries.  The source checkout is only read while Docker builds the image;
Compose never bind-mounts it, so local sessions cannot modify checked-in game
data.

The named volume is deliberately disposable for local development:

```bash
# Stop services but retain local players and world changes.
docker compose down

# Destroy local game state and seed a fresh volume on the next `up`.
docker compose down -v
```

Do not use `down -v` for a production deployment.

## Image behavior

`docker/mud.Dockerfile` is a Linux/amd64 multi-stage build.  It compiles the
existing Makefile without overriding its default `-DDEBUG`; the legacy
`main()` therefore stays foreground rather than forking into the background.
The container entrypoint uses `frp -r "$MUD_PORT"`, retaining the legacy
`SO_REUSEADDR` option and making the MUD process container PID 1.

The MUD process runs as the dedicated `muhan` user.  `mud-init` is the only
short-lived root service, needed to initialize/chown an empty Docker volume.
The gateway image uses a separate non-root `gateway` user.

The MUD health check reads `/proc/net/tcp` for a LISTEN socket on `MUD_PORT`.
It does not open a TCP connection, because every accepted legacy connection
starts the old ident-helper path.  This is an in-container readiness check;
the gateway only starts after it is healthy.  Gateway HTTP readiness should be
used by an external load balancer.

## Production persistence and backups

The MUD saves raw C structs and uses rename-based replacement of room/player
files.  It requires one writer, a Linux/amd64-compatible runtime, and a
durable filesystem with atomic rename semantics.  Do not run multiple `mud`
replicas against the same volume and do not migrate its state by changing
architecture/ABI.

For production, use the same `/home/muhan` mount point with a provider-backed
persistent disk/volume.  Create the volume from the image seed only once, then
back up its full contents while the MUD is stopped or by provider snapshot:

- `rooms/` — world and permanent room state
- `player/` — player records plus aliases, bank, family, vote, marriage, and
  invite data
- `post/` and `log/` — messages, boards, audit/ident artifacts, and logs
- `objmon/`, `help/`, `resources_utf8/`, and `resources_manifest/` — retain
  with a snapshot so runtime resource resolution matches the server binary

Restore into an empty replacement volume, preserve ownership for UID/GID
10001, and start exactly one `mud` instance.  Test restore with a disposable
environment before directing gateway traffic to it.

On `SIGTERM`, the C process now sets only a signal-safe shutdown flag.  Its
main loop then stops accepting new TCP connections, broadcasts the fixed
shutdown notice, makes one normal nonblocking output pass, and runs the same
`resave_all_rom(1)` and `save_all_ply()` sequence used by the scheduled in-game
shutdown before exiting with status 0.  Drain the gateway first so no browser
session begins a new admission while the MUD listener is closing.

The normal case is bounded by the event loop's 75 ms select timeout plus the
existing synchronous save work.  A stalled filesystem can still block those
legacy writes; retain regular volume snapshots and set the platform grace
period above the measured save time for the largest production world.  The
deterministic regression command below runs a real `frp`, sends `SIGTERM`,
requires a clean bounded exit, and relogs into the saved player.  It redacts
the test password from transcript, diagnostics, and retained fixture logs.

```bash
scripts/run-shutdown-scenario.sh
```

## Network and secret policy

- Terminate TLS before or in the gateway and expose only HTTPS/WSS publicly.
- Keep `mud-net` private; never add a host port mapping to the MUD service.
- Set exact production and preview origins in `ALLOWED_ORIGINS`; do not use a
  wildcard.
- Mount the Supabase service-role key only into the Gateway process, alongside
  its private `SUPABASE_INTERNAL_REST_URL`; never put either in browser
  variables, logs, frontend build variables, or the MUD process.
- `MUD_ADMISSION_SECRET` must be the same at the Gateway and C MUD, contain
  32 through 512 printable ASCII bytes, and be mounted as a secret rather than
  committed configuration. `GATEWAY_INSTANCE_ID` identifies only the Gateway
  lease holder and contains no user credential.
- Deployment manifests pass the Gateway-only REST/service-role/instance values
  to the Gateway container and the same admission secret to Gateway and MUD.
  Compose requires these values at interpolation time. Helm also injects
  `MUD_REQUIRE_TRUSTED_ADMISSION=1`, so a missing secret cannot bring up a
  legacy password listener.

The gateway must enforce the architecture protocol: authenticate before
opening MUD TCP, validate JWT issuer/audience/expiry/subject, consume Telnet
echo negotiation, relay terminal bytes as binary frames, and bound slow-client
queues.  Its image is built from `services/gateway/`; its own health endpoint
is the appropriate public readiness probe.

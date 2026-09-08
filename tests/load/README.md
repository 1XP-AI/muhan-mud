# Local Go connector load harness

This directory is a test-owned, local-only harness. It wraps the current Go
`WorldConnector` in `httptest` handlers; it does not add a production REST
route, use a browser, contact an external network, start Docker, or read real
player data.

The two modes answer different questions:

- `rest-like`: every local HTTP request performs `Open -> Submit -> Close`.
  This is a request-per-command cost model, not a claim that production has a
  REST API.
- `persistent`: local HTTP requests open sessions once, submit one command,
  then close them. The run holds admitted sessions while the target is opened,
  probes one overflow admission, and accounts the remaining virtual clients,
  so `MaxSessions=32` is visible as an expected `503` rejection without an
  unsafe 1000-request burst.

Only bounded targets `100`, `250`, `500`, and `1000` are accepted. The fixture
uses synthetic deterministic characters and a deterministic `점수` command.

## Small smoke

```sh
cd tests/load
go test -run 'Test(PersistentCapacityReportsMaxSessions|RequestIsolationAcrossLocalRuntimes)$' -count=1
go run ./cmd/muhan-load -mode rest-like -target 100 -timeout 30s
go run ./cmd/muhan-load -mode persistent -target 100 -timeout 30s
```

The command prints JSON suitable for attaching to a local performance note.
The persistent smoke should report `accepted=32`, `rejected=68`, and
`completed=32`. A REST-like run should complete all 100 requests. Any failure
includes `stage`, `actor`, HTTP status, and the server error; first fix the
reported stage before raising a target.

Benchmarks compare the same local adapters without external dependencies:

```sh
go test -run '^$' -bench 'Benchmark(RESTLike|PersistentSession)Command$' -benchtime=3s -count=1
```

For a target sweep, run the CLI separately at each staged value. Do not infer
production or PostgreSQL capacity from this memory-store result.

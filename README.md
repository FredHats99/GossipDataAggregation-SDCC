# GossipDataAggregation-SDCC

## Go Service Bootstrap (Step 2)

The base Go module and clean-architecture bootstrap are available with:

- `cmd/node/main.go` entrypoint
- `internal/app` application wiring and graceful shutdown
- `internal/config` config loading (file + env overrides)
- `internal/api` health endpoints scaffold (`/healthz`, `/readyz`)
- `internal/observability/logging` structured JSON logging

Implementation status and rationale are tracked in `IMPLEMENTATION_NOTES.md`.
Aggregation design choices are tracked in `AGGREGATION_DESIGN.md`.
Gossip aggregation pipeline choices are tracked in `GOSSIP_AGGREGATION_PIPELINE.md`.

### Local run

Use default config values:

```powershell
make run
```

Use file config:

```powershell
$env:APP_CONFIG_PATH="configs/node.dev.json"
make run
```

Override with env vars:

```powershell
$env:NODE_ID="node-1"
$env:HTTP_ADDR=":8081"
$env:BIND_ADDR="0.0.0.0:7000"
$env:SEED_NODES="node1:7000,node2:7000,node3:7000"
$env:GOSSIP_INTERVAL_MS="1000"
$env:FANOUT="2"
$env:LOG_LEVEL="debug"
$env:SHUTDOWN_TIMEOUT_SECONDS="15"
make run
```

### Health endpoints

- `GET /healthz` returns process liveness
- `GET /readyz` returns readiness (automatically set to not-ready during shutdown)
- `GET /members` returns current membership snapshot (`node_id`, `endpoint`, `status`, `last_seen`)

### Membership transport

Membership bootstrap and liveness probing use the gossip protocol envelope over UDP:

- `Ping` payload: `node_id`, `incarnation`
- `Ack` payload: `acked_seq`, `status`, optional `reason`
- frames are encoded through `internal/gossip/transport.JSONCodec`
- UDP I/O is provided by `internal/gossip/transport.UDPFrameTransport`

The old text protocol (`PING <node_id>` / `ACK <node_id>`) is no longer used.

### Transport reliability

`internal/gossip/transport` includes reusable Step 4 reliability components:

- `RetryingSender` for bounded retry/backoff around any `Sender`
- `MessageGuard` for duplicate suppression and per-sender sequence monotonicity
- `GuardedReceiver` for skipping duplicate or stale envelopes

These components are implemented and tested. `MessageGuard` is not yet wired
into membership runtime because restart handling needs `incarnation` or
persistent sequence semantics.

## Local Docker Setup (Functional Testing)

This project includes a local Docker Compose setup to test gossip functionality with 3 nodes.

### Files

- `.env`: local runtime parameters and tunable values.
- `.env.example`: template copy of `.env`.
- `docker-compose.yml`: default root Compose file for local 3-node startup.
- `deployments/docker-compose.local.yml`: local cluster startup definition.

### Start local cluster

```powershell
docker compose -f docker-compose.yml up -d --build
```

Equivalent explicit local Compose file:

```powershell
docker compose --env-file .env -f deployments/docker-compose.local.yml up -d --build
```

### Stop local cluster

```powershell
docker compose -f docker-compose.yml down
```

For the explicit local Compose file:

```powershell
docker compose --env-file .env -f deployments/docker-compose.local.yml down
```

### Tune parameters

Edit `.env` to modify:

- Gossip timing: `GOSSIP_INTERVAL_MS`, `ANTI_ENTROPY_INTERVAL_MS`
- Fanout: `FANOUT`
- Seed topology: `SEED_NODES`
- Logging: `LOG_LEVEL`
- Exposed ports: `API_PORT_BASE`, `GOSSIP_PORT` (node2/node3 host ports are set in compose)

## Testing

Default unit suite (recommended for local dev and CI):

```powershell
go test -count=1 ./...
```

Integration tests are excluded from default `go test` and require explicit tag:

```powershell
go test -tags=integration -count=1 ./...
```

Equivalent Make targets:

```powershell
make test
make test-race
make test-integration
make lint
```

PowerShell wrapper (auto-detect `go`, fallback Docker):

```powershell
powershell -ExecutionPolicy Bypass -File scripts/go-test.ps1
```

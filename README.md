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
Anti-entropy and snapshot sync choices are tracked in `STEP7_ANTI_ENTROPY_STATE_SYNC.md`.
Persistence and crash recovery choices are tracked in `STEP8_PERSISTENCE_CRASH_RECOVERY.md`.
Transitive membership dissemination is documented in `STEP3_MEMBERSHIP_DISSEMINATION.md`.

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
$env:TOPK_MAX="10"
$env:OUTBOUND_QUEUE_SIZE="128"
$env:LOG_LEVEL="debug"
$env:SHUTDOWN_TIMEOUT_SECONDS="15"
make run
```

### Health endpoints

- `GET /healthz` returns process liveness
- `GET /readyz` returns readiness (automatically set to not-ready during shutdown)
- `GET /members` returns current membership snapshot (`node_id`, `endpoint`, `status`, `incarnation`, `last_seen`)
- `POST /update` applies a local `SUM` or `TOPK` update and emits a gossip delta
- `GET /aggregate/sum` returns the local SUM estimate
- `GET /aggregate/topk?k=...` returns the local TOP-K estimate

Example SUM update:

```powershell
Invoke-RestMethod -Method Post -Uri http://localhost:8080/update -ContentType application/json -Body '{"aggregate_type":"SUM","value":5}'
Invoke-RestMethod -Uri http://localhost:8080/aggregate/sum
```

Example TOP-K update:

```powershell
Invoke-RestMethod -Method Post -Uri http://localhost:8080/update -ContentType application/json -Body '{"aggregate_type":"TOPK","value":{"item_id":"item-a","score":9.5}}'
Invoke-RestMethod -Uri 'http://localhost:8080/aggregate/topk?k=3'
```

### Membership transport

Membership bootstrap and liveness probing use the gossip protocol envelope over UDP:

- membership handshakes use envelope protocol version `v2`
- `Ping` payload: `node_id`, `endpoint`, `incarnation`, optional bounded `membership` batch
- `Ack` payload: `acked_seq`, `status`, optional `reason`, `endpoint`,
  `incarnation`, optional bounded `membership` batch
- frames are encoded through `internal/gossip/transport.JSONCodec`
- UDP I/O is provided by `internal/gossip/transport.UDPFrameTransport`
- indirectly learned live endpoints join the membership probe pool
- the aggregate gossip runtime also uses live transitively discovered peers

The old text protocol (`PING <node_id>` / `ACK <node_id>`) is no longer used.
See `STEP3_MEMBERSHIP_DISSEMINATION.md` for merge, restart, endpoint, and
bounded-dissemination semantics.

Run the isolated partial-seed Docker verification after building the local
image:

```powershell
docker compose build
powershell -ExecutionPolicy Bypass -File scripts\test-membership-dissemination.ps1
```

### Transport reliability

`internal/gossip/transport` includes reusable Step 4 reliability components:

- `RetryingSender` for bounded retry/backoff around any `Sender`
- `MessageGuard` for duplicate suppression and per-sender sequence monotonicity
- `GuardedReceiver` for skipping duplicate or stale envelopes

These components are implemented and tested. Membership uses incarnation-aware
state merge, but `MessageGuard` is still not applied to `Ping`/`Ack`: its
sender sequence high-water mark is not scoped by incarnation and would reject
the reset sequence of a legitimate restarted process.

### Gossip aggregation runtime

Step 6 now wires aggregation deltas into runtime gossip flow:

- local updates enqueue `StateDelta` payloads
- `internal/gossip/delta.Runtime` wraps deltas in `StateDelta` envelopes
- outbound deltas are sent to sampled peers with `RetryingSender`
- incoming `StateDelta` envelopes are dispatched from the membership UDP listener
- received deltas are merged into the local pipeline and forwarded only when
  the merge advances local state

The local Docker convergence run has been verified with three nodes for
membership, `SUM`, and `TOPK` propagation.

### Anti-entropy and state sync

Step 7 adds periodic `StateDigest` exchange, delta-range repair, and snapshot fallback:

- digest messages include aggregate versions, state checksums, and per-origin
  delta sequence watermarks
- behind nodes request missing delta ranges from a bounded in-memory history
- evicted ranges and same-version divergence fall back to selected snapshots
- snapshots are merged through the existing CRDT rules, not applied as blind
  replacement
- temporary partitions heal after connectivity returns

### Persistence and crash recovery

Step 8 persists every effective aggregate mutation and restores state before
the node rejoins the cluster:

- append-only checksummed WAL with configurable fsync policy
- periodic atomic snapshots with the covered WAL index
- startup recovery from latest snapshot plus WAL tail
- named Docker volume per node
- preserved local delta sequence across restart

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
- Aggregation runtime: `TOPK_MAX`, `OUTBOUND_QUEUE_SIZE`, `DELTA_HISTORY_SIZE`
- Persistence: `DATA_DIR`, `SNAPSHOT_INTERVAL_SECONDS`, `WAL_FSYNC_MODE`,
  `WAL_FSYNC_BATCH_SIZE`
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

Docker crash/recovery test:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/test-crash-recovery.ps1
```

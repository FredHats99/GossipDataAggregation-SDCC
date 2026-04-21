# GossipDataAggregation-SDCC

## Go Service Bootstrap (Step 2)

The base Go module and clean-architecture bootstrap are available with:

- `cmd/node/main.go` entrypoint
- `internal/app` application wiring and graceful shutdown
- `internal/config` config loading (file + env overrides)
- `internal/api` health endpoints scaffold (`/healthz`, `/readyz`)
- `internal/observability/logging` structured JSON logging

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
$env:LOG_LEVEL="debug"
$env:SHUTDOWN_TIMEOUT_SECONDS="15"
make run
```

### Health endpoints

- `GET /healthz` returns process liveness
- `GET /readyz` returns readiness (automatically set to not-ready during shutdown)

## Local Docker Setup (Functional Testing)

This project includes a local Docker Compose setup to test gossip functionality with 3 nodes.

### Files

- `.env`: local runtime parameters and tunable values.
- `.env.example`: template copy of `.env`.
- `deployments/docker-compose.local.yml`: local cluster startup definition.

### Start local cluster

```powershell
docker compose --env-file .env -f deployments/docker-compose.local.yml up -d --build
```

### Stop local cluster

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

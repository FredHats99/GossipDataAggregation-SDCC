# GossipDataAggregation-SDCC

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

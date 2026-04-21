# GossipDataAggregation-SDCC Detailed Implementation Checklist

This checklist is designed to track implementation progress for a gossip-based decentralized data aggregation service in Go, including robustness to node crash and Docker Compose deployment on EC2.

## 0) Project Setup and Governance

- [x] Confirm selected aggregation operations for MVP:
  - [x] `SUM` (MVP strategy: exact CRDT G-Counter)
  - [x] `TOP-K` (MVP strategy: exact deterministic bounded candidate set)
- [ ] Define non-functional targets:
  - [ ] Cluster size target (e.g., 3/5/10 nodes)
  - [ ] Convergence expectation (eventual, bounded time under normal network)
  - [ ] Fault model (node crash, restart, packet loss, partition)
- [x] Decide coding standards:
  - [x] Go version
  - [x] Lint/test tooling
  - [x] Logging/metrics conventions (see `LOGGING_CONVENTIONS.md`)
  - [x] Decision draft documented in `STEP0_CODING_STANDARDS.md`

## 1) Requirements and Protocol Contract

- [x] Write protocol specification document (see `PROTOCOL_SPEC.md`):
  - [x] Node identity format (`node_id`)
  - [x] Message envelope fields (`type`, `seq`, `timestamp`, `checksum`, `from`)
  - [x] Message types (`Ping`, `StateDigest`, `StateDelta`, `Ack`, `SnapshotReq`, `SnapshotResp`)
- [ ] Define merge invariants for all aggregate states:
  - [x] Associative merge
  - [x] Commutative merge
  - [x] Idempotent merge
- [x] Define aggregation interface contract:
  - [x] `Update(value)`
  - [x] `Merge(peerState)`
  - [x] `Estimate()`
  - [x] `Serialize()/Deserialize()`
- [x] Define versioning strategy:
  - [x] State version/vector clock/Lamport rule
  - [x] Backward compatibility plan for protocol evolution

## 2) Base Go Module and Clean Architecture

- [x] Initialize core module:
  - [x] `go.mod` and Go toolchain setup
  - [x] Base `Makefile` targets (`build`, `test`, `lint`, `run`)
- [x] Create app bootstrap:
  - [x] Dependency wiring in `internal/app`
  - [x] Graceful shutdown (SIGTERM/SIGINT)
  - [x] Context propagation and cancellation
- [x] Add configuration system:
  - [x] File + environment override
  - [x] Validation and default values
- [x] Add structured logging and error policy:
  - [x] Log levels and key fields (`node_id`, `peer`, `msg_type`)
  - [x] Error classification (`recoverable`, `fatal`)
- [x] Add health endpoints scaffold:
  - [x] `/healthz`
  - [x] `/readyz`

## 3) Membership and Peer Discovery

- [ ] Implement static seed-based bootstrap:
  - [ ] Seed parsing from config/env
  - [ ] Join handshake
- [ ] Implement gossip membership view:
  - [ ] Periodic peer sampling
  - [ ] Membership table with statuses (`alive`, `suspect`, `dead`)
- [ ] Implement failure detection:
  - [ ] Timeout or phi-based suspicion
  - [ ] State transition thresholds and timers
- [ ] Implement membership convergence tests:
  - [ ] New node joins and becomes visible cluster-wide
  - [ ] Dead node eventually marked dead

## 4) Gossip Transport Layer

- [ ] Implement transport abstraction in `internal/gossip/transport`:
  - [ ] Sender interface
  - [ ] Receiver interface
  - [ ] Pluggable codec
- [ ] Implement message encoding/decoding:
  - [ ] JSON or protobuf encoding choice
  - [ ] Envelope validation and checksum verification
- [ ] Add reliability controls:
  - [ ] Retry/backoff for selected message types
  - [ ] Duplicate suppression cache
  - [ ] Max message size and reject policy
- [ ] Add anti-loop protections:
  - [ ] Seen-message cache with TTL
  - [ ] Sequence monotonicity per sender
- [ ] Add transport tests:
  - [ ] Serialization round-trip
  - [ ] Duplicate handling
  - [ ] Malformed message handling

## 5) CRDT-Style Aggregation State

- [ ] Implement `SUM` aggregator:
  - [ ] Per-node contribution map
  - [ ] Merge by element-wise max (if G-Counter style)
  - [ ] Read estimate as total sum
- [ ] Implement `TOP-K` aggregator:
  - [ ] Mergeable bounded candidate structure
  - [ ] Deterministic tie-breaking
  - [ ] Bounded memory guarantees
- [ ] Define serialization contract for aggregate states:
  - [ ] Versioned schema
  - [ ] Compatibility tests
- [ ] Add property-style merge tests:
  - [ ] Commutativity test cases
  - [ ] Associativity test cases
  - [ ] Idempotency test cases

## 6) Wire Gossip + Aggregation

- [ ] Implement local update pipeline:
  - [ ] Validate input
  - [ ] Apply to local state
  - [ ] Emit delta for gossip
- [ ] Implement receive/merge pipeline:
  - [ ] Validate sender/message
  - [ ] Merge state/delta
  - [ ] Trigger forward gossip on advancement
- [ ] Add backpressure controls:
  - [ ] Queue size limits
  - [ ] Drop strategy + metrics
- [ ] Verify convergence in a multi-node local run:
  - [ ] Random update workload
  - [ ] Eventual equal estimates across nodes

## 7) Anti-Entropy and State Sync

- [ ] Implement periodic digest exchange:
  - [ ] Digest content (state version summary)
  - [ ] Pull missing deltas when behind
- [ ] Implement snapshot sync fallback:
  - [ ] Snapshot request/response messages
  - [ ] Safe apply on receiving full snapshot
- [ ] Add healing tests:
  - [ ] Packet loss scenario
  - [ ] Temporary network partition scenario
  - [ ] Post-healing convergence checks

## 8) Persistence and Crash Recovery

- [ ] Implement WAL in `internal/storage/wal`:
  - [ ] Append-only update records
  - [ ] fsync policy and batching strategy
- [ ] Implement snapshots in `internal/storage/snapshot`:
  - [ ] Periodic snapshot creation
  - [ ] Snapshot metadata and integrity check
- [ ] Implement startup recovery:
  - [ ] Load latest snapshot
  - [ ] Replay WAL tail
- [ ] Add crash-recovery tests:
  - [ ] Kill node during write workload
  - [ ] Restart node and verify no invalid rollback
  - [ ] Rejoin and converge with peers

## 9) API and Observability

- [ ] Implement HTTP API in `internal/api`:
  - [ ] `POST /update`
  - [ ] `GET /aggregate/sum`
  - [ ] `GET /aggregate/topk?k=...`
  - [ ] `GET /members`
  - [ ] `GET /healthz`
  - [ ] `GET /readyz`
- [ ] Add input validation and error responses:
  - [ ] Consistent error schema
  - [ ] Proper status codes
- [ ] Add metrics in `internal/observability`:
  - [ ] Gossip send/receive counters
  - [ ] Merge counts and failures
  - [ ] Convergence lag estimate
  - [ ] Queue depth/drop counters
- [ ] Add dashboards/queries:
  - [ ] Basic Prometheus scrape config
  - [ ] Optional Grafana starter dashboard

## 10) Testing Strategy (Including Robustness to Crash)

- [ ] Unit tests:
  - [ ] Aggregator merge laws
  - [ ] Transport codec and validation
  - [ ] Membership transitions
- [ ] Integration tests (`test/integration`):
  - [ ] 3+ node converge on `SUM`
  - [ ] 3+ node converge on `TOP-K`
- [ ] Fault tests (`test/fault`):
  - [ ] Random node crash/restart
  - [ ] Packet loss/delay injection
  - [ ] Temporary partition and heal
- [ ] End-to-end tests (`test/e2e`):
  - [ ] API-driven workload + convergence assertion
  - [ ] Sustained load smoke test
- [ ] CI pipeline:
  - [ ] Run unit + integration on every push
  - [ ] Nightly/periodic fault test job

## 11) Docker Compose for Local and EC2

- [ ] Create Dockerfile:
  - [ ] Multi-stage build
  - [ ] Non-root runtime user
  - [ ] Minimal runtime image
- [ ] Add local functional test startup:
  - [ ] Create `deployments/docker-compose.local.yml`
  - [ ] Wire 3-node local cluster (`node1`, `node2`, `node3`)
  - [ ] Validate local boot with `docker compose up`
- [ ] Create `deployments/docker-compose.yml`:
  - [ ] `node1..nodeN` services
  - [ ] Stable network and service names
  - [ ] Seed list env wiring
  - [ ] Healthchecks and restart policy
- [ ] Add root `.env` tuning file:
  - [ ] Gossip timing knobs (`GOSSIP_INTERVAL_MS`, `ANTI_ENTROPY_INTERVAL_MS`)
  - [ ] Dissemination knob (`FANOUT`)
  - [ ] Seed and identity variables (`SEED_NODES`, `NODE*_ID`)
  - [ ] Logging and image variables (`LOG_LEVEL`, `APP_IMAGE`)
- [ ] Add `.env.example` template for onboarding
- [ ] Create `deployments/docker-compose.fault.yml`:
  - [ ] Fault-injection profile (if used)
  - [ ] Observability stack profile
- [ ] Add helper scripts:
  - [ ] Load generator script
  - [ ] Random kill/restart script
  - [ ] Convergence check script

## 12) EC2 Deployment Hardening and Runbook

- [ ] Prepare EC2 instance baseline:
  - [ ] Docker + Compose installed
  - [ ] Time sync and hostname config
  - [ ] Disk/log retention setup
- [ ] Configure network/security:
  - [ ] Security group rules for API/gossip ports
  - [ ] Restrict public exposure to required endpoints
- [ ] Prepare deployment artifacts:
  - [ ] `.env` or config templates for production
  - [ ] Startup script (`deployments/ec2/bootstrap.sh`)
- [ ] Define operations runbook (`deployments/ec2/runbook.md`):
  - [ ] Start/stop/update procedures
  - [ ] Crash recovery procedure
  - [ ] Scale-out procedure
  - [ ] Troubleshooting checklist

## Project Structure Implementation Checklist

- [ ] Create root layout:
  - [ ] `cmd/node/main.go`
  - [ ] `internal/`
  - [ ] `pkg/`
  - [ ] `test/`
  - [ ] `deployments/`
  - [ ] `scripts/`
  - [ ] `configs/`
- [ ] Create internal package directories:
  - [ ] `internal/app`
  - [ ] `internal/config`
  - [ ] `internal/membership`
  - [ ] `internal/gossip/transport`
  - [ ] `internal/gossip/protocol`
  - [ ] `internal/gossip/anti_entropy`
  - [ ] `internal/aggregation/common`
  - [ ] `internal/aggregation/sum`
  - [ ] `internal/aggregation/topk`
  - [ ] `internal/storage/wal`
  - [ ] `internal/storage/snapshot`
  - [ ] `internal/api`
  - [ ] `internal/observability`
  - [ ] `internal/simulation`
- [ ] Create test directories:
  - [ ] `test/integration`
  - [ ] `test/fault`
  - [ ] `test/e2e`
- [ ] Create deployment directories/files:
  - [ ] `deployments/Dockerfile`
  - [ ] `deployments/docker-compose.local.yml`
  - [ ] `deployments/docker-compose.yml`
  - [ ] `deployments/docker-compose.fault.yml`
  - [ ] `deployments/ec2/bootstrap.sh`
  - [ ] `deployments/ec2/runbook.md`
- [ ] Create support files:
  - [ ] `.env`
  - [ ] `.env.example`
  - [ ] `scripts/loadgen.sh`
  - [ ] `scripts/kill_random_node.sh`
  - [ ] `scripts/check_convergence.sh`
  - [ ] `configs/node.dev.yaml`
  - [ ] `configs/node.prod.yaml`

## Definition of Done (Project)

- [ ] Cluster converges for selected operations in normal conditions.
- [ ] Crash/restart tests pass with state recovery and re-convergence.
- [ ] Fault scenarios (loss/partition) recover via anti-entropy/snapshot.
- [ ] Docker Compose deployment works locally and on EC2.
- [ ] README includes architecture, run commands, and test instructions.

# GossipDataAggregation-SDCC Detailed Implementation Checklist

This checklist is designed to track implementation progress for a gossip-based decentralized data aggregation service in Go, including robustness to node crash and Docker Compose deployment on EC2.

## 0) Project Setup and Governance

- [x] Confirm selected aggregation operations for MVP:
  - [x] `SUM` (MVP strategy: exact CRDT G-Counter)
  - [x] `TOP-K` (MVP strategy: exact deterministic bounded candidate set)
- [x] Define non-functional targets (see `NON_FUNCTIONAL_TARGETS.md`):
  - [x] Cluster size target (3/5/10 progressive tiers)
  - [x] Convergence expectation (eventual + soft SLO under normal network)
  - [x] Fault model (phased crash/restart, loss/delay, partition/heal)
- [x] Decide coding standards:
  - [x] Go version
  - [x] Lint/test tooling
  - [x] Logging/metrics conventions (see `LOGGING_CONVENTIONS.md`)
  - [x] Decision draft documented in `STEP0_CODING_STANDARDS.md`

## 1) Requirements and Protocol Contract

- [x] Write protocol specification document (see `PROTOCOL_SPEC.md`):
  - [x] Node identity format (`node_id`)
  - [x] Message envelope fields (`type`, `seq`, `timestamp`, `checksum`, `from`)
  - [x] Message types (`Ping`, `StateDigest`, `StateDelta`, `DeltaRangeReq`, `DeltaRangeResp`, `Ack`, `SnapshotReq`, `SnapshotResp`)
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

- [x] Implement static seed-based bootstrap:
  - [x] Seed parsing from config/env
  - [x] Join handshake
- [ ] Implement gossip membership view:
  - [x] Periodic seed/peer probing
  - [x] Membership table with statuses (`alive`, `suspect`, `dead`)
  - [ ] Disseminate membership entries through gossip messages
  - [x] Align membership `Ping`/`Ack` with protocol envelope
- [x] Implement failure detection:
  - [x] Timeout or phi-based suspicion
  - [x] State transition thresholds and timers
- [x] Implement membership convergence tests:
  - [x] New node joins and becomes visible cluster-wide
  - [x] Dead node eventually marked dead

## 4) Gossip Transport Layer

- [x] Implement transport abstraction in `internal/gossip/transport`:
  - [x] Sender interface
  - [x] Receiver interface
  - [x] Pluggable codec
- [x] Implement message encoding/decoding:
  - [x] JSON encoding choice
  - [x] Envelope validation and checksum verification
- [x] Add reliability controls:
  - [x] Retry/backoff for selected message types
  - [x] Duplicate suppression cache
  - [x] Max message size and reject policy for UDP frames
- [x] Add anti-loop protections:
  - [x] Seen-message cache with TTL
  - [x] Sequence monotonicity per sender
- [x] Add transport tests:
  - [x] Serialization round-trip
  - [x] Duplicate handling
  - [x] Malformed/invalid envelope handling

## 5) CRDT-Style Aggregation State

- [x] Implement `SUM` aggregator:
  - [x] Per-node contribution map
  - [x] Merge by element-wise max (G-Counter style)
  - [x] Read estimate as total sum
- [x] Implement `TOP-K` aggregator:
  - [x] Mergeable bounded candidate structure
  - [x] Deterministic tie-breaking
  - [x] Bounded memory guarantees
- [x] Define serialization contract for aggregate states:
  - [x] Versioned schema
  - [x] Compatibility/roundtrip tests
- [x] Add property-style merge tests:
  - [x] Commutativity test cases
  - [x] Associativity test cases
  - [x] Idempotency test cases

## 6) Wire Gossip + Aggregation

- [x] Implement local update pipeline:
  - [x] Validate input
  - [x] Apply to local state
  - [x] Emit delta for gossip
- [x] Implement receive/merge pipeline:
  - [x] Validate message payload
  - [x] Merge state/delta
  - [x] Trigger forward gossip on advancement
- [x] Add backpressure controls:
  - [x] Queue size limits
  - [x] Drop strategy + metrics
- [x] Verify convergence in a multi-node local run:
  - [x] Deterministic simulated multi-manager workload
  - [x] Eventual equal estimates across managers under full delta delivery
  - [x] Runtime `StateDelta` send/receive/forward wiring over transport interfaces
  - [x] Networked multi-node gossip run

## 7) Anti-Entropy and State Sync

- [x] Implement periodic digest exchange:
  - [x] Digest content (state version summary)
  - [x] Digest content includes state checksums to detect same-version divergence
  - [x] Pull missing aggregate state when behind via snapshot fallback
  - [x] Pull missing deltas by sequence range from a bounded in-memory delta history
- [x] Implement snapshot sync fallback:
  - [x] Snapshot request/response messages
  - [x] Safe apply on receiving full snapshot
- [x] Add healing tests:
  - [x] Dropped-delta healing scenario
  - [x] Temporary network partition scenario
  - [x] Post-healing convergence checks

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

- [x] Implement HTTP API in `internal/api`:
  - [x] `POST /update`
  - [x] `GET /aggregate/sum`
  - [x] `GET /aggregate/topk?k=...`
  - [x] `GET /members`
  - [x] `GET /healthz`
  - [x] `GET /readyz`
- [ ] Add input validation and error responses:
  - [x] Basic JSON error schema for aggregate endpoints
  - [x] Basic method/status handling for aggregate endpoints
  - [ ] Project-wide consistent error schema
- [ ] Add metrics in `internal/observability`:
  - [ ] Gossip send/receive counters
  - [ ] Merge counts and failures
  - [ ] Convergence lag estimate
  - [ ] Queue depth/drop counters
- [ ] Add dashboards/queries:
  - [ ] Basic Prometheus scrape config
  - [ ] Optional Grafana starter dashboard

## 10) Testing Strategy (Including Robustness to Crash)

- [x] Unit tests:
  - [x] Aggregator merge laws
  - [x] Transport codec and validation
  - [x] Membership transitions
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

- [x] Create Dockerfile:
  - [x] Multi-stage build
  - [x] Non-root runtime user
  - [x] Minimal runtime image
- [ ] Add local functional test startup:
  - [x] Create root `docker-compose.yml`
  - [x] Create `deployments/docker-compose.local.yml`
  - [x] Wire 3-node local cluster (`node1`, `node2`, `node3`)
  - [x] Validate local boot with `docker compose up`
- [ ] Create `deployments/docker-compose.yml`:
  - [ ] `node1..nodeN` services
  - [ ] Stable network and service names
  - [ ] Seed list env wiring
  - [ ] Healthchecks and restart policy
- [x] Add root `.env` tuning file:
  - [x] Gossip timing knobs (`GOSSIP_INTERVAL_MS`, `ANTI_ENTROPY_INTERVAL_MS`)
  - [x] Dissemination knob (`FANOUT`)
  - [x] Seed and identity variables (`SEED_NODES`, `NODE*_ID`)
  - [x] Logging and image variables (`LOG_LEVEL`, `APP_IMAGE`)
- [x] Add `.env.example` template for onboarding
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
  - [x] `cmd/node/main.go`
  - [x] `internal/`
  - [ ] `pkg/`
  - [ ] `test/`
  - [x] `deployments/`
  - [x] `scripts/`
  - [x] `configs/`
- [ ] Create internal package directories:
  - [x] `internal/app`
  - [x] `internal/config`
  - [x] `internal/membership`
  - [x] `internal/gossip/transport`
  - [x] `internal/gossip/protocol`
  - [x] `internal/gossip/anti_entropy` behavior integrated in `internal/gossip/delta`
  - [x] `internal/aggregation/common`
  - [x] `internal/aggregation/sum`
  - [x] `internal/aggregation/topk`
  - [x] `internal/aggregation/pipeline`
  - [ ] `internal/storage/wal`
  - [ ] `internal/storage/snapshot`
  - [x] `internal/api`
  - [x] `internal/observability`
  - [ ] `internal/simulation`
- [ ] Create test directories:
  - [ ] `test/integration`
  - [ ] `test/fault`
  - [ ] `test/e2e`
- [ ] Create deployment directories/files:
  - [x] `docker-compose.yml`
  - [x] `deployments/Dockerfile`
  - [x] `deployments/docker-compose.local.yml`
  - [ ] `deployments/docker-compose.yml`
  - [ ] `deployments/docker-compose.fault.yml`
  - [ ] `deployments/ec2/bootstrap.sh`
  - [ ] `deployments/ec2/runbook.md`
- [ ] Create support files:
  - [x] `.env`
  - [x] `.env.example`
  - [ ] `scripts/loadgen.sh`
  - [ ] `scripts/kill_random_node.sh`
  - [ ] `scripts/check_convergence.sh`
  - [x] `configs/node.dev.json`
  - [ ] `configs/node.dev.yaml`
  - [ ] `configs/node.prod.yaml`

## Definition of Done (Project)

- [ ] Cluster converges for selected operations in normal conditions.
- [ ] Crash/restart tests pass with state recovery and re-convergence.
- [ ] Fault scenarios (loss/partition) recover via anti-entropy/snapshot.
- [ ] Docker Compose deployment works locally and on EC2.
- [ ] README includes architecture, run commands, and test instructions.

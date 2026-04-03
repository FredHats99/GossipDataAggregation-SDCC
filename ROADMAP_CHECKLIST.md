# GossipDataAggregation-SDCC Detailed Implementation Checklist

This checklist is designed to track implementation progress for a gossip-based decentralized data aggregation service in Go, including robustness to node crash and Docker Compose deployment on EC2.

## 0) Project Setup and Governance

- [ ] Confirm selected aggregation operations for MVP:
  - [ ] `SUM`
  - [ ] `TOP-K`
- [ ] Define non-functional targets:
  - [ ] Cluster size target (e.g., 3/5/10 nodes)
  - [ ] Convergence expectation (eventual, bounded time under normal network)
  - [ ] Fault model (node crash, restart, packet loss, partition)
- [ ] Decide coding standards:
  - [ ] Go version
  - [ ] Lint/test tooling
  - [ ] Logging/metrics conventions

## 1) Requirements and Protocol Contract

- [ ] Write protocol specification document:
  - [ ] Node identity format (`node_id`)
  - [ ] Message envelope fields (`type`, `seq`, `timestamp`, `checksum`, `from`)
  - [ ] Message types (`Ping`, `StateDigest`, `StateDelta`, `Ack`, `SnapshotReq`, `SnapshotResp`)
- [ ] Define merge invariants for all aggregate states:
  - [ ] Associative merge
  - [ ] Commutative merge
  - [ ] Idempotent merge
- [ ] Define aggregation interface contract:
  - [ ] `Update(value)`
  - [ ] `Merge(peerState)`
  - [ ] `Estimate()`
  - [ ] `Serialize()/Deserialize()`
- [ ] Define versioning strategy:
  - [ ] State version/vector clock/Lamport rule
  - [ ] Backward compatibility plan for protocol evolution

## 2) Base Go Module and Clean Architecture

- [ ] Initialize core module:
  - [ ] `go.mod` and Go toolchain setup
  - [ ] Base `Makefile` targets (`build`, `test`, `lint`, `run`)
- [ ] Create app bootstrap:
  - [ ] Dependency wiring in `internal/app`
  - [ ] Graceful shutdown (SIGTERM/SIGINT)
  - [ ] Context propagation and cancellation
- [ ] Add configuration system:
  - [ ] File + environment override
  - [ ] Validation and default values
- [ ] Add structured logging and error policy:
  - [ ] Log levels and key fields (`node_id`, `peer`, `msg_type`)
  - [ ] Error classification (`recoverable`, `fatal`)
- [ ] Add health endpoints scaffold:
  - [ ] `/healthz`
  - [ ] `/readyz`

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
- [ ] Create `deployments/docker-compose.yml`:
  - [ ] `node1..nodeN` services
  - [ ] Stable network and service names
  - [ ] Seed list env wiring
  - [ ] Healthchecks and restart policy
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
  - [ ] `deployments/docker-compose.yml`
  - [ ] `deployments/docker-compose.fault.yml`
  - [ ] `deployments/ec2/bootstrap.sh`
  - [ ] `deployments/ec2/runbook.md`
- [ ] Create support files:
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

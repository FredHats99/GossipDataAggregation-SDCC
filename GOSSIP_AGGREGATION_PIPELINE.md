# Gossip Aggregation Pipeline

This document records Step 6 implementation choices for wiring aggregate state
to gossip-style deltas.

## Scope

Step 6 introduces an application-level pipeline. It does not yet start a real
network gossip runtime.

Implemented:

- local update validation and apply
- delta creation after successful local updates
- outbound bounded queue for gossip dissemination
- receive/merge pipeline for incoming `StateDelta`
- runtime sender loop for queued `StateDelta` messages
- UDP envelope dispatch from the membership listener to the delta runtime
- forwarding received deltas when they advance local aggregate state
- HTTP endpoints for update/read smoke workflows
- periodic digest exchange for anti-entropy repair
- bounded delta history and sequence-range repair
- snapshot request/response fallback for missed aggregate state
- duplicate-safe merge behavior inherited from the aggregators
- simulated multi-node convergence test

Not implemented yet:

- automated Docker fault-injection profile

## Package Layout

- `internal/gossip/protocol`
  - defines `StateDelta`
  - defines `SUMDelta`
  - defines `TOPKDelta`
  - provides constructors and decoders for aggregate deltas

- `internal/aggregation/pipeline`
  - owns a node-local `SUM` and `TOP-K` state
  - applies local updates
  - emits outbound deltas
  - applies received deltas
  - tracks outbound queue drops

- `internal/gossip/delta`
  - consumes outbound deltas from the pipeline
  - wraps deltas in protocol envelopes
  - sends deltas to sampled peers through `transport.Sender`
  - applies received `StateDelta` envelopes
  - forwards advancing received deltas to peers
  - exchanges `StateDigest` messages periodically
  - serves and applies `DeltaRangeReq`/`DeltaRangeResp` repair messages
  - serves and applies snapshot repair messages

- `internal/gossip/anti_entropy`
  - compares local and peer digests
  - stores bounded per-origin delta histories
  - computes missing contiguous sequence ranges
  - decides which aggregate snapshots must be requested

- `internal/api`
  - exposes `POST /update`
  - exposes `GET /aggregate/sum`
  - exposes `GET /aggregate/topk?k=...`

## Local Update Pipeline

The pipeline entrypoint is `Manager.ApplyLocalUpdate`.

For `SUM`:

1. validate positive increment through the `sum.GCounter`
2. increment only the local node contribution
3. read the cumulative local contribution
4. emit `StateDelta(SUM)` with:
   - `aggregate_type = SUM`
   - `delta_version = local SUM state version`
   - `origin_node_id = local node ID`
   - `delta_sequence = next local delta sequence`
   - `delta.node_id = local node_id`
   - `delta.value = cumulative contribution for local node`

For `TOP-K`:

1. accept a candidate candidate input
2. set `origin_node_id` to the local manager node id
3. validate and apply it through `topk.Set`
4. emit `StateDelta(TOPK)` with:
   - `aggregate_type = TOPK`
   - `delta_version = local TOP-K state version`
   - `origin_node_id = local node ID`
   - `delta_sequence = next local delta sequence`
   - candidate tuple fields in `delta`

The local manager owns `origin_node_id` assignment for local updates. This
prevents a future API caller from spoofing another node as candidate origin.

## Receive/Merge Pipeline

The receive entrypoint is `Manager.ApplyReceivedDelta`.

For `SUM`:

- decode `SUMDelta`
- convert the cumulative contribution to a partial `sum.State`
- merge with element-wise max

For `TOP-K`:

- decode `TOPKDelta`
- convert the candidate tuple to a partial `topk.State`
- merge by union, deterministic sort, and bounded trim

The method returns `advanced=true` only when local state actually changed.
Duplicate deltas are safe and return `advanced=false`.

The runtime-level receive path is `delta.Runtime.HandleEnvelope`.

For `StateDelta` envelopes:

1. duplicate/stale envelopes are filtered by `transport.MessageGuard`
2. payload bytes are decoded into `protocol.StateDelta`
3. the pipeline merge path is invoked
4. advancing merges are forwarded to sampled peers

The forwarded envelope preserves original `from` and `seq` metadata so other
nodes can suppress duplicate paths with the same guard semantics.

## Backpressure

Each manager has a bounded outbound queue.

Behavior:

- successful local updates attempt to enqueue a `StateDelta`
- if the queue is full, the new delta is dropped
- drop count is exposed by `DroppedOutbound`

Chosen policy:

- drop newest on full queue

Reason:

- simple and deterministic
- does not block API/update paths
- safe for CRDT state because future anti-entropy/snapshot sync can repair
  dropped deltas

This is an MVP policy. Later work can add priority, coalescing, or per-peer
queues.

## HTTP Smoke Workflow

The runtime can now be exercised through HTTP:

```powershell
Invoke-RestMethod -Method Post -Uri http://localhost:18080/update -ContentType application/json -Body '{"aggregate_type":"SUM","value":5}'
Invoke-RestMethod -Uri http://localhost:18080/aggregate/sum
```

For TOP-K:

```powershell
Invoke-RestMethod -Method Post -Uri http://localhost:18080/update -ContentType application/json -Body '{"aggregate_type":"TOPK","value":{"item_id":"item-a","score":9.5}}'
Invoke-RestMethod -Uri 'http://localhost:18080/aggregate/topk?k=3'
```

## Convergence Tests

`TestManagersConvergeWithBroadcastDeltas` creates three managers, applies local
`SUM` and `TOP-K` updates on different nodes, broadcasts the emitted deltas to
the other managers, and asserts equal estimates.

This verifies:

- local update emits usable deltas
- receive/merge pipeline applies deltas correctly
- duplicate-safe CRDT merge semantics remain intact
- all managers can converge to the same `SUM` and `TOP-K` under full delta
  delivery

That unit test is not a network test; it covers deterministic in-memory
delivery.

The Docker Compose runtime path was also verified locally with three nodes:

- `docker compose -f docker-compose.yml up -d --build`
- `GET /healthz` and `GET /readyz` returned healthy responses on all nodes
- `/members` converged to three `alive` members on all nodes
- a `SUM` update posted to node1 converged to value `5` on node1, node2, and node3
- a `TOPK` update posted to node2 converged to the same item on node1, node2, and node3

## Step 7/Runtime Notes

Step 7 runtime wiring:

- `StateDigest` messages compare aggregate versions and checksums
- digests advertise per-origin contiguous delta sequence watermarks
- `DeltaRangeReq`/`DeltaRangeResp` repair gaps from bounded in-memory history
- `SnapshotReq` requests only aggregates that appear behind or divergent
- `SnapshotResp` carries versioned serialized aggregate states
- missing/evicted delta ranges automatically fall back to snapshots
- snapshots are merged through CRDT rules, so local unseen updates are preserved

Remaining runtime hardening:

- decide whether to replace the direct listener callback with a bounded inbound queue
- decide how much of `MessageGuard` should be incarnation-aware before crash/restart work
- add real packet-loss automation around the Docker deployment

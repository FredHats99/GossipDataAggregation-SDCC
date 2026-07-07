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
- duplicate-safe merge behavior inherited from the aggregators
- simulated multi-node convergence test

Not implemented yet:

- background sender loop over real peers
- real network `StateDelta` exchange over `transport.Sender`
- forwarding received deltas to other peers
- API endpoints for updates and reads
- anti-entropy digest/snapshot repair

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

## Local Update Pipeline

The pipeline entrypoint is `Manager.ApplyLocalUpdate`.

For `SUM`:

1. validate positive increment through the `sum.GCounter`
2. increment only the local node contribution
3. read the cumulative local contribution
4. emit `StateDelta(SUM)` with:
   - `aggregate_type = SUM`
   - `delta_version = local SUM state version`
   - `delta.node_id = local node_id`
   - `delta.value = cumulative contribution for local node`

For `TOP-K`:

1. accept a candidate candidate input
2. set `origin_node_id` to the local manager node id
3. validate and apply it through `topk.Set`
4. emit `StateDelta(TOPK)` with:
   - `aggregate_type = TOPK`
   - `delta_version = local TOP-K state version`
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

## Convergence Test

`TestManagersConvergeWithBroadcastDeltas` creates three managers, applies local
`SUM` and `TOP-K` updates on different nodes, broadcasts the emitted deltas to
the other managers, and asserts equal estimates.

This verifies:

- local update emits usable deltas
- receive/merge pipeline applies deltas correctly
- duplicate-safe CRDT merge semantics remain intact
- all managers can converge to the same `SUM` and `TOP-K` under full delta
  delivery

It is not a network test. Networked gossip convergence belongs to the next
runtime integration step.

## Step 7/Runtime Notes

The next runtime wiring should:

- serialize `protocol.StateDelta` as envelope payload for `StateDelta` messages
- send queued deltas through `transport.RetryingSender`
- receive envelopes through `transport.GuardedReceiver`
- apply decoded deltas through `Manager.ApplyReceivedDelta`
- forward deltas when merges advance local state
- add anti-entropy digest and snapshot repair for dropped or missed deltas

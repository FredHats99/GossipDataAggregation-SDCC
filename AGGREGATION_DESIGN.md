# Aggregation Design

This document records Step 5 implementation choices for the MVP aggregate
state layer.

## Scope

Step 5 implements in-memory CRDT-style aggregate state only.

Included:

- `SUM` aggregator
- `TOP-K` aggregator
- versioned serialization/deserialization
- merge-law tests for associativity, commutativity, and idempotency

Not included yet:

- HTTP update/read API
- gossip `StateDelta` send/receive pipeline
- anti-entropy digest exchange
- snapshot sync
- persistence/WAL

Those are covered by later roadmap steps.

## Package Layout

- `internal/aggregation/common`
  - aggregate type constants
  - protocol version constant
  - shared `Aggregator` interface
  - shared `node_id` validation

- `internal/aggregation/sum`
  - exact G-Counter implementation

- `internal/aggregation/topk`
  - deterministic bounded candidate-set implementation

## SUM Choice

`SUM` is implemented as an exact CRDT G-Counter.

State:

- `contrib[node_id] -> uint64`
- each node only increments its own key during local updates

Update:

- accepts positive integer increments
- rejects zero and negative increments
- increments local node contribution
- advances local state version by `+1`

Merge:

- applies element-wise `max` for every observed `node_id`
- advances local Lamport-style state version only when state changes
- duplicate merges do not change the estimate

Estimate:

- returns the sum of all node contributions

Why this was selected:

- exact for increment-only workloads
- duplicate-safe
- order-independent
- converges under gossip when all updates are eventually delivered

Tradeoff:

- supports monotonic increments only; decrement support would require a
  PN-Counter variant later

## TOP-K Choice

`TOP-K` is implemented as an exact deterministic bounded candidate set.

State:

- candidates are tuples:
  - `item_id`
  - `score`
  - `event_ts`
  - `origin_node_id`
- exact duplicate tuples are deduplicated
- memory is bounded by configured `Kmax`

Update:

- validates candidate shape
- rejects empty `item_id`
- rejects invalid `origin_node_id`
- rejects `NaN` and infinite scores
- rejects zero timestamps
- inserts the candidate, canonicalizes, sorts, and trims to `Kmax`

Merge:

- unions local and peer candidates
- removes exact duplicate tuples
- sorts with the protocol comparator
- keeps the first `Kmax` candidates

Ordering:

1. `score` descending
2. `event_ts` descending
3. `origin_node_id` ascending
4. `item_id` ascending

Important design choice:

The implementation follows the normative `PROTOCOL_SPEC.md` model: TOP-K state
is a set of candidate tuples with no exact duplicates. It does not collapse all
entries with the same `item_id` into a single winner.

Reason:

- tuple-set union plus deterministic top-`Kmax` pruning is associative,
  commutative, and idempotent
- replacing by `item_id` would require a separate conflict-resolution rule and
  more careful proof that pruning remains safe

This can be revisited later if product semantics require one active entry per
`item_id`.

## Serialization Contract

Both aggregate implementations serialize to JSON with:

- `aggregate_type`
- `state_version`
- `protocol_version`
- `schema_version`
- `state_payload`

Current values:

- protocol version: `v1`
- schema version: `1`

`Deserialize` rejects unsupported aggregate/protocol/schema combinations.

`SUM` payload:

- map `node_id -> uint64`

`TOP-K` payload:

- canonical sorted candidate list

## Versioning

Each aggregate tracks an in-memory state version.

Rules:

- successful local update: `version += 1`
- merge that advances state: `version = max(local_version, peer_version) + 1`
- merge with no effective state change: `version = max(local_version, peer_version)`

The version is intended for future digest/snapshot optimization. It is not used
for conflict resolution; merge rules define correctness.

## Merge-Law Tests

Tests validate the required CRDT-style laws:

- commutativity
- associativity
- idempotency

Additional tests cover:

- update + estimate behavior
- duplicate merge no-op behavior
- serialization/deserialization roundtrip
- TOP-K deterministic tie-breaking

## Step 6 Integration Notes

The next step should wire these states into gossip:

- local update pipeline emits `StateDelta`
- receive pipeline validates `StateDelta`
- `SUM` delta carries cumulative per-node contribution
- `TOP-K` delta carries candidate tuple
- merge advancement triggers optional forward gossip

`internal/gossip/transport.RetryingSender` and `GuardedReceiver` are ready to be
used by that pipeline.

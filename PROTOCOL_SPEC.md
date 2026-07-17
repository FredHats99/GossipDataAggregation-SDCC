# Gossip Protocol Specification (Draft)

This document is the Step 1 draft protocol contract for gossip communication.

## 1) Node Identity Format (`node_id`)

### Requirements

- unique within a cluster
- stable across process restarts for the same logical node
- ASCII, lowercase, URL-safe

### Format

- regex: `^[a-z0-9][a-z0-9-]{0,30}$`
- examples:
  - `node1`
  - `node-az1-3`
- invalid:
  - `Node1` (uppercase)
  - `node_1` (underscore)

## 2) Message Envelope

Every gossip message must include:

- `type` (string): message type discriminator
- `seq` (uint64): sender-local monotonic sequence
- `timestamp` (RFC3339 UTC string): sender event time
- `checksum` (string): payload integrity hash (hex SHA-256)
- `from` (string): sender `node_id`
- `trace_id` (string, optional): cross-peer trace correlation
- `correlation_id` (string, optional): exchange/batch correlation
- `version` (string): protocol version, initial `v1`

### Envelope JSON shape

```json
{
  "type": "StateDelta",
  "seq": 1842,
  "timestamp": "2026-04-15T10:22:31.412Z",
  "checksum": "7a5d...e3",
  "from": "node1",
  "trace_id": "tr_7d9e",
  "correlation_id": "corr_91aa",
  "version": "v1",
  "payload": {}
}
```

## 3) Message Types

## 3.1 `Ping`

Purpose:

- liveness check and membership freshness

Envelope version: `v2`

Payload:

- `node_id` (string)
- `endpoint` (reachable `host:port`)
- `incarnation` (uint64)
- `membership` (optional array of at most 64 membership entries)
  - `node_id` (string)
  - `endpoint` (reachable `host:port`)
  - `status` (`alive|suspect|dead`)
  - `incarnation` (uint64)

## 3.2 `StateDigest`

Purpose:

- summarize known state versions to detect divergence

Payload:

- `aggregates` (object):
  - `sum_version` (uint64)
  - `topk_version` (uint64)
  - `sum_checksum` (string, optional SHA-256 of serialized SUM state)
  - `topk_checksum` (string, optional SHA-256 of serialized TOP-K state)
- `membership_version` (uint64)
- `delta_sequences` (object, optional): highest contiguous `delta_sequence` by
  origin node

Implementation note:

- the Go implementation encodes this as explicit `sum` and `topk` digest
  objects with `version` and `checksum` fields
- checksums are used to detect same-version divergent aggregate content

## 3.3 `StateDelta`

Purpose:

- disseminate incremental aggregate updates

Payload:

- `aggregate_type` (`SUM|TOPK`)
- `delta_version` (uint64)
- `origin_node_id` (string)
- `delta_sequence` (uint64, monotonic for the origin node's delta stream)
- `delta` (object, aggregate-specific)

SUM delta shape:

- `node_id` (string)
- `value` (uint64, cumulative contribution for `node_id` in v1)

TOP-K delta shape:

- `item_id` (string)
- `score` (number)
- `event_ts` (RFC3339 UTC string)
- `origin_node_id` (string)

## 3.4 `DeltaRangeReq`

Purpose:

- request missing deltas from a peer's bounded history

Payload:

- `ranges` (array), one range per origin:
  - `origin_node_id` (string)
  - `from_sequence` (uint64, inclusive)
  - `to_sequence` (uint64, inclusive)
- `known_versions` (requester's `StateDigest`)

Each range MUST contain at most 256 deltas. Larger gaps are repaired over
successive digest rounds.

## 3.5 `DeltaRangeResp`

Purpose:

- return a complete requested sequence range

Payload:

- `deltas` (non-empty array of sequenced `StateDelta` payloads)

If any requested sequence is unavailable, the responder MUST send a
`SnapshotResp` fallback instead of a partial range response.

## 3.6 `Ack`

Purpose:

- acknowledge receipt/application of a sequence

Membership handshake envelope version: `v2`

Payload:

- `acked_seq` (uint64)
- `status` (`accepted|rejected`)
- `reason` (string, optional)
- `endpoint` (responder's reachable `host:port`)
- `incarnation` (responder's uint64 process incarnation)
- `membership` (optional bounded membership-entry array using the `Ping` entry shape)

## 3.7 `SnapshotReq`

Purpose:

- request full state transfer when delta sync is insufficient

Payload:

- `want_aggregate_types` (array of `SUM|TOPK`)
- `known_versions` (object with local versions)

## 3.8 `SnapshotResp`

Purpose:

- return full state snapshot for requested aggregates

Payload:

- `snapshot_version` (uint64)
- `sum_state` (object, optional)
- `topk_state` (object, optional)
- `delta_sequences` (object, optional): per-origin watermarks covered by the snapshot
- `created_at` (RFC3339 UTC string)

Snapshot apply rule:

- receivers MUST merge snapshot state into local state using the aggregate CRDT
  merge rules
- receivers MUST NOT blindly replace local state with a peer snapshot
- receivers SHOULD advance local delta watermarks to the values covered by a
  successfully merged snapshot

## 4) Validation Rules

- reject unknown `type`
- reject missing required envelope fields
- reject invalid `from` (fails `node_id` regex)
- reject checksum mismatch
- reject stale or non-monotonic `seq` per sender policy
- enforce max message size
- reject direct membership observations with missing endpoint or zero incarnation
- ignore malformed indirectly disseminated membership entries

## 5) Compatibility Rules

- `version` defaults to `v1`
- receivers must ignore unknown payload fields
- adding optional fields is backward-compatible in `v1`
- breaking changes require new `version` value

## 6) Merge Invariants (Normative)

All aggregate state merge functions MUST satisfy:

- Associativity: `merge(a, merge(b, c)) = merge(merge(a, b), c)`
- Commutativity: `merge(a, b) = merge(b, a)`
- Idempotency: `merge(a, a) = a`

Rationale:

- gossip delivery order is not stable
- duplicates are expected
- nodes may crash/restart and replay state

If any invariant is violated, cluster convergence is not guaranteed.

## 6.1 SUM Invariants and Merge Contract (G-Counter)

State model:

- `sum_state` is a map `contrib[node_id] -> uint64`
- each node updates only its own key

Delta/apply rule:

- local `Update(x)` on node `n` sets `contrib[n] = contrib[n] + x`
- `StateDelta(SUM)` carries cumulative `value = contrib[node_id]`

Merge rule:

- for every key `k` in either state:
  - `merged.contrib[k] = max(left.contrib[k], right.contrib[k])`

Estimate rule:

- `Estimate(SUM) = sum(contrib[*])`

Why invariants hold:

- `max` is associative, commutative, idempotent per key
- map-wise composition preserves the same properties

## 6.2 TOP-K Invariants and Merge Contract (Deterministic Bounded Candidate Set)

State model:

- candidate entries are tuples:
  - `(item_id, score, event_ts, origin_node_id)`
- `topk_state` is a set of candidate entries (no exact duplicates)
- `Kmax` is a fixed configured bound (`Kmax >= max query k`)

Total order (deterministic ranking):

- sort by:
  1) `score` descending
  2) `event_ts` descending
  3) `origin_node_id` ascending
  4) `item_id` ascending

Merge rule:

- `U = union(left.candidates, right.candidates)`
- `merged.candidates = first Kmax entries of U after applying the total order`

Estimate rule:

- `Estimate(TOP-K, k)` returns first `k` entries from `topk_state` using the same total order

Why invariants hold:

- set union is associative, commutative, idempotent
- deterministic `top(Kmax, order)` on the same union is deterministic
- therefore `merge` remains associative, commutative, idempotent

## 6.3 Membership Convergence Rule

Membership entries are ordered independently per `node_id`:

- a higher `incarnation` wins;
- at equal incarnation, status severity is `alive < suspect < dead`;
- indirect lower-incarnation or lower-severity entries are stale and ignored;
- direct `Ping`/`Ack` evidence may restore `alive` at the same incarnation;
- a node that receives `suspect` or `dead` about itself at an equal or higher
  incarnation advances above the reported incarnation and advertises `alive`.

`LastSeen` and consecutive probe misses are local observations and MUST NOT be
disseminated. Membership batches MUST contain no more than 64 entries and MUST
include the sender entry. Implementations SHOULD rotate remaining entries so
tables larger than one batch still converge eventually.

## 7) Aggregation Interface Contract (Normative)

All aggregate implementations MUST expose the following logical contract:

```go
type Aggregator interface {
    // Applies a local user update and returns whether local state advanced.
    Update(value any) (advanced bool, err error)

    // Merges peer state (or decoded snapshot fragment) into local state.
    // Returns whether local state advanced.
    Merge(peerState any) (advanced bool, err error)

    // Returns a deterministic read model for API/serialization.
    Estimate(opts any) (result any, err error)

    // Encodes local state into a versioned payload.
    Serialize() ([]byte, error)

    // Decodes a payload into a valid in-memory state.
    Deserialize(payload []byte) error
}
```

Behavioral requirements:

- all methods MUST be deterministic for the same inputs
- `Merge` MUST satisfy invariants from section 6
- `advanced=false` means no effective state change
- methods MUST be concurrency-safe when called by gossip and API pipelines
- input validation errors MUST be explicit and non-fatal for process lifetime

## 7.1 `Update(value)`

Contract:

- validates user input and applies a local mutation only if valid
- MUST NOT violate merge invariants after mutation

SUM (`value` shape):

- accepted input: `uint64 increment`, `increment > 0`
- effect: increment local node contribution
- returns `advanced=true` when increment applied

TOP-K (`value` shape):

- accepted input object:
  - `item_id` (string, non-empty)
  - `score` (number, finite)
  - `event_ts` (RFC3339 UTC)
- effect: candidate inserted/replaced, then deterministic prune to `Kmax`
- returns `advanced=true` only if resulting canonical candidate set changes

## 7.2 `Merge(peerState)`

Contract:

- validates decoded peer state schema and version before merge
- merge operation MUST be monotonic (no rollback of already observed maxima)
- must be duplicate-safe (`Merge(s)` repeated does not change state after first apply)

SUM:

- input shape: map `contrib[node_id] -> uint64`
- apply element-wise `max` by key

TOP-K:

- input shape: candidate set of tuples `(item_id, score, event_ts, origin_node_id)`
- apply `union + deterministic top(Kmax)`

## 7.3 `Estimate(opts)`

Contract:

- read-only; MUST NOT mutate state
- deterministic ordering MUST be preserved in returned result

SUM:

- returns total `uint64` sum of contributions

TOP-K:

- accepts `k` in `opts`; if absent, defaults to `Kmax`
- returns first `k` candidates by section 6.2 total order

## 7.4 `Serialize()/Deserialize()`

Contract:

- serialization MUST include schema version and aggregate type discriminator
- deserialization MUST reject malformed payloads and unsupported versions
- serialize-deserialize roundtrip MUST preserve canonical state

Minimum encoded fields:

- `aggregate_type`: `SUM|TOPK`
- `state_version`: `uint64` (local aggregate state version)
- `protocol_version`: string (initially `v1`)
- `state_payload`: aggregate-specific canonical representation

Canonicalization requirements:

- maps/lists MUST be emitted in deterministic order (or sorted before compare/hash)
- floating-point values in TOP-K scores MUST be finite; NaN/Inf are invalid

## 8) Versioning Strategy (Normative)

This protocol uses two distinct versioning domains:

- transport sequence/versioning for message handling
- aggregate state versioning for anti-entropy and snapshot freshness

`seq`, `delta_sequence`, and `delta_version` MUST NOT be used to resolve
aggregate conflicts.
Conflict resolution is defined only by merge rules in section 6.

## 8.1 Transport-Level Versioning

Fields:

- `version` (envelope): protocol compatibility version; aggregate messages use
  `v1`, while membership `Ping`/`Ack` with required endpoint dissemination use
  `v2`
- `seq` (envelope): sender-local monotonic message sequence

Rules:

- each sender MUST emit strictly increasing `seq`
- receiver duplicate/staleness checks are scoped per `from` sender id
- `seq` is used for replay/duplicate suppression only, not state ordering across senders

Implementation note:

- `internal/gossip/transport.MessageGuard` implements duplicate and stale
  sequence rejection for `(from, seq)`
- membership does not apply that guard to `Ping`/`Ack` yet because the guard's
  high-water key does not include the membership incarnation; a future guard
  MUST key by `(from, incarnation)` or use a persisted sequence counter

## 8.2 Aggregate State Version Rule (Lamport)

Each aggregate (`SUM`, `TOPK`) maintains its own local Lamport clock:

- `sum_state_version` (`uint64`)
- `topk_state_version` (`uint64`)

Clock advancement:

- on successful local `Update`, increment local aggregate clock by `+1`
- on successful `Merge(peerState)` that advances local state:
  - `local_version = max(local_version, peer_version) + 1`
- on merge with no effective state change:
  - `local_version = max(local_version, peer_version)`

Propagation rules:

- `StateDelta.delta_version` MUST be the sender aggregate Lamport version after apply
- `StateDelta.delta_sequence` MUST increase monotonically for each
  `origin_node_id` and is independent from envelope `seq`
- `StateDigest.aggregates.{sum_version,topk_version}` MUST expose local aggregate Lamport versions
- `StateDigest.delta_sequences` MUST expose only contiguous per-origin delta watermarks
- `SnapshotResp.snapshot_version` MUST be `max(sum_state_version, topk_state_version)` for included states
- serialized aggregate payload MUST include `state_version` (section 7.4)

Validation rules:

- peers MAY accept deltas with lower/equal `delta_version`; merge invariants guarantee safety
- peers SHOULD use version comparison as optimization hint for pull/snapshot decisions only

## 8.3 Backward Compatibility Plan

Compatibility policy for envelope `version`:

- `v1` receivers MUST accept `v1` messages
- unknown fields MUST be ignored
- required-field removals/renames are breaking

Change classes:

- backward-compatible (within `v1`):
  - add optional fields
  - add new optional message payload attributes
  - tighten validation only if previously accepted invalid data remains invalid
- breaking (requires `v2`):
  - remove/rename required fields
  - change field type/semantics incompatibly
  - alter merge semantics for existing aggregate types

Rollout plan for major upgrades:

1) implement dual-read support (`v1` + `v2`) on receivers
2) deploy readers cluster-wide
3) switch writers to `v2`
4) retire `v1` write path after convergence window

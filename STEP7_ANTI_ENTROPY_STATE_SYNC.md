# Step 7 Anti-Entropy and State Sync

This document records the Step 7 implementation for repairing missed aggregate
gossip updates.

## Scope

Implemented:

- periodic `StateDigest` exchange
- digest content for `SUM` and `TOPK`
- digest checksums in addition to Lamport versions
- bounded in-memory delta history with per-origin contiguous watermarks
- missing-delta pull by origin sequence range
- snapshot request/response messages
- safe snapshot apply through CRDT merge
- snapshot fallback when a requested range was evicted
- healing tests for dropped deltas and temporary network partitions

Deferred to later steps:

- durable delta history/WAL across process restarts
- packet-loss injection through Docker profiles

The runtime first requests missing deltas. Snapshot repair remains the safe
fallback when the bounded history no longer contains a complete range or when
equal sequence watermarks still produce divergent checksums.

## Digest Model

`protocol.StateDigest` contains:

- `sum.version`
- `sum.checksum`
- `topk.version`
- `topk.checksum`
- `membership_version`
- `delta_sequences[origin_node_id]`

Versions are Lamport-style aggregate versions. Checksums are SHA-256 hashes of
the versioned serialized aggregate state.

The checksum is intentionally included because version-only comparison can miss
divergence when two nodes independently advance the same aggregate version but
hold different content.

Delta sequence watermarks are independent from envelope `seq` and aggregate
Lamport versions. Each locally produced delta receives a monotonically
increasing `delta_sequence`; histories only advertise the highest contiguous
sequence stored or covered by a snapshot for each origin.

## Delta Range Repair

When a peer advertises a higher contiguous watermark, the receiver sends
`DeltaRangeReq` with one range per origin. Requests are capped at 256 deltas per
origin and continue over later digest rounds when a larger gap exists.

`DeltaRangeResp` returns the original `StateDelta` payloads in sequence order.
Recovered deltas are merged idempotently and recorded locally, allowing the
repaired node to serve them to other peers.

The in-memory history is bounded by `DELTA_HISTORY_SIZE` per origin (default
1024). If any requested delta has been evicted, the responder sends a
`SnapshotResp` instead. Snapshots carry the responder's delta watermarks so the
receiver does not repeatedly request deltas already represented by the merged
snapshot.

## Snapshot Decision

The comparison logic lives in `internal/gossip/anti_entropy`.

When a node receives a peer digest, it requests a snapshot for an aggregate when:

- the peer aggregate version is greater than the local version
- or versions are equal but non-empty checksums differ

If the peer is behind, the local node does not pull from it. The peer can repair
itself when it receives this node's next digest.

## Snapshot Apply

Snapshots are not applied by replacing local state.

The pipeline decodes each snapshot fragment into aggregate state and calls the
existing CRDT merge:

- `SUM`: element-wise max of per-node contributions
- `TOPK`: union, deterministic sort, bounded trim

This preserves local updates that the snapshot sender has not seen.

## Runtime Flow

`delta.Runtime.Start` now runs two loops:

- outbound delta loop for `StateDelta`
- anti-entropy loop for periodic `StateDigest`

Incoming envelope handling:

- `StateDelta`: guarded, merged, and forwarded on advancement
- `StateDigest`: compared with local digest; may emit `DeltaRangeReq` or
  `SnapshotReq`
- `DeltaRangeReq`: answered from bounded history or with snapshot fallback
- `DeltaRangeResp`: merges and records recovered deltas, then emits a digest
- `SnapshotReq`: answered with selected aggregate snapshot fragments
- `SnapshotResp`: safely merged into local state; emits a fresh digest on
  advancement

## Verification

Covered by tests:

- `TestManagerSnapshotMergeHealsMissingState`
- `TestRuntimeRequestsSnapshotWhenDigestDiffers`
- `TestRuntimeAppliesSnapshotResponse`
- `TestRuntimeAntiEntropyHealsDroppedDelta`
- `TestRuntimeFallsBackToSnapshotWhenDeltaRangeWasEvicted`
- `TestRuntimeAntiEntropyHealsTemporaryNetworkPartition`
- delta history, range comparison, and protocol validation unit tests

Run:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/go-test.ps1
powershell -ExecutionPolicy Bypass -File scripts/go-test.ps1 -Integration
```

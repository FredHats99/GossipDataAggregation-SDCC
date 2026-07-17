# Implementation Notes

This document records the implementation decisions and code changes made while
aligning the project roadmap with the current codebase.

## Checklist Alignment

`ROADMAP_CHECKLIST.md` has been updated to reflect repository reality instead
of intent only.

Completed or partially completed items now marked explicitly:

- non-functional targets are documented in `NON_FUNCTIONAL_TARGETS.md`
- transport abstraction exists in `internal/gossip/transport`
- JSON envelope encoding/decoding exists through `JSONCodec`
- UDP frame transport exists through `UDPFrameTransport`
- Dockerfile, local Compose file, `.env`, `.env.example`, `scripts`, and
  `configs/node.dev.json` are present
- membership view remains partial because membership entries are not yet
  disseminated through gossip messages

The membership checklist was intentionally changed from complete to partial:

- implemented:
  - seed parsing
  - join handshake
  - periodic seed/peer probing
  - membership table with `alive`, `suspect`, `dead`
  - failure detection transitions
  - `Ping`/`Ack` envelope alignment
- still open:
  - disseminating membership entries through gossip messages

## Membership Protocol Alignment

Membership bootstrap and liveness probing no longer use the previous ad-hoc
text protocol:

- removed wire shape: `PING <node_id>`
- removed wire shape: `ACK <node_id>`

Membership now uses the protocol envelope defined in `PROTOCOL_SPEC.md`:

- `Ping`
  - envelope `type`: `Ping`
  - payload: `node_id`, `incarnation`
- `Ack`
  - envelope `type`: `Ack`
  - payload: `acked_seq`, `status`, optional `reason`

Code paths:

- listener: `internal/membership/bootstrap.go`
- envelope codec: `internal/gossip/transport/json_codec.go`
- UDP frame I/O: `internal/gossip/transport/udp.go`

The membership listener decodes incoming UDP frames with `JSONCodec`, accepts
only `Ping` envelopes, validates that payload `node_id` matches envelope `from`,
marks the peer alive, and replies with an `Ack` envelope.

The join client creates a `Ping` envelope, sends it over UDP, waits for an
`Ack`, verifies `acked_seq`, and returns the peer node id from envelope `from`.

## UDP Frame Transport

`internal/gossip/transport/udp.go` introduces `UDPFrameTransport`.

Responsibilities:

- bind a UDP packet socket
- send raw frames to `host:port` peers
- receive raw frames with remote peer metadata
- respect context deadlines during send/receive
- reject frames larger than `DefaultMaxFrameSize`

This keeps UDP details out of membership and future gossip business logic.

## Transport Reliability and Anti-Loop Controls

`internal/gossip/transport/reliability.go` introduces reusable wrappers for
Step 4 reliability and anti-loop requirements.

Implemented components:

- `RetryingSender`
  - wraps any `Sender`
  - retries failed sends
  - uses bounded exponential backoff
  - honors context cancellation

- `MessageGuard`
  - tracks seen `(from, seq)` pairs for a TTL
  - rejects duplicate messages
  - tracks per-sender high-water sequence
  - rejects stale or non-monotonic sequences

- `GuardedReceiver`
  - wraps any `Receiver`
  - skips duplicate/stale envelopes
  - returns only accepted envelopes

New transport errors:

- `ErrNilSender`
- `ErrNilReceiver`
- `ErrFrameTooLarge`
- `ErrDuplicateMessage`
- `ErrStaleSequence`

Important integration note:

`MessageGuard` is implemented and tested, but it is not yet wired into the
membership runtime. The reason is restart semantics: the current node sequence
counter is in memory, so a restarted node may emit `seq=1` again. Applying
strict monotonic sequence checks to membership before using `incarnation` or a
persistent sequence counter would incorrectly reject legitimate post-restart
messages.

The guard is ready for the future gossip/aggregation receive pipeline, where
the restart/versioning policy can be applied deliberately.

## Self-Seed Detection Fix

Membership self-seed detection was corrected.

Previous behavior treated local hosts such as `127.0.0.1` or `localhost` as
self regardless of port. This could cause local multi-node tests to skip all
peers on loopback.

Current behavior:

- host aliases such as `127.0.0.1`, `localhost`, `0.0.0.0`, and `::1` normalize
  to the same local host
- endpoints are considered the same only when both normalized host and port
  match

Covered by `TestSameEndpointTreatsOnlyMatchingLocalPortAsSelf`.

## Tests Added or Exercised

Transport tests:

- JSON codec roundtrip
- checksum mismatch rejection
- invalid `from` rejection
- retry succeeds after transient failures
- retry returns final error after attempts are exhausted
- duplicate message rejection
- stale sequence rejection
- guarded receiver skips duplicate/stale messages

Membership tests:

- membership table transitions to `suspect` and `dead`
- timeout transitions
- self member is not timed out
- unknown missed endpoint does not create a placeholder
- local endpoint equivalence checks port as well as host
- integration tests for join visibility and dead-node detection

Verification commands run successfully through Docker fallback:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\go-test.ps1
powershell -ExecutionPolicy Bypass -File scripts\go-test.ps1 -Integration
```

`go` and `gofmt` were not available in the local PowerShell PATH, so formatting
and tests were run with the `golang:1.24` Docker image.

## Current Step 4 Status

Step 4 is complete at the component level:

- transport abstraction: complete
- JSON codec: complete
- envelope validation and checksum verification: complete
- UDP max frame size reject policy: complete
- retry/backoff wrapper: complete
- duplicate suppression cache: complete
- seen-message TTL cache: complete
- per-sender sequence monotonicity guard: complete
- transport unit tests: complete

Step 4 is not yet fully applied to all runtime paths:

- membership uses the envelope codec and UDP frame transport
- membership does not yet use `MessageGuard` because restart/incarnation
  semantics must be finalized first
- aggregation gossip runtime now uses the sender-side wrappers for `StateDelta`
  dissemination; the receive side enters through membership listener dispatch

## Step 6 Runtime Wiring

Step 6 now has a runtime path for `StateDelta` messages.

Implemented in this pass:

- added `internal/gossip/delta.Runtime`
  - consumes outbound deltas from `pipeline.Manager.NextOutbound`
  - wraps local deltas in `StateDelta` envelopes
  - sends envelopes to sampled peers through `transport.Sender`
  - accepts incoming `StateDelta` envelopes
  - applies decoded deltas through `Manager.ApplyReceivedDelta`
  - forwards received deltas only when merge returns `advanced=true`
- extended membership listener dispatch
  - `Ping` remains handled by membership
  - non-`Ping` envelopes are delegated to an optional envelope handler
  - dispatch is asynchronous so delta forwarding does not block membership pings
- wired application startup
  - aggregation pipeline is created from config
  - delta runtime uses `EncodingSender` + `RetryingSender`
  - membership listener delegates `StateDelta` envelopes to the delta runtime
- added HTTP aggregate endpoints for smoke testing
  - `POST /update`
  - `GET /aggregate/sum`
  - `GET /aggregate/topk?k=...`
- added runtime configuration knobs
  - `TOPK_MAX`
  - `OUTBOUND_QUEUE_SIZE`

Tests added:

- aggregate API update/read tests
- delta runtime outbound send test
- delta runtime forward-on-advancement and duplicate suppression test

Verification status:

- Docker Desktop was started locally and Docker engine version `28.0.1`
  responded.
- `docker compose -f docker-compose.yml up -d --build` completed successfully.
- All three local containers were running:
  - `gossip-local-node1` on API port `18080`
  - `gossip-local-node2` on API port `18081`
  - `gossip-local-node3` on API port `18082`
- `GET /healthz` and `GET /readyz` returned successful responses on all nodes.
- `GET /members` showed all three members as `alive` on all nodes.
- A `SUM` update sent to node1 converged to value `5` on all nodes.
- A `TOPK` update sent to node2 converged to the same top item on all nodes.
- `gofmt` was run through the `golang:1.24` Docker image.
- Unit suite passed through `scripts/go-test.ps1`.
- Integration suite passed through `scripts/go-test.ps1 -Integration`.

## Remaining Design Work

Recommended next decisions before Step 7:

- use `incarnation` in membership to distinguish node restarts from stale
  messages
- decide whether envelope `seq` is persisted or reset per incarnation
- disseminate membership entries through gossip messages instead of probing
  only configured seeds/peers
- validate Docker Compose multi-node convergence for `SUM` and `TOPK`
- decide whether the delta runtime inbound path should use a bounded queue
  instead of direct asynchronous callback dispatch

## Step 7 Anti-Entropy and Snapshot Sync

Step 7 adds repair for missed aggregate gossip messages.

Implemented:

- `protocol.StateDigest`
  - exposes `SUM` and `TOPK` aggregate versions
  - includes SHA-256 checksums of serialized aggregate state
  - keeps `membership_version` for future membership digest integration
- `protocol.StateDelta`
  - carries `origin_node_id` and a dedicated per-origin `delta_sequence`
- `protocol.DeltaRangeReq` / `protocol.DeltaRangeResp`
  - request and return missing contiguous delta sequence ranges
  - cap each requested origin range at 256 deltas
- `protocol.SnapshotReq`
  - requests selected aggregate types
  - carries the requester's known versions/digest
- `protocol.SnapshotResp`
  - carries selected versioned serialized aggregate snapshots
  - includes `snapshot_version`, `created_at`, and covered delta watermarks
- pipeline digest/snapshot APIs
  - `Manager.Digest`
  - `Manager.Snapshot`
  - `Manager.ApplySnapshot`
- safe snapshot application
  - decoded snapshot fragments are merged into local CRDT state
  - local state is not blindly replaced by peer snapshots
- runtime anti-entropy
  - periodic digest broadcast
  - delta-range request when peer has a higher per-origin watermark
  - bounded in-memory history, configured by `DELTA_HISTORY_SIZE`
  - snapshot fallback when a requested range is unavailable
  - snapshot request when same-version checksum differs
  - snapshot response handling
  - fresh digest broadcast after snapshot advancement

Important design choice:

- the delta history is intentionally in memory and bounded per origin
- history loss or eviction is repaired by the existing CRDT snapshot fallback
- Step 8 can persist the same sequenced deltas in a WAL without changing the
  range-repair protocol

Tests added:

- snapshot merge preserves local unseen `SUM` contribution
- runtime requests snapshots for divergent digests
- runtime applies snapshot responses
- runtime anti-entropy heals a dropped-delta scenario
- range eviction falls back to snapshot repair
- a temporary network partition heals through delta-range exchange

## Step 8 Persistence and Crash Recovery

Step 8 adds node-local durable storage under `DATA_DIR`:

- `internal/storage/wal`
  - checksummed append-only mutation records
  - monotonic record indexes
  - `always`, `batch`, and `none` fsync modes
  - crash-tail truncation with corruption rejection for complete records
- `internal/storage/snapshot`
  - atomic temporary-write + fsync + rename publication
  - schema, covered WAL index, creation time, and SHA-256 metadata
- `internal/storage.Store`
  - journals deltas and merged snapshots
  - creates checkpoints at an exact WAL boundary
  - returns checkpoint plus WAL tail for recovery
- pipeline integration
  - persistence errors roll mutations back
  - recovery bypasses re-journaling
  - local delta sequence survives restart
- application integration
  - recovery completes before listeners start
  - periodic and graceful-shutdown checkpoints
- Docker integration
  - one named data volume per node
  - non-root runtime owns `/var/lib/gossip`

Verification includes storage unit tests, application recovery tests, and a
Docker kill/restart/rejoin workflow in `scripts/test-crash-recovery.ps1`.

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
- aggregation gossip runtime does not exist yet, so Step 4 wrappers are ready
  for future Step 6 integration

## Remaining Design Work

Recommended next decisions before Step 5/6:

- use `incarnation` in membership to distinguish node restarts from stale
  messages
- decide whether envelope `seq` is persisted or reset per incarnation
- disseminate membership entries through gossip messages instead of probing
  only configured seeds/peers
- wire `RetryingSender` and `GuardedReceiver` into the future aggregate gossip
  receive/send pipeline

# Gossip Transport Interfaces

This package defines the minimal contracts for message exchange in the gossip layer.
It does not impose any specific network protocol.

## Goals

- Keep upper layers independent from transport details.
- Make reliability policies composable (retry/backoff, dedup, anti-loop).
- Enable testability with in-memory adapters.

## Interfaces

- `Sender`:
  Sends one `Envelope` to a peer endpoint.
  Implementations must honor `context.Context`.

- `Receiver`:
  Provides a pull-based stream (`Next`) of incoming envelopes.
  `Close` stops the receiver and unblocks pending calls.

- `Codec`:
  Transforms `Envelope` to/from wire bytes.
  It is the right place for frame-level checks and decode-time validation.

- `FrameSender`:
  Sends raw wire bytes (`[]byte`) to a peer.

- `FrameReceiver`:
  Receives raw wire bytes from peers.
  Returns both `peer` and `frame` so network-level metadata is preserved.

## Envelope Scope

`Envelope` contains protocol metadata (`Type`, `Seq`, `Timestamp`, `From`, `Checksum`)
and a raw `Payload`.

Field semantics are owned by the protocol specification in
`PROTOCOL_SPEC.md`; this package only defines transport-facing shape.

## Design Notes

- Retry policy is not part of `Sender` to keep interfaces small and adaptable.
- Duplicate suppression / seen-message cache is a higher-level concern and
  should wrap `Receiver` or message-processing logic.
- Max-size checks can be implemented in `Codec.Decode` or transport adapters
  before decoding.

## Step 4.1 Logic Implemented

Step 4.1 requires transport abstraction with sender, receiver and pluggable codec.
The package now implements this with two layers:

- Byte transport layer:
  `FrameSender` and `FrameReceiver` define network I/O contracts without
  binding to JSON/protobuf.

- Gossip message layer:
  `Sender` and `Receiver` operate on `Envelope`.

The bridge between them is implemented by adapters in `adapters.go`:

- `EncodingSender`:
  takes an `Envelope`, calls `Codec.Encode`, then forwards bytes through
  `FrameSender.SendFrame`.

- `DecodingReceiver`:
  pulls bytes from `FrameReceiver.NextFrame`, calls `Codec.Decode`, then
  returns a validated `Envelope`.

`udp.go` provides the current UDP frame transport used by membership bootstrap:

- `UDPFrameTransport`:
  binds a UDP packet socket, sends raw frames to `host:port` peers, receives
  raw frames with peer metadata, and rejects frames larger than the configured
  maximum size.

Reliability and anti-loop helpers live in `reliability.go`:

- `RetryingSender`:
  wraps a `Sender` and retries failed sends with bounded exponential backoff.

- `MessageGuard`:
  tracks seen `(from, seq)` pairs for a TTL and keeps a per-sender high-water
  sequence number.

- `GuardedReceiver`:
  wraps a `Receiver` and skips duplicate or stale messages before returning the
  next accepted envelope.

This keeps responsibilities separated:

- networking handles delivery of bytes;
- codec handles serialization/validation;
- gossip layer handles protocol semantics.

As a consequence, changing codec (JSON -> protobuf) does not require changing
network transport implementations, and changing transport (UDP -> HTTP) does
not require changing gossip business logic.

## Step 4.2 Logic Implemented

Step 4.2 requires message encoding/decoding, envelope validation and checksum
verification. The package now includes `JSONCodec` (`json_codec.go`).

Encoding behavior:

- codec defaults `version` to `v1` when omitted
- codec auto-computes checksum when missing
- envelope is validated before marshal
- output frame is JSON with protocol fields (`type`, `seq`, `timestamp`,
  `checksum`, `from`, `trace_id`, `correlation_id`, `version`, `payload`)

Decoding behavior:

- frame is unmarshaled from JSON
- `version` defaults to `v1` when missing
- envelope validation is applied
- checksum is recomputed from payload and compared with envelope checksum
- mismatch returns `ErrChecksumMismatch`

Validation rules currently enforced (`validation.go`):

- allowed message types only: `Ping`, `StateDigest`, `StateDelta`, `Ack`,
  `SnapshotReq`, `SnapshotResp`
- required fields present: `type`, `from`, `timestamp`, `checksum`, `payload`
- `from` must satisfy node id regex from protocol spec
- `timestamp` must be RFC3339/RFC3339Nano parseable
- `checksum` must be 64-char hex SHA-256 string
- UDP frames larger than `DefaultMaxFrameSize` are rejected by the UDP adapter
- duplicate `(from, seq)` messages are rejected by `MessageGuard`
- non-monotonic per-sender sequences are rejected by `MessageGuard`

Unit tests added (`json_codec_test.go`):

- serialization roundtrip (`Encode` -> `Decode`)
- checksum mismatch rejection
- malformed sender id rejection

Reliability tests added (`reliability_test.go`):

- retry succeeds after transient send failures
- retry returns the final send error after attempts are exhausted
- duplicate message rejection
- stale sequence rejection
- guarded receiver skips duplicate/stale messages

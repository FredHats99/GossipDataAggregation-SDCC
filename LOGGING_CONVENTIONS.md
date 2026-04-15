# Logging Conventions

This document defines logging conventions for multi-peer gossip operation.

## 1) Goals

- make cross-peer debugging practical
- trace one logical operation across many nodes
- keep logs machine-parseable and low-noise

## 2) Log Format

- default format: JSON (one object per line)
- output target: stdout/stderr (container runtime collects it)
- timestamp: UTC RFC3339 with milliseconds

## 3) Required Fields

Every log entry must include:

- `ts`: event timestamp in UTC (RFC3339)
- `level`: `debug|info|warn|error`
- `event`: stable event name
- `msg`: short human-readable summary
- `node_id`: current node identifier
- `cluster`: cluster name/id
- `service`: service name (e.g. `gossip-node`)
- `build_sha`: build or git revision

## 4) Context Fields (When Available)

- `peer`: remote peer id/host
- `msg_type`: gossip message type
- `seq`: sender sequence number
- `trace_id`: logical request/update trace id
- `correlation_id`: gossip exchange/batch id
- `op`: operation name (`sum_update`, `topk_merge`, etc.)
- `duration_ms`: elapsed processing time
- `outcome`: `success|failure|retry|dropped|noop`
- `reason`: short failure/drop reason code

## 5) Event Taxonomy

Use fixed event names. Do not invent ad-hoc names at call sites.

- `api_update_received`
- `local_update_applied`
- `gossip_send`
- `gossip_recv`
- `gossip_drop`
- `merge_applied`
- `merge_noop`
- `anti_entropy_round_start`
- `anti_entropy_round_end`
- `snapshot_request_sent`
- `snapshot_received`
- `membership_state_changed`
- `queue_depth_sample`
- `queue_drop`
- `transport_retry_scheduled`
- `transport_send_failed`

## 6) Correlation Rules Across Peers

1. External ingress creates a `trace_id` if missing.
2. Any gossip dissemination derived from that ingress reuses the same `trace_id`.
3. Each send attempt/batch creates a `correlation_id`.
4. Receiving peer logs `trace_id` and `correlation_id` from envelope.
5. Retries keep the same `correlation_id` and increment retry metadata.

## 7) Log Level Policy

- `debug`: per-message payload metadata, retry internals, merge details
- `info`: lifecycle and success transitions (`gossip_send`, `merge_applied`, membership changes)
- `warn`: recoverable anomalies (timeouts, drops, malformed peer message rejected)
- `error`: failed operations impacting correctness/progress

Guidelines:

- never log full payload bodies by default
- include sizes/hashes instead of raw data when possible
- redact secrets/tokens/credentials always

## 8) Sampling and Volume Control

- no sampling for `warn`/`error`
- optional sampling for high-frequency `info` success events
- never sample state transition events (`membership_state_changed`)
- prefer periodic summaries for repetitive events

Suggested control env vars:

- `LOG_LEVEL` (`debug|info|warn|error`)
- `LOG_FORMAT` (`json`)
- `LOG_SAMPLE_SUCCESS_RATE` (`0.0` to `1.0`, default `1.0` in dev, lower in perf runs)

## 9) Errors and Reason Codes

Use stable reason codes in `reason` field, for example:

- `invalid_checksum`
- `decode_failed`
- `duplicate_message`
- `queue_full`
- `peer_unreachable`
- `merge_conflict_resolved`
- `snapshot_apply_failed`

## 10) Example Log Lines

```json
{"ts":"2026-04-15T10:22:31.412Z","level":"info","event":"gossip_send","msg":"State delta sent","node_id":"node1","cluster":"gossip-local","service":"gossip-node","build_sha":"a1b2c3d","peer":"node2","msg_type":"StateDelta","seq":1842,"trace_id":"tr_7d9e","correlation_id":"corr_91aa","op":"sum_update","outcome":"success","duration_ms":4}
```

```json
{"ts":"2026-04-15T10:22:31.438Z","level":"info","event":"gossip_recv","msg":"State delta received","node_id":"node2","cluster":"gossip-local","service":"gossip-node","build_sha":"a1b2c3d","peer":"node1","msg_type":"StateDelta","seq":1842,"trace_id":"tr_7d9e","correlation_id":"corr_91aa","outcome":"success"}
```

```json
{"ts":"2026-04-15T10:22:31.451Z","level":"warn","event":"gossip_drop","msg":"Message dropped due to duplicate cache hit","node_id":"node2","cluster":"gossip-local","service":"gossip-node","build_sha":"a1b2c3d","peer":"node1","msg_type":"StateDelta","seq":1842,"trace_id":"tr_7d9e","correlation_id":"corr_91aa","outcome":"dropped","reason":"duplicate_message"}
```

## 11) Implementation Checklist

- emit JSON logs only
- ensure `trace_id`/`correlation_id` propagation in protocol envelope
- enforce event-name constants in code
- add tests for required fields presence on critical events
- document query examples in observability dashboards

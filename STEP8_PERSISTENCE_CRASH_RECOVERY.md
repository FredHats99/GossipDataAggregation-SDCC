# Step 8 Persistence and Crash Recovery

This document records the durability and startup recovery design for aggregate
state.

## Storage Layout

Each node owns an independent data directory:

```text
DATA_DIR/
  wal/updates.wal
  snapshots/snapshot-<wal-index>.json
```

Docker Compose mounts a named volume for each node at `/var/lib/gossip`. The
runtime user (`appuser`, UID 10001) owns that directory.

## Write-Ahead Log

The WAL is an append-only newline-delimited JSON stream. Every record contains:

- monotonic `index`
- mutation `type` (`state_delta` or `state_snapshot`)
- mutation payload
- UTC creation time
- SHA-256 checksum over index, type, and payload

Effective local updates, received deltas, and merged snapshot responses are
journaled before the mutation becomes externally visible or is forwarded.
Persistence failure rolls the in-memory mutation and local delta sequence back.

The final incomplete line is treated as a crash tail: startup keeps the valid
prefix, truncates the incomplete bytes, and resumes appending at the next index.
A corrupt complete record fails startup instead of silently discarding data.

## Fsync Policy

`WAL_FSYNC_MODE` supports:

- `always` (default): sync every acknowledged mutation
- `batch`: sync after `WAL_FSYNC_BATCH_SIZE` records; bounded recent-loss window
- `none`: rely on operating-system flushing; intended only for disposable tests

Shutdown and checkpoint creation always force a sync regardless of policy.

## Snapshots

The application creates a full `SUM` + `TOPK` checkpoint every
`SNAPSHOT_INTERVAL_SECONDS` and once during graceful shutdown.

Snapshots are written to a temporary file, fsynced, and atomically renamed.
Metadata contains:

- schema version
- highest WAL index covered
- creation time
- SHA-256 payload checksum

The node-local delta sequence is stored with aggregate state so it remains
monotonic after restart.

## Startup Recovery

Startup performs these steps before opening network/API listeners:

1. validate and load the newest snapshot
2. restore serialized aggregate state directly
3. replay WAL records after the snapshot's covered index
4. restore the node-local delta sequence
5. attach the live journal and start gossip

Replayed mutations use CRDT merge rules and are not appended to the WAL again.

## Verification

Automated Go tests cover:

- append/reopen/replay
- batch fsync behavior
- truncated-tail recovery followed by new appends
- complete-record corruption rejection
- atomic snapshot save/load and checksum rejection
- checkpoint plus WAL-tail recovery for SUM and TOP-K
- rollback when persistence append fails
- delta sequence continuity after recovery

Docker crash verification:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/test-crash-recovery.ps1
```

The test kills `node1`, restarts it without a network, verifies state recovered
only from its volume, reconnects it, and asserts cluster convergence.

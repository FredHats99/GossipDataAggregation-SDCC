package storage

import (
	"encoding/json"
	"testing"
	"time"

	"gossipdataaggregation-sdcc/internal/gossip/protocol"
	"gossipdataaggregation-sdcc/internal/storage/wal"
)

func TestStoreRecoversCheckpointAndWALTail(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(Config{DataDir: dir, WALSyncMode: wal.SyncAlways})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	delta1 := sumDelta(t, 1, 3)
	if err := store.AppendDelta(delta1); err != nil {
		t.Fatalf("append first delta: %v", err)
	}
	checkpoint := protocol.SnapshotResp{
		SnapshotVersion: 1,
		SUMState:        json.RawMessage(`{"aggregate_type":"SUM"}`),
		TOPKState:       json.RawMessage(`{"aggregate_type":"TOPK"}`),
		CreatedAt:       time.Now().UTC(),
	}
	if err := store.SaveCheckpoint(checkpoint); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	delta2 := sumDelta(t, 2, 7)
	if err := store.AppendDelta(delta2); err != nil {
		t.Fatalf("append WAL tail: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := Open(Config{DataDir: dir, WALSyncMode: wal.SyncAlways})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer reopened.Close()
	recovery, err := reopened.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if recovery.Checkpoint == nil || recovery.Checkpoint.SnapshotVersion != 1 {
		t.Fatalf("unexpected checkpoint: %+v", recovery.Checkpoint)
	}
	if len(recovery.Mutations) != 1 || recovery.Mutations[0].Delta == nil || recovery.Mutations[0].Delta.DeltaSequence != 2 {
		t.Fatalf("unexpected WAL tail: %+v", recovery.Mutations)
	}
}

func sumDelta(t *testing.T, sequence, value uint64) protocol.StateDelta {
	t.Helper()
	delta, err := protocol.NewSUMStateDelta(sequence, protocol.SUMDelta{NodeID: "node1", Value: value})
	if err != nil {
		t.Fatalf("new SUM delta: %v", err)
	}
	delta.DeltaSequence = sequence
	return delta
}

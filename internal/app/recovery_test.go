package app

import (
	"testing"
	"time"

	"gossipdataaggregation-sdcc/internal/aggregation/common"
	"gossipdataaggregation-sdcc/internal/aggregation/pipeline"
	topkagg "gossipdataaggregation-sdcc/internal/aggregation/topk"
	"gossipdataaggregation-sdcc/internal/storage"
	"gossipdataaggregation-sdcc/internal/storage/wal"
)

func TestAggregationRecoveryLoadsCheckpointAndReplaysWALTail(t *testing.T) {
	dir := t.TempDir()
	manager := recoveryManager(t)
	store, err := storage.Open(storage.Config{DataDir: dir, WALSyncMode: wal.SyncAlways})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	manager.SetJournal(store)

	applyRecoveryUpdate(t, manager, pipeline.LocalUpdate{AggregateType: common.AggregateSUM, Value: uint64(5)})
	applyRecoveryUpdate(t, manager, pipeline.LocalUpdate{
		AggregateType: common.AggregateTOPK,
		Value: topkagg.Candidate{
			ItemID:  "durable-item",
			Score:   50,
			EventTS: time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC),
		},
	})
	if err := manager.Checkpoint(store); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	applyRecoveryUpdate(t, manager, pipeline.LocalUpdate{AggregateType: common.AggregateSUM, Value: uint64(7)})
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	recoveredManager := recoveryManager(t)
	reopened, err := storage.Open(storage.Config{DataDir: dir, WALSyncMode: wal.SyncAlways})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer reopened.Close()
	if err := recoverAggregation(recoveredManager, reopened); err != nil {
		t.Fatalf("recover aggregation: %v", err)
	}
	recoveredManager.SetJournal(reopened)

	estimates, err := recoveredManager.Estimates(3)
	if err != nil {
		t.Fatalf("recovered estimates: %v", err)
	}
	if estimates.SUM != 12 || len(estimates.TOPK) != 1 || estimates.TOPK[0].ItemID != "durable-item" {
		t.Fatalf("unexpected recovered state: %+v", estimates)
	}
	delta, advanced, err := recoveredManager.ApplyLocalUpdate(pipeline.LocalUpdate{
		AggregateType: common.AggregateSUM,
		Value:         uint64(1),
	})
	if err != nil || !advanced {
		t.Fatalf("post-recovery update advanced=%v err=%v", advanced, err)
	}
	if delta.DeltaSequence != 4 {
		t.Fatalf("expected recovered delta sequence 4, got %d", delta.DeltaSequence)
	}
}

func recoveryManager(t *testing.T) *pipeline.Manager {
	t.Helper()
	manager, err := pipeline.New(pipeline.Config{NodeID: "node1", TopKMax: 3, OutboundQueueSize: 16})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	return manager
}

func applyRecoveryUpdate(t *testing.T, manager *pipeline.Manager, update pipeline.LocalUpdate) {
	t.Helper()
	if _, advanced, err := manager.ApplyLocalUpdate(update); err != nil || !advanced {
		t.Fatalf("apply update advanced=%v err=%v", advanced, err)
	}
}

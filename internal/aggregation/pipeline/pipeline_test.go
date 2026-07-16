package pipeline

import (
	"context"
	"reflect"
	"testing"
	"time"

	"gossipdataaggregation-sdcc/internal/aggregation/common"
	topkagg "gossipdataaggregation-sdcc/internal/aggregation/topk"
	"gossipdataaggregation-sdcc/internal/gossip/protocol"
)

func TestManagerLocalUpdateEmitsDeltas(t *testing.T) {
	manager := newManager(t, "node1", 3, 10)

	sumDelta, advanced, err := manager.ApplyLocalUpdate(LocalUpdate{
		AggregateType: common.AggregateSUM,
		Value:         uint64(5),
	})
	if err != nil || !advanced {
		t.Fatalf("sum update failed advanced=%v err=%v", advanced, err)
	}
	if sumDelta.AggregateType != common.AggregateSUM {
		t.Fatalf("expected SUM delta, got %s", sumDelta.AggregateType)
	}
	if sumDelta.OriginNodeID != "node1" || sumDelta.DeltaSequence != 1 {
		t.Fatalf("unexpected SUM delta sequence metadata: %+v", sumDelta)
	}

	topkDelta, advanced, err := manager.ApplyLocalUpdate(LocalUpdate{
		AggregateType: common.AggregateTOPK,
		Value: topkagg.Candidate{
			ItemID:  "item-a",
			Score:   10,
			EventTS: mustTime("2026-05-05T10:00:00Z"),
		},
	})
	if err != nil || !advanced {
		t.Fatalf("topk update failed advanced=%v err=%v", advanced, err)
	}
	if topkDelta.AggregateType != common.AggregateTOPK {
		t.Fatalf("expected TOPK delta, got %s", topkDelta.AggregateType)
	}
	if topkDelta.OriginNodeID != "node1" || topkDelta.DeltaSequence != 2 {
		t.Fatalf("unexpected TOPK delta sequence metadata: %+v", topkDelta)
	}

	if got := manager.DrainOutbound(); len(got) != 2 {
		t.Fatalf("expected 2 outbound deltas, got %d", len(got))
	}
}

func TestManagerApplyReceivedDeltaConvergesSUMAndTOPK(t *testing.T) {
	node1 := newManager(t, "node1", 3, 10)
	node2 := newManager(t, "node2", 3, 10)

	sumDelta, _, err := node1.ApplyLocalUpdate(LocalUpdate{
		AggregateType: common.AggregateSUM,
		Value:         uint64(7),
	})
	if err != nil {
		t.Fatalf("sum update: %v", err)
	}
	topkDelta, _, err := node1.ApplyLocalUpdate(LocalUpdate{
		AggregateType: common.AggregateTOPK,
		Value: topkagg.Candidate{
			ItemID:  "item-a",
			Score:   42,
			EventTS: mustTime("2026-05-05T10:00:00Z"),
		},
	})
	if err != nil {
		t.Fatalf("topk update: %v", err)
	}

	if advanced, err := node2.ApplyReceivedDelta(sumDelta); err != nil || !advanced {
		t.Fatalf("sum delta merge failed advanced=%v err=%v", advanced, err)
	}
	if advanced, err := node2.ApplyReceivedDelta(topkDelta); err != nil || !advanced {
		t.Fatalf("topk delta merge failed advanced=%v err=%v", advanced, err)
	}
	if advanced, err := node2.ApplyReceivedDelta(sumDelta); err != nil || advanced {
		t.Fatalf("duplicate sum delta should not advance advanced=%v err=%v", advanced, err)
	}

	estimates, err := node2.Estimates(3)
	if err != nil {
		t.Fatalf("estimates: %v", err)
	}
	if estimates.SUM != 7 {
		t.Fatalf("expected SUM 7, got %d", estimates.SUM)
	}
	if len(estimates.TOPK) != 1 || estimates.TOPK[0].OriginNodeID != "node1" {
		t.Fatalf("unexpected TOPK estimate: %+v", estimates.TOPK)
	}
}

func TestManagerBackpressureDropsWhenOutboundQueueFull(t *testing.T) {
	manager := newManager(t, "node1", 3, 1)

	for i := 0; i < 3; i++ {
		if _, _, err := manager.ApplyLocalUpdate(LocalUpdate{
			AggregateType: common.AggregateSUM,
			Value:         uint64(1),
		}); err != nil {
			t.Fatalf("update %d: %v", i, err)
		}
	}

	if got := len(manager.DrainOutbound()); got != 1 {
		t.Fatalf("expected 1 queued delta, got %d", got)
	}
	if manager.DroppedOutbound() != 2 {
		t.Fatalf("expected 2 dropped deltas, got %d", manager.DroppedOutbound())
	}
}

func TestManagerNextOutboundHonorsContext(t *testing.T) {
	manager := newManager(t, "node1", 3, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := manager.NextOutbound(ctx); err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestManagersConvergeWithBroadcastDeltas(t *testing.T) {
	nodes := []*Manager{
		newManager(t, "node1", 4, 20),
		newManager(t, "node2", 4, 20),
		newManager(t, "node3", 4, 20),
	}

	updates := []struct {
		node   int
		update LocalUpdate
	}{
		{0, LocalUpdate{AggregateType: common.AggregateSUM, Value: uint64(5)}},
		{1, LocalUpdate{AggregateType: common.AggregateSUM, Value: uint64(7)}},
		{2, LocalUpdate{AggregateType: common.AggregateSUM, Value: uint64(11)}},
		{0, LocalUpdate{AggregateType: common.AggregateTOPK, Value: topkagg.Candidate{ItemID: "item-a", Score: 10, EventTS: mustTime("2026-05-05T10:00:00Z")}}},
		{1, LocalUpdate{AggregateType: common.AggregateTOPK, Value: topkagg.Candidate{ItemID: "item-b", Score: 30, EventTS: mustTime("2026-05-05T10:00:00Z")}}},
		{2, LocalUpdate{AggregateType: common.AggregateTOPK, Value: topkagg.Candidate{ItemID: "item-c", Score: 20, EventTS: mustTime("2026-05-05T10:00:00Z")}}},
	}

	for _, update := range updates {
		if _, _, err := nodes[update.node].ApplyLocalUpdate(update.update); err != nil {
			t.Fatalf("local update on node %d: %v", update.node, err)
		}
	}

	var deltas []anyDelta
	for idx, node := range nodes {
		for _, delta := range node.DrainOutbound() {
			deltas = append(deltas, anyDelta{origin: idx, delta: delta})
		}
	}
	for _, item := range deltas {
		for idx, node := range nodes {
			if idx == item.origin {
				continue
			}
			if _, err := node.ApplyReceivedDelta(item.delta); err != nil {
				t.Fatalf("merge delta on node %d: %v", idx, err)
			}
		}
	}

	first, err := nodes[0].Estimates(3)
	if err != nil {
		t.Fatalf("node0 estimates: %v", err)
	}
	for idx, node := range nodes[1:] {
		got, err := node.Estimates(3)
		if err != nil {
			t.Fatalf("node%d estimates: %v", idx+1, err)
		}
		if got.SUM != first.SUM {
			t.Fatalf("SUM mismatch: got %d want %d", got.SUM, first.SUM)
		}
		if !reflect.DeepEqual(got.TOPK, first.TOPK) {
			t.Fatalf("TOPK mismatch: got %+v want %+v", got.TOPK, first.TOPK)
		}
	}
	if first.SUM != 23 {
		t.Fatalf("expected converged SUM 23, got %d", first.SUM)
	}
	if got := itemIDs(first.TOPK); !reflect.DeepEqual(got, []string{"item-b", "item-c", "item-a"}) {
		t.Fatalf("unexpected TOPK order: %v", got)
	}
}

func TestManagerSnapshotMergeHealsMissingState(t *testing.T) {
	source := newManager(t, "node1", 3, 8)
	target := newManager(t, "node2", 3, 8)

	if _, advanced, err := source.ApplyLocalUpdate(LocalUpdate{
		AggregateType: common.AggregateSUM,
		Value:         uint64(11),
	}); err != nil || !advanced {
		t.Fatalf("apply source SUM update advanced=%v err=%v", advanced, err)
	}
	if _, advanced, err := target.ApplyLocalUpdate(LocalUpdate{
		AggregateType: common.AggregateSUM,
		Value:         uint64(7),
	}); err != nil || !advanced {
		t.Fatalf("apply target SUM update advanced=%v err=%v", advanced, err)
	}

	snapshot, err := source.Snapshot([]string{common.AggregateSUM})
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	advanced, err := target.ApplySnapshot(snapshot)
	if err != nil {
		t.Fatalf("apply snapshot: %v", err)
	}
	if !advanced {
		t.Fatal("expected snapshot to advance target")
	}

	estimates, err := target.Estimates(3)
	if err != nil {
		t.Fatalf("target estimates: %v", err)
	}
	if estimates.SUM != 18 {
		t.Fatalf("expected merged SUM 18, got %d", estimates.SUM)
	}
}

type anyDelta struct {
	origin int
	delta  protocol.StateDelta
}

func newManager(t *testing.T, nodeID string, kmax int, queueSize int) *Manager {
	t.Helper()
	manager, err := New(Config{
		NodeID:            nodeID,
		TopKMax:           kmax,
		OutboundQueueSize: queueSize,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	return manager
}

func mustTime(raw string) time.Time {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		panic(err)
	}
	return parsed
}

func itemIDs(candidates []topkagg.Candidate) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.ItemID)
	}
	return out
}

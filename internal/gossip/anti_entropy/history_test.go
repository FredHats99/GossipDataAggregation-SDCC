package antientropy

import (
	"testing"

	"gossipdataaggregation-sdcc/internal/gossip/protocol"
)

func TestDeltaHistoryAdvancesContiguousWatermarkAndServesRange(t *testing.T) {
	history, err := NewDeltaHistory(8)
	if err != nil {
		t.Fatalf("new history: %v", err)
	}

	if err := history.Record(sumDeltaForHistory(t, "node1", 2, 2)); err != nil {
		t.Fatalf("record sequence 2: %v", err)
	}
	if got := history.Watermarks()["node1"]; got != 0 {
		t.Fatalf("expected gap to keep watermark at 0, got %d", got)
	}
	if err := history.Record(sumDeltaForHistory(t, "node1", 1, 1)); err != nil {
		t.Fatalf("record sequence 1: %v", err)
	}
	if got := history.Watermarks()["node1"]; got != 2 {
		t.Fatalf("expected contiguous watermark 2, got %d", got)
	}

	deltas, ok := history.Range(protocol.DeltaRange{OriginNodeID: "node1", FromSequence: 1, ToSequence: 2})
	if !ok || len(deltas) != 2 {
		t.Fatalf("expected complete range, ok=%v count=%d", ok, len(deltas))
	}
}

func TestDeltaHistoryReportsEvictedRangeUnavailable(t *testing.T) {
	history, err := NewDeltaHistory(1)
	if err != nil {
		t.Fatalf("new history: %v", err)
	}
	for sequence := uint64(1); sequence <= 2; sequence++ {
		if err := history.Record(sumDeltaForHistory(t, "node1", sequence, sequence)); err != nil {
			t.Fatalf("record sequence %d: %v", sequence, err)
		}
	}

	if _, ok := history.Range(protocol.DeltaRange{OriginNodeID: "node1", FromSequence: 1, ToSequence: 2}); ok {
		t.Fatal("expected evicted range to be unavailable")
	}
}

func TestDeltaHistoryAdvancesWatermarkFromSnapshot(t *testing.T) {
	history, err := NewDeltaHistory(4)
	if err != nil {
		t.Fatalf("new history: %v", err)
	}
	history.AdvanceWatermarks(map[string]uint64{"node1": 7})
	if got := history.Watermarks()["node1"]; got != 7 {
		t.Fatalf("expected snapshot watermark 7, got %d", got)
	}
}

func sumDeltaForHistory(t *testing.T, nodeID string, sequence, value uint64) protocol.StateDelta {
	t.Helper()
	delta, err := protocol.NewSUMStateDelta(sequence, protocol.SUMDelta{NodeID: nodeID, Value: value})
	if err != nil {
		t.Fatalf("new SUM delta: %v", err)
	}
	delta.DeltaSequence = sequence
	return delta
}

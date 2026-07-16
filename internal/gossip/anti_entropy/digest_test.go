package antientropy

import (
	"reflect"
	"testing"

	"gossipdataaggregation-sdcc/internal/aggregation/common"
	"gossipdataaggregation-sdcc/internal/gossip/protocol"
)

func TestAggregatesNeedingSnapshotRequestsPeerAhead(t *testing.T) {
	local := protocol.StateDigest{
		SUM:  protocol.AggregateDigest{Version: 1, Checksum: "a"},
		TOPK: protocol.AggregateDigest{Version: 1, Checksum: "b"},
	}
	peer := protocol.StateDigest{
		SUM:  protocol.AggregateDigest{Version: 2, Checksum: "c"},
		TOPK: protocol.AggregateDigest{Version: 1, Checksum: "b"},
	}

	got := AggregatesNeedingSnapshot(local, peer)
	if !reflect.DeepEqual(got, []string{common.AggregateSUM}) {
		t.Fatalf("unexpected aggregates: %v", got)
	}
}

func TestAggregatesNeedingSnapshotRequestsSameVersionDivergence(t *testing.T) {
	local := protocol.StateDigest{
		SUM:  protocol.AggregateDigest{Version: 1, Checksum: "a"},
		TOPK: protocol.AggregateDigest{Version: 3, Checksum: "b"},
	}
	peer := protocol.StateDigest{
		SUM:  protocol.AggregateDigest{Version: 1, Checksum: "a"},
		TOPK: protocol.AggregateDigest{Version: 3, Checksum: "c"},
	}

	got := AggregatesNeedingSnapshot(local, peer)
	if !reflect.DeepEqual(got, []string{common.AggregateTOPK}) {
		t.Fatalf("unexpected aggregates: %v", got)
	}
}

func TestAggregatesNeedingSnapshotIgnoresPeerBehind(t *testing.T) {
	local := protocol.StateDigest{
		SUM: protocol.AggregateDigest{Version: 3, Checksum: "a"},
	}
	peer := protocol.StateDigest{
		SUM: protocol.AggregateDigest{Version: 2, Checksum: "b"},
	}

	if got := AggregatesNeedingSnapshot(local, peer); len(got) != 0 {
		t.Fatalf("expected no snapshot request for peer behind, got %v", got)
	}
}

func TestMissingDeltaRangesUsesContiguousWatermarksAndBoundsRequest(t *testing.T) {
	local := protocol.StateDigest{DeltaSequences: map[string]uint64{"node1": 3}}
	peer := protocol.StateDigest{DeltaSequences: map[string]uint64{
		"node2": 2,
		"node1": 3 + protocol.MaxDeltaRangeSize + 5,
	}}

	got := MissingDeltaRanges(local, peer)
	want := []protocol.DeltaRange{
		{OriginNodeID: "node1", FromSequence: 4, ToSequence: 3 + protocol.MaxDeltaRangeSize},
		{OriginNodeID: "node2", FromSequence: 1, ToSequence: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected ranges: got %+v want %+v", got, want)
	}
}

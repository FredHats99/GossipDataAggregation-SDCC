package antientropy

import (
	"sort"

	"gossipdataaggregation-sdcc/internal/aggregation/common"
	"gossipdataaggregation-sdcc/internal/gossip/protocol"
)

func MissingDeltaRanges(local, peer protocol.StateDigest) []protocol.DeltaRange {
	origins := make([]string, 0, len(peer.DeltaSequences))
	for originNodeID, peerSequence := range peer.DeltaSequences {
		if peerSequence > local.DeltaSequences[originNodeID] {
			origins = append(origins, originNodeID)
		}
	}
	sort.Strings(origins)

	ranges := make([]protocol.DeltaRange, 0, len(origins))
	for _, originNodeID := range origins {
		from := local.DeltaSequences[originNodeID] + 1
		to := peer.DeltaSequences[originNodeID]
		if to-from+1 > protocol.MaxDeltaRangeSize {
			to = from + protocol.MaxDeltaRangeSize - 1
		}
		ranges = append(ranges, protocol.DeltaRange{
			OriginNodeID: originNodeID,
			FromSequence: from,
			ToSequence:   to,
		})
	}
	return ranges
}

func AggregatesNeedingSnapshot(local, peer protocol.StateDigest) []string {
	want := make([]string, 0, 2)
	if digestNeedsSnapshot(local.SUM, peer.SUM) {
		want = append(want, common.AggregateSUM)
	}
	if digestNeedsSnapshot(local.TOPK, peer.TOPK) {
		want = append(want, common.AggregateTOPK)
	}
	return want
}

func digestNeedsSnapshot(local, peer protocol.AggregateDigest) bool {
	if peer.Version > local.Version {
		return true
	}
	return peer.Version == local.Version &&
		peer.Checksum != "" &&
		local.Checksum != "" &&
		peer.Checksum != local.Checksum
}

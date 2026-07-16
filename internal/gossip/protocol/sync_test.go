package protocol

import (
	"encoding/json"
	"testing"
)

func TestDeltaRangeMessagesRoundTrip(t *testing.T) {
	rangeReq, err := NewDeltaRangeReq([]DeltaRange{{
		OriginNodeID: "node1",
		FromSequence: 2,
		ToSequence:   4,
	}}, StateDigest{DeltaSequences: map[string]uint64{"node1": 1}})
	if err != nil {
		t.Fatalf("new range request: %v", err)
	}
	raw, err := json.Marshal(rangeReq)
	if err != nil {
		t.Fatalf("marshal range request: %v", err)
	}
	decodedReq, err := DecodeDeltaRangeReq(raw)
	if err != nil {
		t.Fatalf("decode range request: %v", err)
	}
	if len(decodedReq.Ranges) != 1 || decodedReq.Ranges[0].ToSequence != 4 {
		t.Fatalf("unexpected decoded request: %+v", decodedReq)
	}

	delta, err := NewSUMStateDelta(2, SUMDelta{NodeID: "node1", Value: 9})
	if err != nil {
		t.Fatalf("new SUM delta: %v", err)
	}
	delta.DeltaSequence = 2
	rangeResp, err := NewDeltaRangeResp([]StateDelta{delta})
	if err != nil {
		t.Fatalf("new range response: %v", err)
	}
	raw, err = json.Marshal(rangeResp)
	if err != nil {
		t.Fatalf("marshal range response: %v", err)
	}
	if _, err := DecodeDeltaRangeResp(raw); err != nil {
		t.Fatalf("decode range response: %v", err)
	}
}

func TestDeltaRangeRejectsOversizedOrUnsequencedData(t *testing.T) {
	if _, err := NewDeltaRangeReq([]DeltaRange{{
		OriginNodeID: "node1",
		FromSequence: 1,
		ToSequence:   MaxDeltaRangeSize + 1,
	}}, StateDigest{}); err == nil {
		t.Fatal("expected oversized range to be rejected")
	}

	delta, err := NewSUMStateDelta(1, SUMDelta{NodeID: "node1", Value: 1})
	if err != nil {
		t.Fatalf("new SUM delta: %v", err)
	}
	if _, err := NewDeltaRangeResp([]StateDelta{delta}); err == nil {
		t.Fatal("expected unsequenced delta to be rejected")
	}
}

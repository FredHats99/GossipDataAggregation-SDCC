package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gossipdataaggregation-sdcc/internal/aggregation/common"
)

var (
	ErrInvalidDigest      = errors.New("protocol: invalid state digest")
	ErrInvalidSnapshotReq = errors.New("protocol: invalid snapshot request")
	ErrInvalidSnapshot    = errors.New("protocol: invalid snapshot")
	ErrInvalidDeltaRange  = errors.New("protocol: invalid delta range")
)

const MaxDeltaRangeSize uint64 = 256

type AggregateDigest struct {
	Version  uint64 `json:"version"`
	Checksum string `json:"checksum,omitempty"`
}

type StateDigest struct {
	SUM               AggregateDigest   `json:"sum"`
	TOPK              AggregateDigest   `json:"topk"`
	MembershipVersion uint64            `json:"membership_version"`
	DeltaSequences    map[string]uint64 `json:"delta_sequences,omitempty"`
}

type DeltaRange struct {
	OriginNodeID string `json:"origin_node_id"`
	FromSequence uint64 `json:"from_sequence"`
	ToSequence   uint64 `json:"to_sequence"`
}

type DeltaRangeReq struct {
	Ranges        []DeltaRange `json:"ranges"`
	KnownVersions StateDigest  `json:"known_versions"`
}

type DeltaRangeResp struct {
	Deltas []StateDelta `json:"deltas"`
}

type SnapshotReq struct {
	WantAggregateTypes []string    `json:"want_aggregate_types"`
	KnownVersions      StateDigest `json:"known_versions"`
}

type SnapshotResp struct {
	SnapshotVersion uint64            `json:"snapshot_version"`
	SUMState        json.RawMessage   `json:"sum_state,omitempty"`
	TOPKState       json.RawMessage   `json:"topk_state,omitempty"`
	DeltaSequences  map[string]uint64 `json:"delta_sequences,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
}

func StateChecksum(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func NewSnapshotReq(want []string, known StateDigest) (SnapshotReq, error) {
	if len(want) == 0 {
		return SnapshotReq{}, ErrInvalidSnapshotReq
	}
	seen := make(map[string]struct{}, len(want))
	out := make([]string, 0, len(want))
	for _, aggregateType := range want {
		if aggregateType != common.AggregateSUM && aggregateType != common.AggregateTOPK {
			return SnapshotReq{}, fmt.Errorf("%w: %s", ErrInvalidAggregateType, aggregateType)
		}
		if _, ok := seen[aggregateType]; ok {
			continue
		}
		seen[aggregateType] = struct{}{}
		out = append(out, aggregateType)
	}
	return SnapshotReq{
		WantAggregateTypes: out,
		KnownVersions:      known,
	}, nil
}

func DecodeStateDigest(raw []byte) (StateDigest, error) {
	var digest StateDigest
	if err := json.Unmarshal(raw, &digest); err != nil {
		return StateDigest{}, fmt.Errorf("%w: %v", ErrInvalidDigest, err)
	}
	for nodeID, sequence := range digest.DeltaSequences {
		if !common.ValidNodeID(nodeID) || sequence == 0 {
			return StateDigest{}, ErrInvalidDigest
		}
	}
	return digest, nil
}

func NewDeltaRangeReq(ranges []DeltaRange, known StateDigest) (DeltaRangeReq, error) {
	if err := validateDeltaRanges(ranges); err != nil {
		return DeltaRangeReq{}, err
	}
	return DeltaRangeReq{Ranges: append([]DeltaRange(nil), ranges...), KnownVersions: known}, nil
}

func DecodeDeltaRangeReq(raw []byte) (DeltaRangeReq, error) {
	var req DeltaRangeReq
	if err := json.Unmarshal(raw, &req); err != nil {
		return DeltaRangeReq{}, fmt.Errorf("%w: %v", ErrInvalidDeltaRange, err)
	}
	if err := validateDeltaRanges(req.Ranges); err != nil {
		return DeltaRangeReq{}, err
	}
	return req, nil
}

func NewDeltaRangeResp(deltas []StateDelta) (DeltaRangeResp, error) {
	if err := validateRecoveredDeltas(deltas); err != nil {
		return DeltaRangeResp{}, err
	}
	return DeltaRangeResp{Deltas: append([]StateDelta(nil), deltas...)}, nil
}

func DecodeDeltaRangeResp(raw []byte) (DeltaRangeResp, error) {
	var resp DeltaRangeResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return DeltaRangeResp{}, fmt.Errorf("%w: %v", ErrInvalidDeltaRange, err)
	}
	if err := validateRecoveredDeltas(resp.Deltas); err != nil {
		return DeltaRangeResp{}, err
	}
	return resp, nil
}

func DecodeSnapshotReq(raw []byte) (SnapshotReq, error) {
	var req SnapshotReq
	if err := json.Unmarshal(raw, &req); err != nil {
		return SnapshotReq{}, fmt.Errorf("%w: %v", ErrInvalidSnapshotReq, err)
	}
	if len(req.WantAggregateTypes) == 0 {
		return SnapshotReq{}, ErrInvalidSnapshotReq
	}
	for _, aggregateType := range req.WantAggregateTypes {
		if aggregateType != common.AggregateSUM && aggregateType != common.AggregateTOPK {
			return SnapshotReq{}, fmt.Errorf("%w: %s", ErrInvalidAggregateType, aggregateType)
		}
	}
	return req, nil
}

func DecodeSnapshotResp(raw []byte) (SnapshotResp, error) {
	var resp SnapshotResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return SnapshotResp{}, fmt.Errorf("%w: %v", ErrInvalidSnapshot, err)
	}
	if resp.CreatedAt.IsZero() {
		return SnapshotResp{}, ErrInvalidSnapshot
	}
	if len(resp.SUMState) == 0 && len(resp.TOPKState) == 0 {
		return SnapshotResp{}, ErrInvalidSnapshot
	}
	for nodeID, sequence := range resp.DeltaSequences {
		if !common.ValidNodeID(nodeID) || sequence == 0 {
			return SnapshotResp{}, ErrInvalidSnapshot
		}
	}
	return resp, nil
}

func validateDeltaRanges(ranges []DeltaRange) error {
	if len(ranges) == 0 {
		return ErrInvalidDeltaRange
	}
	seen := make(map[string]struct{}, len(ranges))
	for _, deltaRange := range ranges {
		if !common.ValidNodeID(deltaRange.OriginNodeID) ||
			deltaRange.FromSequence == 0 ||
			deltaRange.ToSequence < deltaRange.FromSequence ||
			deltaRange.ToSequence-deltaRange.FromSequence+1 > MaxDeltaRangeSize {
			return ErrInvalidDeltaRange
		}
		if _, ok := seen[deltaRange.OriginNodeID]; ok {
			return ErrInvalidDeltaRange
		}
		seen[deltaRange.OriginNodeID] = struct{}{}
	}
	return nil
}

func validateRecoveredDeltas(deltas []StateDelta) error {
	if len(deltas) == 0 {
		return ErrInvalidDeltaRange
	}
	for _, delta := range deltas {
		if !common.ValidNodeID(delta.OriginNodeID) || delta.DeltaSequence == 0 {
			return ErrInvalidDeltaRange
		}
		switch delta.AggregateType {
		case common.AggregateSUM:
			if _, err := DecodeSUMDelta(delta); err != nil {
				return fmt.Errorf("%w: %v", ErrInvalidDeltaRange, err)
			}
		case common.AggregateTOPK:
			if _, err := DecodeTOPKDelta(delta); err != nil {
				return fmt.Errorf("%w: %v", ErrInvalidDeltaRange, err)
			}
		default:
			return ErrInvalidDeltaRange
		}
	}
	return nil
}

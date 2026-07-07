package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gossipdataaggregation-sdcc/internal/aggregation/common"
)

var (
	ErrInvalidAggregateType = errors.New("protocol: invalid aggregate type")
	ErrInvalidDeltaPayload  = errors.New("protocol: invalid delta payload")
)

type StateDelta struct {
	AggregateType string          `json:"aggregate_type"`
	DeltaVersion  uint64          `json:"delta_version"`
	Delta         json.RawMessage `json:"delta"`
}

type SUMDelta struct {
	NodeID string `json:"node_id"`
	Value  uint64 `json:"value"`
}

type TOPKDelta struct {
	ItemID       string    `json:"item_id"`
	Score        float64   `json:"score"`
	EventTS      time.Time `json:"event_ts"`
	OriginNodeID string    `json:"origin_node_id"`
}

func NewSUMStateDelta(deltaVersion uint64, delta SUMDelta) (StateDelta, error) {
	if !common.ValidNodeID(delta.NodeID) {
		return StateDelta{}, ErrInvalidDeltaPayload
	}
	raw, err := json.Marshal(delta)
	if err != nil {
		return StateDelta{}, err
	}
	return StateDelta{
		AggregateType: common.AggregateSUM,
		DeltaVersion:  deltaVersion,
		Delta:         raw,
	}, nil
}

func NewTOPKStateDelta(deltaVersion uint64, delta TOPKDelta) (StateDelta, error) {
	raw, err := json.Marshal(delta)
	if err != nil {
		return StateDelta{}, err
	}
	return StateDelta{
		AggregateType: common.AggregateTOPK,
		DeltaVersion:  deltaVersion,
		Delta:         raw,
	}, nil
}

func DecodeSUMDelta(stateDelta StateDelta) (SUMDelta, error) {
	if stateDelta.AggregateType != common.AggregateSUM {
		return SUMDelta{}, ErrInvalidAggregateType
	}
	var delta SUMDelta
	if err := json.Unmarshal(stateDelta.Delta, &delta); err != nil {
		return SUMDelta{}, fmt.Errorf("%w: %v", ErrInvalidDeltaPayload, err)
	}
	if !common.ValidNodeID(delta.NodeID) {
		return SUMDelta{}, ErrInvalidDeltaPayload
	}
	return delta, nil
}

func DecodeTOPKDelta(stateDelta StateDelta) (TOPKDelta, error) {
	if stateDelta.AggregateType != common.AggregateTOPK {
		return TOPKDelta{}, ErrInvalidAggregateType
	}
	var delta TOPKDelta
	if err := json.Unmarshal(stateDelta.Delta, &delta); err != nil {
		return TOPKDelta{}, fmt.Errorf("%w: %v", ErrInvalidDeltaPayload, err)
	}
	return delta, nil
}

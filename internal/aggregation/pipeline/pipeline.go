package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"gossipdataaggregation-sdcc/internal/aggregation/common"
	sumagg "gossipdataaggregation-sdcc/internal/aggregation/sum"
	topkagg "gossipdataaggregation-sdcc/internal/aggregation/topk"
	"gossipdataaggregation-sdcc/internal/gossip/protocol"
)

var (
	ErrInvalidConfig       = errors.New("pipeline: invalid config")
	ErrInvalidUpdate       = errors.New("pipeline: invalid update")
	ErrOutboundQueueClosed = errors.New("pipeline: outbound queue closed")
)

type Config struct {
	NodeID            string
	TopKMax           int
	OutboundQueueSize int
}

type LocalUpdate struct {
	AggregateType string
	Value         any
}

type Estimates struct {
	SUM  uint64
	TOPK []topkagg.Candidate
}

type Manager struct {
	nodeID string
	sum    *sumagg.GCounter
	topk   *topkagg.Set

	outbound chan protocol.StateDelta
	queueMu  sync.Mutex
	closed   bool
	dropped  atomic.Uint64
}

func New(config Config) (*Manager, error) {
	if !common.ValidNodeID(config.NodeID) {
		return nil, ErrInvalidConfig
	}
	if config.TopKMax <= 0 || config.OutboundQueueSize <= 0 {
		return nil, ErrInvalidConfig
	}

	sumState, err := sumagg.New(config.NodeID)
	if err != nil {
		return nil, err
	}
	topkState, err := topkagg.New(config.TopKMax)
	if err != nil {
		return nil, err
	}

	return &Manager{
		nodeID:   config.NodeID,
		sum:      sumState,
		topk:     topkState,
		outbound: make(chan protocol.StateDelta, config.OutboundQueueSize),
	}, nil
}

func (m *Manager) ApplyLocalUpdate(update LocalUpdate) (protocol.StateDelta, bool, error) {
	switch update.AggregateType {
	case common.AggregateSUM:
		advanced, err := m.sum.Update(update.Value)
		if err != nil || !advanced {
			return protocol.StateDelta{}, false, err
		}
		state := m.sum.State()
		delta, err := protocol.NewSUMStateDelta(state.Version, protocol.SUMDelta{
			NodeID: m.nodeID,
			Value:  state.Contrib[m.nodeID],
		})
		if err != nil {
			return protocol.StateDelta{}, false, err
		}
		m.enqueue(delta)
		return delta, true, nil
	case common.AggregateTOPK:
		candidate, ok := update.Value.(topkagg.Candidate)
		if !ok {
			return protocol.StateDelta{}, false, ErrInvalidUpdate
		}
		candidate.OriginNodeID = m.nodeID
		advanced, err := m.topk.Update(candidate)
		if err != nil || !advanced {
			return protocol.StateDelta{}, false, err
		}
		state := m.topk.State()
		delta, err := protocol.NewTOPKStateDelta(state.Version, protocol.TOPKDelta{
			ItemID:       candidate.ItemID,
			Score:        candidate.Score,
			EventTS:      candidate.EventTS,
			OriginNodeID: candidate.OriginNodeID,
		})
		if err != nil {
			return protocol.StateDelta{}, false, err
		}
		m.enqueue(delta)
		return delta, true, nil
	default:
		return protocol.StateDelta{}, false, ErrInvalidUpdate
	}
}

func (m *Manager) ApplyReceivedDelta(delta protocol.StateDelta) (bool, error) {
	switch delta.AggregateType {
	case common.AggregateSUM:
		sumDelta, err := protocol.DecodeSUMDelta(delta)
		if err != nil {
			return false, err
		}
		return m.sum.Merge(sumagg.State{
			Contrib: map[string]uint64{sumDelta.NodeID: sumDelta.Value},
			Version: delta.DeltaVersion,
		})
	case common.AggregateTOPK:
		topkDelta, err := protocol.DecodeTOPKDelta(delta)
		if err != nil {
			return false, err
		}
		return m.topk.Merge(topkagg.State{
			Candidates: []topkagg.Candidate{{
				ItemID:       topkDelta.ItemID,
				Score:        topkDelta.Score,
				EventTS:      topkDelta.EventTS,
				OriginNodeID: topkDelta.OriginNodeID,
			}},
			Version: delta.DeltaVersion,
		})
	default:
		return false, fmt.Errorf("%w: %s", ErrInvalidUpdate, delta.AggregateType)
	}
}

func (m *Manager) NextOutbound(ctx context.Context) (protocol.StateDelta, error) {
	select {
	case <-ctx.Done():
		return protocol.StateDelta{}, ctx.Err()
	case delta, ok := <-m.outbound:
		if !ok {
			return protocol.StateDelta{}, ErrOutboundQueueClosed
		}
		return delta, nil
	}
}

func (m *Manager) DrainOutbound() []protocol.StateDelta {
	out := make([]protocol.StateDelta, 0)
	for {
		select {
		case delta, ok := <-m.outbound:
			if !ok {
				return out
			}
			out = append(out, delta)
		default:
			return out
		}
	}
}

func (m *Manager) Estimates(topK int) (Estimates, error) {
	sumEstimate, err := m.sum.Estimate(nil)
	if err != nil {
		return Estimates{}, err
	}
	topkEstimate, err := m.topk.Estimate(topK)
	if err != nil {
		return Estimates{}, err
	}
	return Estimates{
		SUM:  sumEstimate.(uint64),
		TOPK: topkEstimate.([]topkagg.Candidate),
	}, nil
}

func (m *Manager) DroppedOutbound() uint64 {
	return m.dropped.Load()
}

func (m *Manager) Close() {
	m.queueMu.Lock()
	defer m.queueMu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.outbound)
	}
}

func (m *Manager) enqueue(delta protocol.StateDelta) {
	m.queueMu.Lock()
	defer m.queueMu.Unlock()
	if m.closed {
		m.dropped.Add(1)
		return
	}
	select {
	case m.outbound <- delta:
	default:
		m.dropped.Add(1)
	}
}

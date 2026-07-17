package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"gossipdataaggregation-sdcc/internal/aggregation/common"
	sumagg "gossipdataaggregation-sdcc/internal/aggregation/sum"
	topkagg "gossipdataaggregation-sdcc/internal/aggregation/topk"
	"gossipdataaggregation-sdcc/internal/gossip/protocol"
)

var (
	ErrInvalidConfig       = errors.New("pipeline: invalid config")
	ErrInvalidUpdate       = errors.New("pipeline: invalid update")
	ErrOutboundQueueClosed = errors.New("pipeline: outbound queue closed")
	ErrPersistence         = errors.New("pipeline: persistence failed")
)

type Journal interface {
	AppendDelta(protocol.StateDelta) error
	AppendSnapshot(protocol.SnapshotResp) error
}

type Checkpointer interface {
	SaveCheckpoint(protocol.SnapshotResp) error
}

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

	outbound   chan protocol.StateDelta
	queueMu    sync.Mutex
	closed     bool
	dropped    atomic.Uint64
	deltaSeq   atomic.Uint64
	mutationMu sync.Mutex
	journal    Journal
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
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()

	switch update.AggregateType {
	case common.AggregateSUM:
		before, err := m.sum.Serialize()
		if err != nil {
			return protocol.StateDelta{}, false, err
		}
		beforeSequence := m.deltaSeq.Load()
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
		delta.OriginNodeID = m.nodeID
		delta.DeltaSequence = m.deltaSeq.Add(1)
		if err := m.appendDelta(delta); err != nil {
			_ = m.sum.Deserialize(before)
			m.deltaSeq.Store(beforeSequence)
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
		before, err := m.topk.Serialize()
		if err != nil {
			return protocol.StateDelta{}, false, err
		}
		beforeSequence := m.deltaSeq.Load()
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
		delta.OriginNodeID = m.nodeID
		delta.DeltaSequence = m.deltaSeq.Add(1)
		if err := m.appendDelta(delta); err != nil {
			_ = m.topk.Deserialize(before)
			m.deltaSeq.Store(beforeSequence)
			return protocol.StateDelta{}, false, err
		}
		m.enqueue(delta)
		return delta, true, nil
	default:
		return protocol.StateDelta{}, false, ErrInvalidUpdate
	}
}

func (m *Manager) ApplyReceivedDelta(delta protocol.StateDelta) (bool, error) {
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	return m.applyReceivedDeltaLocked(delta, true)
}

func (m *Manager) applyReceivedDeltaLocked(delta protocol.StateDelta, persist bool) (bool, error) {
	var advanced bool
	var err error
	var rollback func() error
	beforeSequence := m.deltaSeq.Load()
	switch delta.AggregateType {
	case common.AggregateSUM:
		sumDelta, err := protocol.DecodeSUMDelta(delta)
		if err != nil {
			return false, err
		}
		before, serializeErr := m.sum.Serialize()
		if serializeErr != nil {
			return false, serializeErr
		}
		rollback = func() error { return m.sum.Deserialize(before) }
		advanced, err = m.sum.Merge(sumagg.State{
			Contrib: map[string]uint64{sumDelta.NodeID: sumDelta.Value},
			Version: delta.DeltaVersion,
		})
	case common.AggregateTOPK:
		topkDelta, err := protocol.DecodeTOPKDelta(delta)
		if err != nil {
			return false, err
		}
		before, serializeErr := m.topk.Serialize()
		if serializeErr != nil {
			return false, serializeErr
		}
		rollback = func() error { return m.topk.Deserialize(before) }
		advanced, err = m.topk.Merge(topkagg.State{
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
	if err != nil {
		return false, err
	}
	if delta.OriginNodeID == m.nodeID {
		advanceAtomic(&m.deltaSeq, delta.DeltaSequence)
	}
	if advanced && persist {
		if err := m.appendDelta(delta); err != nil {
			if rollbackErr := rollback(); rollbackErr != nil {
				return false, fmt.Errorf("%v; rollback failed: %w", err, rollbackErr)
			}
			m.deltaSeq.Store(beforeSequence)
			return false, err
		}
	}
	return advanced, nil
}

func (m *Manager) Digest() (protocol.StateDigest, error) {
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	sumState := m.sum.State()
	sumRaw, err := m.sum.Serialize()
	if err != nil {
		return protocol.StateDigest{}, err
	}
	topkState := m.topk.State()
	topkRaw, err := m.topk.Serialize()
	if err != nil {
		return protocol.StateDigest{}, err
	}
	digest := protocol.StateDigest{
		SUM: protocol.AggregateDigest{
			Version:  sumState.Version,
			Checksum: protocol.StateChecksum(sumRaw),
		},
		TOPK: protocol.AggregateDigest{
			Version:  topkState.Version,
			Checksum: protocol.StateChecksum(topkRaw),
		},
	}
	if sequence := m.deltaSeq.Load(); sequence > 0 {
		digest.DeltaSequences = map[string]uint64{m.nodeID: sequence}
	}
	return digest, nil
}

func (m *Manager) Snapshot(want []string) (protocol.SnapshotResp, error) {
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	return m.snapshotLocked(want)
}

func (m *Manager) snapshotLocked(want []string) (protocol.SnapshotResp, error) {
	resp := protocol.SnapshotResp{
		CreatedAt: time.Now().UTC(),
	}
	if sequence := m.deltaSeq.Load(); sequence > 0 {
		resp.DeltaSequences = map[string]uint64{m.nodeID: sequence}
	}
	for _, aggregateType := range want {
		switch aggregateType {
		case common.AggregateSUM:
			raw, err := m.sum.Serialize()
			if err != nil {
				return protocol.SnapshotResp{}, err
			}
			resp.SUMState = json.RawMessage(raw)
			resp.SnapshotVersion = maxUint64(resp.SnapshotVersion, m.sum.State().Version)
		case common.AggregateTOPK:
			raw, err := m.topk.Serialize()
			if err != nil {
				return protocol.SnapshotResp{}, err
			}
			resp.TOPKState = json.RawMessage(raw)
			resp.SnapshotVersion = maxUint64(resp.SnapshotVersion, m.topk.State().Version)
		default:
			return protocol.SnapshotResp{}, fmt.Errorf("%w: %s", ErrInvalidUpdate, aggregateType)
		}
	}
	if len(resp.SUMState) == 0 && len(resp.TOPKState) == 0 {
		return protocol.SnapshotResp{}, ErrInvalidUpdate
	}
	return resp, nil
}

func (m *Manager) ApplySnapshot(snapshot protocol.SnapshotResp) (bool, error) {
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	beforeSUM, err := m.sum.Serialize()
	if err != nil {
		return false, err
	}
	beforeTOPK, err := m.topk.Serialize()
	if err != nil {
		return false, err
	}
	beforeSequence := m.deltaSeq.Load()
	advanced, err := m.applySnapshotLocked(snapshot)
	if err != nil || !advanced {
		return advanced, err
	}
	if err := m.appendSnapshot(snapshot); err != nil {
		if sumErr := m.sum.Deserialize(beforeSUM); sumErr != nil {
			return false, fmt.Errorf("%v; SUM rollback failed: %w", err, sumErr)
		}
		if topkErr := m.topk.Deserialize(beforeTOPK); topkErr != nil {
			return false, fmt.Errorf("%v; TOPK rollback failed: %w", err, topkErr)
		}
		m.deltaSeq.Store(beforeSequence)
		return false, err
	}
	return true, nil
}

func (m *Manager) applySnapshotLocked(snapshot protocol.SnapshotResp) (bool, error) {
	advanced := false
	if len(snapshot.SUMState) > 0 {
		state, err := sumagg.StateFromSerialized(snapshot.SUMState)
		if err != nil {
			return false, err
		}
		sumAdvanced, err := m.sum.Merge(state)
		if err != nil {
			return false, err
		}
		advanced = advanced || sumAdvanced
	}
	if len(snapshot.TOPKState) > 0 {
		state, err := topkagg.StateFromSerialized(snapshot.TOPKState)
		if err != nil {
			return false, err
		}
		topkAdvanced, err := m.topk.Merge(state)
		if err != nil {
			return false, err
		}
		advanced = advanced || topkAdvanced
	}
	advanceAtomic(&m.deltaSeq, snapshot.DeltaSequences[m.nodeID])
	return advanced, nil
}

func (m *Manager) SetJournal(journal Journal) {
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	m.journal = journal
}

func (m *Manager) RestoreCheckpoint(checkpoint protocol.SnapshotResp) error {
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	if len(checkpoint.SUMState) == 0 || len(checkpoint.TOPKState) == 0 {
		return ErrInvalidUpdate
	}
	if err := m.sum.Deserialize(checkpoint.SUMState); err != nil {
		return err
	}
	if err := m.topk.Deserialize(checkpoint.TOPKState); err != nil {
		return err
	}
	advanceAtomic(&m.deltaSeq, checkpoint.DeltaSequences[m.nodeID])
	return nil
}

func (m *Manager) ReplayDelta(delta protocol.StateDelta) error {
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	_, err := m.applyReceivedDeltaLocked(delta, false)
	return err
}

func (m *Manager) ReplaySnapshot(snapshot protocol.SnapshotResp) error {
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	_, err := m.applySnapshotLocked(snapshot)
	return err
}

func (m *Manager) Checkpoint(checkpointer Checkpointer) error {
	if checkpointer == nil {
		return ErrPersistence
	}
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	state, err := m.snapshotLocked([]string{common.AggregateSUM, common.AggregateTOPK})
	if err != nil {
		return err
	}
	if err := checkpointer.SaveCheckpoint(state); err != nil {
		return fmt.Errorf("%w: %v", ErrPersistence, err)
	}
	return nil
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
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
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

func (m *Manager) appendDelta(delta protocol.StateDelta) error {
	if m.journal == nil {
		return nil
	}
	if err := m.journal.AppendDelta(delta); err != nil {
		return fmt.Errorf("%w: %v", ErrPersistence, err)
	}
	return nil
}

func (m *Manager) appendSnapshot(snapshot protocol.SnapshotResp) error {
	if m.journal == nil {
		return nil
	}
	if err := m.journal.AppendSnapshot(snapshot); err != nil {
		return fmt.Errorf("%w: %v", ErrPersistence, err)
	}
	return nil
}

func advanceAtomic(value *atomic.Uint64, candidate uint64) {
	for {
		current := value.Load()
		if candidate <= current || value.CompareAndSwap(current, candidate) {
			return
		}
	}
}

func maxUint64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

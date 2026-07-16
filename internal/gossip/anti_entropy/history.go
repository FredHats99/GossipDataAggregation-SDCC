package antientropy

import (
	"errors"
	"sync"

	"gossipdataaggregation-sdcc/internal/aggregation/common"
	"gossipdataaggregation-sdcc/internal/gossip/protocol"
)

var (
	ErrInvalidHistoryCapacity = errors.New("anti-entropy: invalid history capacity")
	ErrInvalidHistoryDelta    = errors.New("anti-entropy: invalid history delta")
)

type DeltaHistory struct {
	mu         sync.RWMutex
	capacity   int
	entries    map[string]map[uint64]protocol.StateDelta
	contiguous map[string]uint64
}

func NewDeltaHistory(capacity int) (*DeltaHistory, error) {
	if capacity <= 0 {
		return nil, ErrInvalidHistoryCapacity
	}
	return &DeltaHistory{
		capacity:   capacity,
		entries:    make(map[string]map[uint64]protocol.StateDelta),
		contiguous: make(map[string]uint64),
	}, nil
}

func (h *DeltaHistory) Record(delta protocol.StateDelta) error {
	if !common.ValidNodeID(delta.OriginNodeID) || delta.DeltaSequence == 0 {
		return ErrInvalidHistoryDelta
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	bySequence := h.entries[delta.OriginNodeID]
	if bySequence == nil {
		bySequence = make(map[uint64]protocol.StateDelta)
		h.entries[delta.OriginNodeID] = bySequence
	}
	if _, exists := bySequence[delta.DeltaSequence]; exists {
		return nil
	}
	bySequence[delta.DeltaSequence] = cloneDelta(delta)

	for len(bySequence) > h.capacity {
		oldest := delta.DeltaSequence
		for sequence := range bySequence {
			if sequence < oldest {
				oldest = sequence
			}
		}
		delete(bySequence, oldest)
	}

	watermark := h.contiguous[delta.OriginNodeID]
	for {
		if _, ok := bySequence[watermark+1]; !ok {
			break
		}
		watermark++
	}
	h.contiguous[delta.OriginNodeID] = watermark
	return nil
}

func (h *DeltaHistory) Watermarks() map[string]uint64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make(map[string]uint64, len(h.contiguous))
	for nodeID, sequence := range h.contiguous {
		if sequence > 0 {
			out[nodeID] = sequence
		}
	}
	return out
}

func (h *DeltaHistory) AdvanceWatermarks(watermarks map[string]uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for nodeID, sequence := range watermarks {
		if !common.ValidNodeID(nodeID) || sequence <= h.contiguous[nodeID] {
			continue
		}
		h.contiguous[nodeID] = sequence
		bySequence := h.entries[nodeID]
		for storedSequence := range bySequence {
			if storedSequence <= sequence {
				delete(bySequence, storedSequence)
			}
		}
		for {
			if _, ok := bySequence[h.contiguous[nodeID]+1]; !ok {
				break
			}
			h.contiguous[nodeID]++
		}
	}
}

func (h *DeltaHistory) Range(deltaRange protocol.DeltaRange) ([]protocol.StateDelta, bool) {
	if !common.ValidNodeID(deltaRange.OriginNodeID) ||
		deltaRange.FromSequence == 0 ||
		deltaRange.ToSequence < deltaRange.FromSequence ||
		deltaRange.ToSequence-deltaRange.FromSequence+1 > protocol.MaxDeltaRangeSize {
		return nil, false
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	bySequence := h.entries[deltaRange.OriginNodeID]
	if bySequence == nil {
		return nil, false
	}
	out := make([]protocol.StateDelta, 0, deltaRange.ToSequence-deltaRange.FromSequence+1)
	for sequence := deltaRange.FromSequence; ; sequence++ {
		delta, ok := bySequence[sequence]
		if !ok {
			return nil, false
		}
		out = append(out, cloneDelta(delta))
		if sequence == deltaRange.ToSequence {
			break
		}
	}
	return out, true
}

func cloneDelta(delta protocol.StateDelta) protocol.StateDelta {
	delta.Delta = append([]byte(nil), delta.Delta...)
	return delta
}

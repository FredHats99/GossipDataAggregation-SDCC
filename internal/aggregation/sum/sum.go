package sum

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"

	"gossipdataaggregation-sdcc/internal/aggregation/common"
)

const schemaVersion = 1

var (
	ErrInvalidNodeID      = errors.New("sum: invalid node_id")
	ErrInvalidIncrement   = errors.New("sum: increment must be greater than zero")
	ErrInvalidPeerState   = errors.New("sum: invalid peer state")
	ErrUnsupportedVersion = errors.New("sum: unsupported version")
	ErrOverflow           = errors.New("sum: uint64 overflow")
)

type GCounter struct {
	mu      sync.RWMutex
	nodeID  string
	version uint64
	contrib map[string]uint64
}

type State struct {
	Contrib map[string]uint64
	Version uint64
}

type payload struct {
	AggregateType   string            `json:"aggregate_type"`
	StateVersion    uint64            `json:"state_version"`
	ProtocolVersion string            `json:"protocol_version"`
	SchemaVersion   int               `json:"schema_version"`
	StatePayload    map[string]uint64 `json:"state_payload"`
}

func New(nodeID string) (*GCounter, error) {
	if !common.ValidNodeID(nodeID) {
		return nil, ErrInvalidNodeID
	}
	return &GCounter{
		nodeID:  nodeID,
		contrib: map[string]uint64{nodeID: 0},
	}, nil
}

func FromState(nodeID string, state State) (*GCounter, error) {
	g, err := New(nodeID)
	if err != nil {
		return nil, err
	}
	if err := validateContrib(state.Contrib); err != nil {
		return nil, err
	}
	g.contrib = cloneContrib(state.Contrib)
	if _, ok := g.contrib[nodeID]; !ok {
		g.contrib[nodeID] = 0
	}
	g.version = state.Version
	return g, nil
}

func (g *GCounter) Update(value any) (bool, error) {
	increment, err := incrementFrom(value)
	if err != nil {
		return false, err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	current := g.contrib[g.nodeID]
	if math.MaxUint64-current < increment {
		return false, ErrOverflow
	}
	g.contrib[g.nodeID] = current + increment
	g.version++
	return true, nil
}

func (g *GCounter) Merge(peerState any) (bool, error) {
	state, err := stateFrom(peerState)
	if err != nil {
		return false, err
	}
	if err := validateContrib(state.Contrib); err != nil {
		return false, err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	advanced := false
	for nodeID, peerValue := range state.Contrib {
		if peerValue > g.contrib[nodeID] {
			g.contrib[nodeID] = peerValue
			advanced = true
		}
	}
	if advanced {
		g.version = max(g.version, state.Version) + 1
	} else if state.Version > g.version {
		g.version = state.Version
	}
	return advanced, nil
}

func (g *GCounter) Estimate(any) (any, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var total uint64
	for _, value := range g.contrib {
		if math.MaxUint64-total < value {
			return nil, ErrOverflow
		}
		total += value
	}
	return total, nil
}

func (g *GCounter) Serialize() ([]byte, error) {
	state := g.State()
	return json.Marshal(payload{
		AggregateType:   common.AggregateSUM,
		StateVersion:    state.Version,
		ProtocolVersion: common.ProtocolVersion,
		SchemaVersion:   schemaVersion,
		StatePayload:    state.Contrib,
	})
}

func (g *GCounter) Deserialize(raw []byte) error {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}
	if p.AggregateType != common.AggregateSUM ||
		p.ProtocolVersion != common.ProtocolVersion ||
		p.SchemaVersion != schemaVersion {
		return ErrUnsupportedVersion
	}
	if err := validateContrib(p.StatePayload); err != nil {
		return err
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	g.contrib = cloneContrib(p.StatePayload)
	if _, ok := g.contrib[g.nodeID]; !ok {
		g.contrib[g.nodeID] = 0
	}
	g.version = p.StateVersion
	return nil
}

func (g *GCounter) State() State {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return State{
		Contrib: cloneContrib(g.contrib),
		Version: g.version,
	}
}

func incrementFrom(value any) (uint64, error) {
	switch v := value.(type) {
	case uint64:
		if v == 0 {
			return 0, ErrInvalidIncrement
		}
		return v, nil
	case uint:
		if v == 0 {
			return 0, ErrInvalidIncrement
		}
		return uint64(v), nil
	case int:
		if v <= 0 {
			return 0, ErrInvalidIncrement
		}
		return uint64(v), nil
	case int64:
		if v <= 0 {
			return 0, ErrInvalidIncrement
		}
		return uint64(v), nil
	default:
		return 0, ErrInvalidIncrement
	}
}

func stateFrom(value any) (State, error) {
	switch v := value.(type) {
	case State:
		return v, nil
	case *State:
		if v == nil {
			return State{}, ErrInvalidPeerState
		}
		return *v, nil
	case map[string]uint64:
		return State{Contrib: v}, nil
	default:
		return State{}, fmt.Errorf("%w: %T", ErrInvalidPeerState, value)
	}
}

func validateContrib(contrib map[string]uint64) error {
	if contrib == nil {
		return ErrInvalidPeerState
	}
	for nodeID := range contrib {
		if !common.ValidNodeID(nodeID) {
			return ErrInvalidNodeID
		}
	}
	return nil
}

func cloneContrib(in map[string]uint64) map[string]uint64 {
	out := make(map[string]uint64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func max(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

package topk

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"gossipdataaggregation-sdcc/internal/aggregation/common"
)

const schemaVersion = 1

var (
	ErrInvalidK           = errors.New("topk: k must be greater than zero")
	ErrInvalidCandidate   = errors.New("topk: invalid candidate")
	ErrInvalidPeerState   = errors.New("topk: invalid peer state")
	ErrUnsupportedVersion = errors.New("topk: unsupported version")
)

type Candidate struct {
	ItemID       string    `json:"item_id"`
	Score        float64   `json:"score"`
	EventTS      time.Time `json:"event_ts"`
	OriginNodeID string    `json:"origin_node_id"`
}

type State struct {
	Candidates []Candidate
	Version    uint64
}

type EstimateOptions struct {
	K int
}

type Set struct {
	mu         sync.RWMutex
	kmax       int
	version    uint64
	candidates []Candidate
}

type payload struct {
	AggregateType   string      `json:"aggregate_type"`
	StateVersion    uint64      `json:"state_version"`
	ProtocolVersion string      `json:"protocol_version"`
	SchemaVersion   int         `json:"schema_version"`
	StatePayload    []Candidate `json:"state_payload"`
}

func New(kmax int) (*Set, error) {
	if kmax <= 0 {
		return nil, ErrInvalidK
	}
	return &Set{kmax: kmax}, nil
}

func FromState(kmax int, state State) (*Set, error) {
	s, err := New(kmax)
	if err != nil {
		return nil, err
	}
	if err := validateCandidates(state.Candidates); err != nil {
		return nil, err
	}
	s.candidates = canonicalize(state.Candidates, kmax)
	s.version = state.Version
	return s, nil
}

func (s *Set) Update(value any) (bool, error) {
	candidate, err := candidateFrom(value)
	if err != nil {
		return false, err
	}
	if err := validateCandidate(candidate); err != nil {
		return false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	before := cloneCandidates(s.candidates)
	s.candidates = canonicalize(append(s.candidates, candidate), s.kmax)
	advanced := !reflect.DeepEqual(before, s.candidates)
	if advanced {
		s.version++
	}
	return advanced, nil
}

func (s *Set) Merge(peerState any) (bool, error) {
	state, err := stateFrom(peerState)
	if err != nil {
		return false, err
	}
	if err := validateCandidates(state.Candidates); err != nil {
		return false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	before := cloneCandidates(s.candidates)
	s.candidates = canonicalize(append(s.candidates, state.Candidates...), s.kmax)
	advanced := !reflect.DeepEqual(before, s.candidates)
	if advanced {
		s.version = max(s.version, state.Version) + 1
	} else if state.Version > s.version {
		s.version = state.Version
	}
	return advanced, nil
}

func (s *Set) Estimate(opts any) (any, error) {
	k := s.kmax
	if opts != nil {
		switch v := opts.(type) {
		case EstimateOptions:
			if v.K <= 0 {
				return nil, ErrInvalidK
			}
			k = v.K
		case *EstimateOptions:
			if v == nil || v.K <= 0 {
				return nil, ErrInvalidK
			}
			k = v.K
		case int:
			if v <= 0 {
				return nil, ErrInvalidK
			}
			k = v
		default:
			return nil, fmt.Errorf("%w: %T", ErrInvalidK, opts)
		}
	}
	if k > s.kmax {
		k = s.kmax
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if k > len(s.candidates) {
		k = len(s.candidates)
	}
	return cloneCandidates(s.candidates[:k]), nil
}

func (s *Set) Serialize() ([]byte, error) {
	state := s.State()
	return json.Marshal(payload{
		AggregateType:   common.AggregateTOPK,
		StateVersion:    state.Version,
		ProtocolVersion: common.ProtocolVersion,
		SchemaVersion:   schemaVersion,
		StatePayload:    state.Candidates,
	})
}

func (s *Set) Deserialize(raw []byte) error {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}
	if p.AggregateType != common.AggregateTOPK ||
		p.ProtocolVersion != common.ProtocolVersion ||
		p.SchemaVersion != schemaVersion {
		return ErrUnsupportedVersion
	}
	if err := validateCandidates(p.StatePayload); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.candidates = canonicalize(p.StatePayload, s.kmax)
	s.version = p.StateVersion
	return nil
}

func (s *Set) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return State{
		Candidates: cloneCandidates(s.candidates),
		Version:    s.version,
	}
}

func candidateFrom(value any) (Candidate, error) {
	switch v := value.(type) {
	case Candidate:
		return v, nil
	case *Candidate:
		if v == nil {
			return Candidate{}, ErrInvalidCandidate
		}
		return *v, nil
	default:
		return Candidate{}, fmt.Errorf("%w: %T", ErrInvalidCandidate, value)
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
	case []Candidate:
		return State{Candidates: v}, nil
	default:
		return State{}, fmt.Errorf("%w: %T", ErrInvalidPeerState, value)
	}
}

func validateCandidates(candidates []Candidate) error {
	for _, candidate := range candidates {
		if err := validateCandidate(candidate); err != nil {
			return err
		}
	}
	return nil
}

func validateCandidate(candidate Candidate) error {
	if strings.TrimSpace(candidate.ItemID) == "" {
		return ErrInvalidCandidate
	}
	if !common.ValidNodeID(candidate.OriginNodeID) {
		return ErrInvalidCandidate
	}
	if math.IsNaN(candidate.Score) || math.IsInf(candidate.Score, 0) {
		return ErrInvalidCandidate
	}
	if candidate.EventTS.IsZero() {
		return ErrInvalidCandidate
	}
	return nil
}

func canonicalize(candidates []Candidate, kmax int) []Candidate {
	byTuple := make(map[string]Candidate, len(candidates))
	for _, candidate := range candidates {
		normalized := normalizeCandidate(candidate)
		byTuple[candidateKey(normalized)] = normalized
	}

	out := make([]Candidate, 0, len(byTuple))
	for _, candidate := range byTuple {
		out = append(out, candidate)
	}
	sortCandidates(out)
	if len(out) > kmax {
		out = out[:kmax]
	}
	return out
}

func normalizeCandidate(candidate Candidate) Candidate {
	candidate.EventTS = candidate.EventTS.UTC().Round(0)
	return candidate
}

func candidateKey(candidate Candidate) string {
	return fmt.Sprintf(
		"%s\x00%g\x00%s\x00%s",
		candidate.ItemID,
		candidate.Score,
		candidate.EventTS.Format(time.RFC3339Nano),
		candidate.OriginNodeID,
	)
}

func sortCandidates(candidates []Candidate) {
	sort.Slice(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.Score != right.Score {
			return left.Score > right.Score
		}
		if !left.EventTS.Equal(right.EventTS) {
			return left.EventTS.After(right.EventTS)
		}
		if left.OriginNodeID != right.OriginNodeID {
			return left.OriginNodeID < right.OriginNodeID
		}
		return left.ItemID < right.ItemID
	})
}

func cloneCandidates(in []Candidate) []Candidate {
	out := make([]Candidate, len(in))
	copy(out, in)
	return out
}

func max(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

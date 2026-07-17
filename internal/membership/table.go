package membership

import (
	"math"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Status string

const (
	StatusAlive   Status = "alive"
	StatusSuspect Status = "suspect"
	StatusDead    Status = "dead"
)

var membershipNodeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

// Entry is the portable portion of a membership record. LastSeen and Missed
// are deliberately node-local failure detector state and are never gossiped.
type Entry struct {
	NodeID      string `json:"node_id"`
	Endpoint    string `json:"endpoint"`
	Status      Status `json:"status"`
	Incarnation uint64 `json:"incarnation"`
}

type Member struct {
	NodeID      string
	Endpoint    string
	Status      Status
	Incarnation uint64
	LastSeen    time.Time
	Missed      int
}

type Table struct {
	selfNodeID string
	mu         sync.RWMutex
	members    map[string]Member
	byTarget   map[string]string
}

func NewTable(selfNodeID string, selfEndpoint string) *Table {
	return newTable(selfNodeID, selfEndpoint, initialIncarnation())
}

func newTable(selfNodeID string, selfEndpoint string, incarnation uint64) *Table {
	if incarnation == 0 {
		incarnation = 1
	}
	now := time.Now().UTC()
	selfEndpoint = strings.TrimSpace(selfEndpoint)
	t := &Table{
		selfNodeID: selfNodeID,
		members:    make(map[string]Member),
		byTarget:   make(map[string]string),
	}
	t.members[selfNodeID] = Member{
		NodeID:      selfNodeID,
		Endpoint:    selfEndpoint,
		Status:      StatusAlive,
		Incarnation: incarnation,
		LastSeen:    now,
	}
	if selfEndpoint != "" {
		t.byTarget[selfEndpoint] = selfNodeID
	}
	return t
}

func initialIncarnation() uint64 {
	now := time.Now().UTC().UnixNano()
	if now <= 0 {
		return 1
	}
	return uint64(now)
}

// ObserveAlive records direct evidence from a successful Ping/Ack exchange.
// Direct evidence can restore alive at the same incarnation; older processes
// cannot overwrite a newer incarnation already known locally.
func (t *Table) ObserveAlive(nodeID, endpoint string, incarnation uint64) bool {
	nodeID = strings.TrimSpace(nodeID)
	endpoint = strings.TrimSpace(endpoint)
	if !membershipNodeIDPattern.MatchString(nodeID) || nodeID == t.selfNodeID ||
		!validMembershipEndpoint(endpoint) || incarnation == 0 {
		return false
	}

	now := time.Now().UTC()
	t.mu.Lock()
	defer t.mu.Unlock()

	current, exists := t.members[nodeID]
	if exists && incarnation < current.Incarnation {
		return false
	}
	if exists && (incarnation > current.Incarnation || (endpoint != "" && current.Endpoint != endpoint)) {
		t.clearTargetsForNodeLocked(nodeID)
	}
	changed := !exists || current.Status != StatusAlive || current.Incarnation != incarnation ||
		(endpoint != "" && current.Endpoint != endpoint)
	current.NodeID = nodeID
	if endpoint != "" {
		current.Endpoint = endpoint
		t.byTarget[endpoint] = nodeID
	}
	current.Status = StatusAlive
	current.Incarnation = incarnation
	current.LastSeen = now
	current.Missed = 0
	t.members[nodeID] = current
	return changed
}

// Merge applies indirectly observed membership entries. A higher incarnation
// always wins. At the same incarnation, failure states only move forward from
// alive to suspect to dead. An accusation about this process is refuted by
// advancing its incarnation and advertising alive again.
func (t *Table) Merge(entries []Entry) bool {
	now := time.Now().UTC()
	changed := false

	t.mu.Lock()
	defer t.mu.Unlock()

	for _, entry := range entries {
		entry.NodeID = strings.TrimSpace(entry.NodeID)
		entry.Endpoint = strings.TrimSpace(entry.Endpoint)
		if !validEntry(entry) {
			continue
		}
		if entry.NodeID == t.selfNodeID {
			self := t.members[t.selfNodeID]
			if entry.Status != StatusAlive && entry.Incarnation >= self.Incarnation {
				self.Incarnation = incrementIncarnation(entry.Incarnation)
				self.Status = StatusAlive
				self.LastSeen = now
				self.Missed = 0
				t.members[t.selfNodeID] = self
				changed = true
			}
			continue
		}

		current, exists := t.members[entry.NodeID]
		accept := !exists || entry.Incarnation > current.Incarnation ||
			(entry.Incarnation == current.Incarnation && statusRank(entry.Status) > statusRank(current.Status))
		if !accept {
			if exists && current.Endpoint == "" && entry.Endpoint != "" && entry.Incarnation == current.Incarnation {
				current.Endpoint = entry.Endpoint
				t.members[entry.NodeID] = current
				t.byTarget[entry.Endpoint] = entry.NodeID
				changed = true
			}
			continue
		}

		member := Member{
			NodeID:      entry.NodeID,
			Endpoint:    entry.Endpoint,
			Status:      entry.Status,
			Incarnation: entry.Incarnation,
			LastSeen:    now,
		}
		if exists && entry.Incarnation > current.Incarnation {
			t.clearTargetsForNodeLocked(entry.NodeID)
		}
		t.members[entry.NodeID] = member
		if entry.Endpoint != "" {
			t.byTarget[entry.Endpoint] = entry.NodeID
		}
		changed = true
	}
	return changed
}

func validEntry(entry Entry) bool {
	if !membershipNodeIDPattern.MatchString(entry.NodeID) || !validMembershipEndpoint(entry.Endpoint) || entry.Incarnation == 0 ||
		(entry.Status != StatusAlive && entry.Status != StatusSuspect && entry.Status != StatusDead) {
		return false
	}
	return true
}

func validMembershipEndpoint(endpoint string) bool {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil || strings.TrimSpace(host) == "" {
		return false
	}
	portNumber, err := strconv.Atoi(port)
	return err == nil && portNumber > 0 && portNumber <= 65535
}

func (t *Table) clearTargetsForNodeLocked(nodeID string) {
	for target, targetNodeID := range t.byTarget {
		if targetNodeID == nodeID {
			delete(t.byTarget, target)
		}
	}
}

func statusRank(status Status) int {
	switch status {
	case StatusAlive:
		return 1
	case StatusSuspect:
		return 2
	case StatusDead:
		return 3
	default:
		return 0
	}
}

func incrementIncarnation(value uint64) uint64 {
	if value == math.MaxUint64 {
		return value
	}
	return value + 1
}

// AssociateTarget remembers the concrete address used to contact a member
// without replacing the canonical endpoint disseminated by that member.
func (t *Table) AssociateTarget(nodeID, target string) {
	nodeID = strings.TrimSpace(nodeID)
	target = strings.TrimSpace(target)
	if nodeID == "" || target == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.members[nodeID]; exists {
		t.byTarget[target] = nodeID
	}
}

func (t *Table) MarkMissedByEndpoint(endpoint string, suspectAfter int, deadAfter int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	nodeID, ok := t.byTarget[endpoint]
	if !ok || nodeID == t.selfNodeID {
		return
	}

	m := t.members[nodeID]
	m.Missed++
	if m.Missed >= deadAfter {
		m.Status = StatusDead
	} else if m.Missed >= suspectAfter {
		m.Status = StatusSuspect
	}
	t.members[nodeID] = m
}

func (t *Table) ApplyTimeouts(now time.Time, suspectAfter time.Duration, deadAfter time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for nodeID, m := range t.members {
		if nodeID == t.selfNodeID {
			continue
		}
		since := now.Sub(m.LastSeen)
		if since >= deadAfter {
			m.Status = StatusDead
		} else if since >= suspectAfter && m.Status == StatusAlive {
			m.Status = StatusSuspect
		}
		t.members[nodeID] = m
	}
}

func (t *Table) Snapshot() []Member {
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make([]Member, 0, len(t.members))
	for _, m := range t.members {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out
}

func (t *Table) Entries() []Entry {
	members := t.Snapshot()
	out := make([]Entry, 0, len(members))
	for _, member := range members {
		if member.Endpoint == "" {
			continue
		}
		out = append(out, Entry{
			NodeID:      member.NodeID,
			Endpoint:    member.Endpoint,
			Status:      member.Status,
			Incarnation: member.Incarnation,
		})
	}
	return out
}

func (t *Table) SelfEntry() Entry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	m := t.members[t.selfNodeID]
	return Entry{NodeID: m.NodeID, Endpoint: m.Endpoint, Status: m.Status, Incarnation: m.Incarnation}
}

func (t *Table) AlivePeerEndpoints() map[string]string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make(map[string]string)
	for nodeID, member := range t.members {
		if nodeID == t.selfNodeID || member.Status != StatusAlive || member.Endpoint == "" {
			continue
		}
		out[nodeID] = member.Endpoint
	}
	return out
}

package membership

import (
	"sync"
	"time"
)

type Status string

const (
	StatusAlive   Status = "alive"
	StatusSuspect Status = "suspect"
	StatusDead    Status = "dead"
)

type Member struct {
	NodeID   string
	Endpoint string
	Status   Status
	LastSeen time.Time
	Missed   int
}

type Table struct {
	selfNodeID string
	mu       sync.RWMutex
	members  map[string]Member
	byTarget map[string]string
}

func NewTable(selfNodeID string, selfEndpoint string) *Table {
	t := &Table{
		selfNodeID: selfNodeID,
		members:  make(map[string]Member),
		byTarget: make(map[string]string),
	}
	t.MarkAlive(selfNodeID, selfEndpoint)
	return t
}

func (t *Table) MarkAlive(nodeID, endpoint string) {
	now := time.Now().UTC()

	t.mu.Lock()
	defer t.mu.Unlock()

	m := t.members[nodeID]
	m.NodeID = nodeID
	if endpoint != "" {
		m.Endpoint = endpoint
		t.byTarget[endpoint] = nodeID
	}
	m.Status = StatusAlive
	m.LastSeen = now
	m.Missed = 0
	t.members[nodeID] = m
}

func (t *Table) MarkMissedByEndpoint(endpoint string, suspectAfter int, deadAfter int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	nodeID, ok := t.byTarget[endpoint]
	if !ok {
		return
	}
	if nodeID == t.selfNodeID {
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
	return out
}

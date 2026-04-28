package membership

import (
	"testing"
	"time"
)

func TestTable_MissedTransitionsToSuspectThenDead(t *testing.T) {
	table := NewTable("self", "self:7000")
	table.MarkAlive("node2", "node2:7000")

	table.MarkMissedByEndpoint("node2:7000", 2, 3)
	snapshot := table.Snapshot()
	member := findMember(snapshot, "node2")
	if member.Status != StatusAlive {
		t.Fatalf("expected alive after first miss, got %s", member.Status)
	}

	table.MarkMissedByEndpoint("node2:7000", 2, 3)
	snapshot = table.Snapshot()
	member = findMember(snapshot, "node2")
	if member.Status != StatusSuspect {
		t.Fatalf("expected suspect after second miss, got %s", member.Status)
	}

	table.MarkMissedByEndpoint("node2:7000", 2, 3)
	snapshot = table.Snapshot()
	member = findMember(snapshot, "node2")
	if member.Status != StatusDead {
		t.Fatalf("expected dead after third miss, got %s", member.Status)
	}
}

func TestTable_TimeoutTransitions(t *testing.T) {
	table := NewTable("self", "self:7000")
	table.MarkAlive("node3", "node3:7000")

	now := time.Now().UTC().Add(10 * time.Second)
	table.ApplyTimeouts(now, 2*time.Second, 4*time.Second)
	snapshot := table.Snapshot()
	member := findMember(snapshot, "node3")
	if member.Status != StatusDead {
		t.Fatalf("expected dead from timeout, got %s", member.Status)
	}
}

func TestTable_TimeoutDoesNotAffectSelf(t *testing.T) {
	table := NewTable("self", "self:7000")
	now := time.Now().UTC().Add(10 * time.Second)
	table.ApplyTimeouts(now, 2*time.Second, 4*time.Second)

	member := findMember(table.Snapshot(), "self")
	if member.Status != StatusAlive {
		t.Fatalf("expected self to stay alive, got %s", member.Status)
	}
}

func TestTable_MissedUnknownEndpointDoesNotCreatePlaceholder(t *testing.T) {
	table := NewTable("self", "self:7000")
	table.MarkMissedByEndpoint("nodeX:7000", 2, 3)

	members := table.Snapshot()
	if len(members) != 1 {
		t.Fatalf("expected only self member, got %d members", len(members))
	}
}

func findMember(members []Member, nodeID string) Member {
	for _, m := range members {
		if m.NodeID == nodeID {
			return m
		}
	}
	return Member{}
}

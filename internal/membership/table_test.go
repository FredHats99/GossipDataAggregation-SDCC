package membership

import (
	"testing"
	"time"
)

func TestTable_MissedTransitionsToSuspectThenDead(t *testing.T) {
	table := NewTable("self", "self:7000")
	table.ObserveAlive("node2", "node2:7000", 1)

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
	table.ObserveAlive("node3", "node3:7000", 1)

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

func TestSameEndpointTreatsOnlyMatchingLocalPortAsSelf(t *testing.T) {
	if !sameEndpoint("127.0.0.1:7000", "0.0.0.0:7000") {
		t.Fatal("expected matching local endpoints to be equal")
	}
	if sameEndpoint("127.0.0.1:7001", "0.0.0.0:7000") {
		t.Fatal("expected different local ports to be different endpoints")
	}
}

func TestTable_MergeOrdersIncarnationAndStatus(t *testing.T) {
	table := newTable("self", "self:7000", 10)
	table.ObserveAlive("node2", "node2:7000", 5)

	if !table.Merge([]Entry{{
		NodeID: "node2", Endpoint: "node2:7000", Status: StatusSuspect, Incarnation: 5,
	}}) {
		t.Fatal("expected suspect entry to advance membership")
	}
	if got := findMember(table.Snapshot(), "node2").Status; got != StatusSuspect {
		t.Fatalf("expected suspect, got %s", got)
	}

	if table.Merge([]Entry{{
		NodeID: "node2", Endpoint: "node2:7000", Status: StatusAlive, Incarnation: 5,
	}}) {
		t.Fatal("stale alive entry must not overwrite suspect at the same incarnation")
	}
	if got := findMember(table.Snapshot(), "node2").Status; got != StatusSuspect {
		t.Fatalf("expected suspect after stale alive, got %s", got)
	}

	if !table.Merge([]Entry{{
		NodeID: "node2", Endpoint: "node2-new:7000", Status: StatusAlive, Incarnation: 6,
	}}) {
		t.Fatal("higher incarnation should advance membership")
	}
	member := findMember(table.Snapshot(), "node2")
	if member.Status != StatusAlive || member.Incarnation != 6 || member.Endpoint != "node2-new:7000" {
		t.Fatalf("unexpected higher-incarnation member: %+v", member)
	}
}

func TestTable_RefutesSuspicionAboutSelf(t *testing.T) {
	table := newTable("self", "self:7000", 10)

	if !table.Merge([]Entry{{
		NodeID: "self", Endpoint: "self:7000", Status: StatusDead, Incarnation: 10,
	}}) {
		t.Fatal("expected self accusation to be refuted")
	}
	self := table.SelfEntry()
	if self.Status != StatusAlive || self.Incarnation != 11 {
		t.Fatalf("expected alive self at incarnation 11, got %+v", self)
	}

	if table.Merge([]Entry{{
		NodeID: "self", Endpoint: "self:7000", Status: StatusDead, Incarnation: 10,
	}}) {
		t.Fatal("stale self accusation should be ignored")
	}
}

func TestTable_OlderProcessCannotOverwriteNewerIncarnation(t *testing.T) {
	table := newTable("self", "self:7000", 10)
	table.ObserveAlive("node2", "node2:7000", 6)
	table.Merge([]Entry{{
		NodeID: "node2", Endpoint: "node2:7000", Status: StatusDead, Incarnation: 7,
	}})

	if table.ObserveAlive("node2", "node2:7000", 6) {
		t.Fatal("older direct observation must not overwrite a newer incarnation")
	}
	if got := findMember(table.Snapshot(), "node2"); got.Status != StatusDead || got.Incarnation != 7 {
		t.Fatalf("unexpected member after old observation: %+v", got)
	}

	if !table.ObserveAlive("node2", "node2:7000", 8) {
		t.Fatal("newer process should restore alive")
	}
	if got := findMember(table.Snapshot(), "node2"); got.Status != StatusAlive || got.Incarnation != 8 {
		t.Fatalf("unexpected member after restart: %+v", got)
	}
}

func TestTable_IgnoresMalformedDisseminatedEntry(t *testing.T) {
	table := newTable("self", "self:7000", 10)
	if table.Merge([]Entry{{
		NodeID: "NOT VALID", Endpoint: "not-an-endpoint", Status: StatusAlive, Incarnation: 1,
	}}) {
		t.Fatal("malformed entry should not advance membership")
	}
	if len(table.Snapshot()) != 1 {
		t.Fatalf("malformed entry poisoned membership: %+v", table.Snapshot())
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

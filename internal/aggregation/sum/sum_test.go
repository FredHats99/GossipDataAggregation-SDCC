package sum

import (
	"reflect"
	"testing"
)

func TestGCounterUpdateEstimateAndSerializeRoundTrip(t *testing.T) {
	counter, err := New("node1")
	if err != nil {
		t.Fatalf("new counter: %v", err)
	}
	if advanced, err := counter.Update(uint64(5)); err != nil || !advanced {
		t.Fatalf("update failed advanced=%v err=%v", advanced, err)
	}
	if advanced, err := counter.Update(7); err != nil || !advanced {
		t.Fatalf("update failed advanced=%v err=%v", advanced, err)
	}

	estimate, err := counter.Estimate(nil)
	if err != nil {
		t.Fatalf("estimate: %v", err)
	}
	if estimate.(uint64) != 12 {
		t.Fatalf("expected sum 12, got %v", estimate)
	}

	raw, err := counter.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	restored, err := New("node1")
	if err != nil {
		t.Fatalf("new restored counter: %v", err)
	}
	if err := restored.Deserialize(raw); err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	if !reflect.DeepEqual(counter.State(), restored.State()) {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", restored.State(), counter.State())
	}
}

func TestGCounterMergeLaws(t *testing.T) {
	a := State{Contrib: map[string]uint64{"node1": 5}, Version: 1}
	b := State{Contrib: map[string]uint64{"node2": 7, "node1": 3}, Version: 1}
	c := State{Contrib: map[string]uint64{"node3": 11, "node2": 2}, Version: 1}

	ab := mergedState(t, "node1", a, b)
	ba := mergedState(t, "node1", b, a)
	if !reflect.DeepEqual(ab.Contrib, ba.Contrib) {
		t.Fatalf("commutativity failed: ab=%+v ba=%+v", ab.Contrib, ba.Contrib)
	}

	left := mergedState(t, "node1", mergedState(t, "node1", a, b), c)
	right := mergedState(t, "node1", a, mergedState(t, "node1", b, c))
	if !reflect.DeepEqual(left.Contrib, right.Contrib) {
		t.Fatalf("associativity failed: left=%+v right=%+v", left.Contrib, right.Contrib)
	}

	aa := mergedState(t, "node1", a, a)
	if !reflect.DeepEqual(a.Contrib, aa.Contrib) {
		t.Fatalf("idempotency failed: got %+v want %+v", aa.Contrib, a.Contrib)
	}
}

func TestGCounterMergeDuplicateDoesNotAdvance(t *testing.T) {
	counter, err := FromState("node1", State{Contrib: map[string]uint64{"node1": 5}, Version: 1})
	if err != nil {
		t.Fatalf("from state: %v", err)
	}
	peer := State{Contrib: map[string]uint64{"node2": 7}, Version: 1}
	if advanced, err := counter.Merge(peer); err != nil || !advanced {
		t.Fatalf("expected first merge to advance, advanced=%v err=%v", advanced, err)
	}
	if advanced, err := counter.Merge(peer); err != nil || advanced {
		t.Fatalf("expected duplicate merge not to advance, advanced=%v err=%v", advanced, err)
	}
}

func mergedState(t *testing.T, nodeID string, states ...State) State {
	t.Helper()
	counter, err := New(nodeID)
	if err != nil {
		t.Fatalf("new counter: %v", err)
	}
	for _, state := range states {
		if _, err := counter.Merge(state); err != nil {
			t.Fatalf("merge: %v", err)
		}
	}
	return counter.State()
}

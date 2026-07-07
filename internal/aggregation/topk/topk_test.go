package topk

import (
	"reflect"
	"testing"
	"time"
)

func TestSetUpdateEstimateAndSerializeRoundTrip(t *testing.T) {
	set, err := New(2)
	if err != nil {
		t.Fatalf("new set: %v", err)
	}
	c1 := candidate("item-a", 10, "2026-05-05T10:00:00Z", "node1")
	c2 := candidate("item-b", 20, "2026-05-05T10:00:00Z", "node2")
	c3 := candidate("item-c", 15, "2026-05-05T10:00:00Z", "node3")

	for _, c := range []Candidate{c1, c2, c3} {
		if advanced, err := set.Update(c); err != nil || !advanced {
			t.Fatalf("update failed advanced=%v err=%v", advanced, err)
		}
	}

	estimate, err := set.Estimate(2)
	if err != nil {
		t.Fatalf("estimate: %v", err)
	}
	got := estimate.([]Candidate)
	if len(got) != 2 || got[0].ItemID != "item-b" || got[1].ItemID != "item-c" {
		t.Fatalf("unexpected top2: %+v", got)
	}

	raw, err := set.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	restored, err := New(2)
	if err != nil {
		t.Fatalf("new restored: %v", err)
	}
	if err := restored.Deserialize(raw); err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	if !reflect.DeepEqual(set.State(), restored.State()) {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", restored.State(), set.State())
	}
}

func TestSetDeterministicTieBreaking(t *testing.T) {
	set, err := New(4)
	if err != nil {
		t.Fatalf("new set: %v", err)
	}
	for _, c := range []Candidate{
		candidate("item-d", 10, "2026-05-05T10:00:00Z", "node2"),
		candidate("item-c", 10, "2026-05-05T10:00:00Z", "node1"),
		candidate("item-a", 10, "2026-05-05T11:00:00Z", "node3"),
		candidate("item-b", 20, "2026-05-05T09:00:00Z", "node1"),
	} {
		if _, err := set.Update(c); err != nil {
			t.Fatalf("update: %v", err)
		}
	}
	estimate, err := set.Estimate(nil)
	if err != nil {
		t.Fatalf("estimate: %v", err)
	}
	got := itemIDs(estimate.([]Candidate))
	want := []string{"item-b", "item-a", "item-c", "item-d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected order: got %v want %v", got, want)
	}
}

func TestSetMergeLaws(t *testing.T) {
	a := State{Candidates: []Candidate{
		candidate("item-a", 10, "2026-05-05T10:00:00Z", "node1"),
		candidate("item-b", 20, "2026-05-05T10:00:00Z", "node2"),
	}, Version: 1}
	b := State{Candidates: []Candidate{
		candidate("item-c", 30, "2026-05-05T10:00:00Z", "node3"),
		candidate("item-d", 15, "2026-05-05T10:00:00Z", "node1"),
	}, Version: 1}
	c := State{Candidates: []Candidate{
		candidate("item-e", 25, "2026-05-05T10:00:00Z", "node2"),
		candidate("item-f", 5, "2026-05-05T10:00:00Z", "node3"),
	}, Version: 1}

	ab := mergedState(t, 4, a, b)
	ba := mergedState(t, 4, b, a)
	if !reflect.DeepEqual(ab.Candidates, ba.Candidates) {
		t.Fatalf("commutativity failed: ab=%+v ba=%+v", ab.Candidates, ba.Candidates)
	}

	left := mergedState(t, 4, mergedState(t, 4, a, b), c)
	right := mergedState(t, 4, a, mergedState(t, 4, b, c))
	if !reflect.DeepEqual(left.Candidates, right.Candidates) {
		t.Fatalf("associativity failed: left=%+v right=%+v", left.Candidates, right.Candidates)
	}

	aa := mergedState(t, 4, a, a)
	if !reflect.DeepEqual(mergedState(t, 4, a).Candidates, aa.Candidates) {
		t.Fatalf("idempotency failed: got %+v", aa.Candidates)
	}
}

func TestSetMergeDuplicateDoesNotAdvance(t *testing.T) {
	state := State{Candidates: []Candidate{candidate("item-a", 10, "2026-05-05T10:00:00Z", "node1")}, Version: 1}
	set, err := New(3)
	if err != nil {
		t.Fatalf("new set: %v", err)
	}
	if advanced, err := set.Merge(state); err != nil || !advanced {
		t.Fatalf("expected first merge to advance, advanced=%v err=%v", advanced, err)
	}
	if advanced, err := set.Merge(state); err != nil || advanced {
		t.Fatalf("expected duplicate merge not to advance, advanced=%v err=%v", advanced, err)
	}
}

func candidate(itemID string, score float64, ts string, origin string) Candidate {
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		panic(err)
	}
	return Candidate{
		ItemID:       itemID,
		Score:        score,
		EventTS:      parsed,
		OriginNodeID: origin,
	}
}

func mergedState(t *testing.T, kmax int, states ...State) State {
	t.Helper()
	set, err := New(kmax)
	if err != nil {
		t.Fatalf("new set: %v", err)
	}
	for _, state := range states {
		if _, err := set.Merge(state); err != nil {
			t.Fatalf("merge: %v", err)
		}
	}
	return set.State()
}

func itemIDs(candidates []Candidate) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.ItemID)
	}
	return out
}

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gossipdataaggregation-sdcc/internal/membership"
)

func TestMembersEndpoint(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	handler := NewMembersHandler(func() []membership.Member {
		return []membership.Member{
			{
				NodeID:   "node1",
				Endpoint: "node1:7000",
				Status:   membership.StatusAlive,
				LastSeen: now,
			},
		}
	})

	mux := http.NewServeMux()
	handler.Register(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/members", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected /members 200, got %d", rec.Code)
	}

	var body map[string][]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body["members"]) != 1 {
		t.Fatalf("expected 1 member, got %d", len(body["members"]))
	}
	if body["members"][0]["node_id"] != "node1" {
		t.Fatalf("unexpected node_id: %v", body["members"][0]["node_id"])
	}
}

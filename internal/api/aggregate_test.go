package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gossipdataaggregation-sdcc/internal/aggregation/pipeline"
)

func TestAggregationUpdateAndSumEndpoint(t *testing.T) {
	manager := newTestManager(t)
	handler := NewAggregationHandler(manager)
	mux := http.NewServeMux()
	handler.Register(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/update", strings.NewReader(`{"aggregate_type":"SUM","value":5}`))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected /update 202, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/aggregate/sum", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected /aggregate/sum 200, got %d", rec.Code)
	}

	var body map[string]uint64
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode sum response: %v", err)
	}
	if body["value"] != 5 {
		t.Fatalf("expected sum 5, got %d", body["value"])
	}
}

func TestAggregationUpdateAndTopKEndpoint(t *testing.T) {
	manager := newTestManager(t)
	handler := NewAggregationHandler(manager)
	mux := http.NewServeMux()
	handler.Register(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/update", strings.NewReader(`{"aggregate_type":"TOPK","value":{"item_id":"item-a","score":9.5}}`))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected /update 202, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/aggregate/topk?k=1", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected /aggregate/topk 200, got %d", rec.Code)
	}

	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode topk response: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0]["item_id"] != "item-a" {
		t.Fatalf("unexpected topk response: %+v", body.Items)
	}
}

func TestAggregationUpdateRejectsInvalidPayload(t *testing.T) {
	manager := newTestManager(t)
	handler := NewAggregationHandler(manager)
	mux := http.NewServeMux()
	handler.Register(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/update", strings.NewReader(`{"aggregate_type":"SUM","value":0}`))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected /update 400, got %d", rec.Code)
	}
}

func newTestManager(t *testing.T) *pipeline.Manager {
	t.Helper()
	manager, err := pipeline.New(pipeline.Config{
		NodeID:            "node1",
		TopKMax:           5,
		OutboundQueueSize: 8,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	return manager
}

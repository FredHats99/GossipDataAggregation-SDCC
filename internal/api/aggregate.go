package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"gossipdataaggregation-sdcc/internal/aggregation/common"
	"gossipdataaggregation-sdcc/internal/aggregation/pipeline"
	"gossipdataaggregation-sdcc/internal/aggregation/topk"
)

type AggregationHandler struct {
	manager *pipeline.Manager
}

type updateRequest struct {
	AggregateType string          `json:"aggregate_type"`
	Value         json.RawMessage `json:"value"`
}

type topKUpdateRequest struct {
	ItemID  string    `json:"item_id"`
	Score   float64   `json:"score"`
	EventTS time.Time `json:"event_ts"`
}

func NewAggregationHandler(manager *pipeline.Manager) *AggregationHandler {
	return &AggregationHandler{manager: manager}
}

func (h *AggregationHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/update", h.update)
	mux.HandleFunc("/aggregate/sum", h.sum)
	mux.HandleFunc("/aggregate/topk", h.topk)
}

func (h *AggregationHandler) update(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req updateRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid update payload"})
		return
	}

	value, err := decodeUpdateValue(req)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	delta, advanced, err := h.manager.ApplyLocalUpdate(pipeline.LocalUpdate{
		AggregateType: req.AggregateType,
		Value:         value,
	})
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusAccepted, map[string]any{
		"advanced": advanced,
		"delta":    delta,
	})
}

func (h *AggregationHandler) sum(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	estimates, err := h.manager.Estimates(1)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, map[string]uint64{"value": estimates.SUM})
}

func (h *AggregationHandler) topk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	k := 10
	if raw := r.URL.Query().Get("k"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "k must be greater than zero"})
			return
		}
		k = parsed
	}

	estimates, err := h.manager.Estimates(k)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"items": estimates.TOPK})
}

func decodeUpdateValue(req updateRequest) (any, error) {
	switch req.AggregateType {
	case common.AggregateSUM:
		return decodeSUMUpdateValue(req.Value)
	case common.AggregateTOPK:
		return decodeTOPKUpdateValue(req.Value)
	default:
		return nil, pipeline.ErrInvalidUpdate
	}
}

func decodeSUMUpdateValue(raw json.RawMessage) (uint64, error) {
	if len(raw) == 0 {
		return 0, pipeline.ErrInvalidUpdate
	}

	var wrapped struct {
		Increment uint64 `json:"increment"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Increment > 0 {
		return wrapped.Increment, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err != nil {
		return 0, errors.New("SUM value must be a positive integer or {\"increment\":n}")
	}
	value, err := strconv.ParseUint(number.String(), 10, 64)
	if err != nil || value == 0 {
		return 0, errors.New("SUM value must be a positive integer")
	}
	return value, nil
}

func decodeTOPKUpdateValue(raw json.RawMessage) (topk.Candidate, error) {
	var req topKUpdateRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return topk.Candidate{}, errors.New("TOPK value must be an object")
	}
	if req.EventTS.IsZero() {
		req.EventTS = time.Now().UTC()
	}
	return topk.Candidate{
		ItemID:  req.ItemID,
		Score:   req.Score,
		EventTS: req.EventTS,
	}, nil
}

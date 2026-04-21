package api

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
)

type HealthHandler struct {
	ready atomic.Bool
}

func NewHealthHandler() *HealthHandler {
	h := &HealthHandler{}
	h.ready.Store(true)
	return h
}

func (h *HealthHandler) SetReady(ready bool) {
	h.ready.Store(ready)
}

func (h *HealthHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.healthz)
	mux.HandleFunc("/readyz", h.readyz)
}

func (h *HealthHandler) healthz(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *HealthHandler) readyz(w http.ResponseWriter, _ *http.Request) {
	if !h.ready.Load() {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func respondJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

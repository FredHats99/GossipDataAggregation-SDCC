package api

import (
	"net/http"
	"time"

	"gossipdataaggregation-sdcc/internal/membership"
)

type MembersProvider func() []membership.Member

type MembersHandler struct {
	getMembers MembersProvider
}

type memberResponse struct {
	NodeID   string `json:"node_id"`
	Endpoint string `json:"endpoint"`
	Status   string `json:"status"`
	LastSeen string `json:"last_seen"`
}

func NewMembersHandler(provider MembersProvider) *MembersHandler {
	return &MembersHandler{getMembers: provider}
}

func (h *MembersHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/members", h.members)
}

func (h *MembersHandler) members(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	raw := h.getMembers()
	out := make([]memberResponse, 0, len(raw))
	for _, m := range raw {
		out = append(out, memberResponse{
			NodeID:   m.NodeID,
			Endpoint: m.Endpoint,
			Status:   string(m.Status),
			LastSeen: m.LastSeen.UTC().Format(time.RFC3339Nano),
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{"members": out})
}

package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/thomas666-beast/nexus-payments/internal/store"
)

// Handler serves /healthz (liveness) and /readyz (readiness).
type Handler struct {
	db *store.DB
}

func New(db *store.DB) *Handler { return &Handler{db: db} }

// Liveness reports that the process is running.
func (h *Handler) Liveness(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Readiness reports that the process can serve traffic (DB reachable).
func (h *Handler) Readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := h.db.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not_ready",
			"error":  err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

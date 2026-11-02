// Package hosted contains the hosted API handlers as plain net/http.
package hosted

import (
	"encoding/json"
	"net/http"

	"github.com/C-Azzone416/meal-planner/internal/core/version"
)

// HealthResponse is the body returned by GET /api/health.
type HealthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

// Health serves GET /api/health.
func Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(HealthResponse{
		Status:  "ok",
		Service: "meal-planner-api",
		Version: version.Version,
	})
}

// Package handler is the Vercel Go function for /api/health.
package handler

import (
	"net/http"

	"github.com/C-Azzone416/meal-planner/internal/hosted"
)

// Handler is the Vercel entrypoint; it delegates to internal/hosted.
func Handler(w http.ResponseWriter, r *http.Request) {
	hosted.Health(w, r)
}

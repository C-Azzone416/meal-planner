// Package handler is the Vercel Go function for /api/auth/authorize.
package handler

import (
	"net/http"

	"github.com/C-Azzone416/meal-planner/hosted"
)

// Handler verifies the provider token against this environment's identity policy.
func Handler(w http.ResponseWriter, r *http.Request) {
	hosted.Authorize(w, r)
}

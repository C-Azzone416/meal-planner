// Package handler is the Vercel Go function for /api/auth-config.
package handler

import (
	"net/http"

	"github.com/C-Azzone416/meal-planner/hosted"
)

// Handler is the Vercel entrypoint; it delegates to hosted.
func Handler(w http.ResponseWriter, r *http.Request) {
	hosted.AuthConfig(w, r)
}

// Package handler is the Vercel Go function for /api/auth-proxy/*.
package handler

import (
	"net/http"

	"github.com/C-Azzone416/meal-planner/hosted"
)

// Handler is the Vercel entrypoint; it delegates to the narrow hosted proxy.
func Handler(w http.ResponseWriter, r *http.Request) {
	hosted.AuthProxyFromEnvironment(w, r)
}

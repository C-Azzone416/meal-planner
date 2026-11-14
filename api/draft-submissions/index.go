// Package handler is the disabled-by-default Preview-only draft ingress.
// It does not accept browser session JWTs and never issues grants or migrates.
package handler

import (
	"context"
	"github.com/C-Azzone416/meal-planner/hosted"
	"net/http"
	"os"
	"sync"
)

var once sync.Once
var store hosted.DraftRepository

func Handler(w http.ResponseWriter, r *http.Request) {
	config := hosted.DraftIngestConfigFromEnvironment()
	if config.Enabled {
		once.Do(func() {
			s, err := hosted.OpenDraftStore(context.Background(), os.Getenv("MEAL_DRAFTS_DATABASE_URL"))
			if err == nil {
				store = s
			}
		})
	}
	hosted.DraftIngestHandler(store, config).ServeHTTP(w, r)
}

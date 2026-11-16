// Package handler exposes outbound-only pairing polling.
package handler

import (
	"context"
	"net/http"
	"os"
	"sync"

	"github.com/C-Azzone416/meal-planner/hosted"
)

var once sync.Once
var store hosted.DraftPairingRepository

func Handler(w http.ResponseWriter, r *http.Request) {
	config := hosted.DraftPairingConfigFromEnvironment()
	if config.Enabled {
		once.Do(func() {
			s, err := hosted.OpenDraftPairingStore(context.Background(), os.Getenv("MEAL_DRAFTS_DATABASE_URL"))
			if err == nil {
				store = s
			}
		})
	}
	hosted.DraftPairingPollHandler(store, config).ServeHTTP(w, r)
}

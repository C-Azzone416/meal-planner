// Package handler exposes signed-in owner connection listing and revocation.
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
	if !config.Enabled {
		hosted.DraftGrantsHandler(nil, config).ServeHTTP(w, r)
		return
	}
	once.Do(func() {
		s, err := hosted.OpenDraftPairingStore(context.Background(), os.Getenv("MEAL_DRAFTS_DATABASE_URL"))
		if err == nil {
			store = s
		}
	})
	hosted.RequireUser(hosted.DraftGrantsHandler(store, config)).ServeHTTP(w, r)
}

// Package handler exposes signed-in owner pairing approval.
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
		hosted.DraftPairingApproveHandler(nil, config).ServeHTTP(w, r)
		return
	}
	once.Do(func() {
		s, err := hosted.OpenDraftPairingStore(context.Background(), os.Getenv("MEAL_DRAFTS_DATABASE_URL"))
		if err == nil {
			store = s
		}
	})
	hosted.RequireUser(hosted.DraftPairingApproveHandler(store, config)).ServeHTTP(w, r)
}

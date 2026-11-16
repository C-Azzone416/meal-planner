package handler

import (
	"context"
	"net/http"
	"os"
	"sync"

	"github.com/C-Azzone416/meal-planner/hosted"
)

var once sync.Once
var store *hosted.PostgresDraftWorkflowStore

func Handler(w http.ResponseWriter, r *http.Request) {
	config := hosted.DraftWorkflowConfigFromEnvironment()
	if !config.Enabled {
		hosted.DraftIntentPollHandler(nil, config).ServeHTTP(w, r)
		return
	}
	once.Do(func() {
		store, _ = hosted.OpenDraftWorkflowStore(context.Background(), os.Getenv("MEAL_DRAFTS_DATABASE_URL"))
	})
	hosted.DraftIntentPollHandler(store, config).ServeHTTP(w, r)
}

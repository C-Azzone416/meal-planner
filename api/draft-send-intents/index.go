// Package handler exposes the disabled-by-default Preview draft proposal route.
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

func workflowStore() *hosted.PostgresDraftWorkflowStore {
	once.Do(func() {
		store, _ = hosted.OpenDraftWorkflowStore(context.Background(), os.Getenv("MEAL_DRAFTS_DATABASE_URL"))
	})
	return store
}

func Handler(w http.ResponseWriter, r *http.Request) {
	config := hosted.DraftWorkflowConfigFromEnvironment()
	if !config.Enabled {
		hosted.DraftIntentCreateHandler(nil, config).ServeHTTP(w, r)
		return
	}
	hosted.DraftIntentCreateHandler(workflowStore(), config).ServeHTTP(w, r)
}

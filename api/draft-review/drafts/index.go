package handler

import (
	"context"
	"net/http"
	"os"
	"sync"

	"github.com/C-Azzone416/meal-planner/hosted"
)

var once sync.Once
var draftStore *hosted.PostgresDraftWorkflowStore
var recipeStore *hosted.PostgresRecipeStore

func Handler(w http.ResponseWriter, r *http.Request) {
	config := hosted.DraftWorkflowConfigFromEnvironment()
	if !config.Enabled {
		hosted.OwnerDraftHandler(nil, nil, config).ServeHTTP(w, r)
		return
	}
	once.Do(func() {
		draftStore, _ = hosted.OpenDraftWorkflowStore(context.Background(), os.Getenv("MEAL_DRAFTS_DATABASE_URL"))
		recipeStore, _ = hosted.OpenRecipeStore(context.Background(), os.Getenv("MEAL_RECIPES_DATABASE_URL"))
	})
	hosted.RequireUser(hosted.OwnerDraftHandler(draftStore, recipeStore, config)).ServeHTTP(w, r)
}

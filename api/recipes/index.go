// Package handler is the authenticated manual-recipe API. Its database
// connection is server-only and never applies migrations automatically.
package handler

import (
	"context"
	"net/http"
	"os"
	"sync"

	"github.com/C-Azzone416/meal-planner/hosted"
)

var recipeStoreOnce sync.Once
var recipeStore hosted.RecipeRepository

func Handler(w http.ResponseWriter, r *http.Request) {
	hosted.RequireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recipeStoreOnce.Do(func() {
			store, err := hosted.OpenRecipeStore(context.Background(), os.Getenv("MEAL_RECIPES_DATABASE_URL"))
			if err == nil {
				recipeStore = store
			}
		})
		hosted.RecipesHandler(recipeStore).ServeHTTP(w, r)
	})).ServeHTTP(w, r)
}

package handler

import (
	"github.com/C-Azzone416/meal-planner/hosted"
	"net/http"
)

func Handler(w http.ResponseWriter, r *http.Request) {
	hosted.FeedbackHandler("reports").ServeHTTP(w, r)
}

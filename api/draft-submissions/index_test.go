package handler

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerDisabledByDefaultAndInProduction(t *testing.T) {
	t.Setenv("MEAL_DRAFTS_DATABASE_URL", "postgres://synthetic:not-a-real-secret@invalid/db?sslmode=verify-full")
	for _, environment := range []string{"", "production", "development", "preview"} {
		t.Run(environment, func(t *testing.T) {
			t.Setenv("VERCEL_ENV", environment)
			t.Setenv("MEAL_DRAFT_INGEST_ENABLED", "false")
			if environment == "production" {
				t.Setenv("MEAL_DRAFT_INGEST_ENABLED", "true")
			}
			req := httptest.NewRequest("POST", "/api/draft-submissions", strings.NewReader(`{}`))
			w := httptest.NewRecorder()
			Handler(w, req)
			if w.Code != 404 || store != nil {
				t.Fatal("disabled route attempted database initialization")
			}
		})
	}
}

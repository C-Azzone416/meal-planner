package handler

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDisabledSourceGateDoesNotInitializeOrAuthenticate(t *testing.T) {
	t.Setenv("VERCEL_ENV", "preview")
	t.Setenv("MEAL_DRAFT_PAIRING_ENABLED", "true")
	t.Setenv("MEAL_DRAFT_PAIRING_ORIGIN", "https://athome-meals-dev.vercel.app")
	t.Setenv("MEAL_DRAFT_MEMBERSHIP_EPOCH", "epoch_000000000001")
	t.Setenv("MEAL_DRAFTS_DATABASE_URL", "postgres://synthetic:secret@invalid/db?sslmode=verify-full")
	r := httptest.NewRequest("POST", "/api/draft-pairings/approve", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	Handler(w, r)
	if w.Code != 404 || store != nil {
		t.Fatalf("disabled handler returned %d or initialized storage", w.Code)
	}
}

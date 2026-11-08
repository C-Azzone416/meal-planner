package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerFailsClosedWithoutAuthConfiguration(t *testing.T) {
	t.Setenv("NEON_AUTH_BASE_URL", "")
	t.Setenv("AUTH_ALLOWED_EMAIL", "")
	rec := httptest.NewRecorder()
	Handler(rec, httptest.NewRequest(http.MethodGet, "/api/recipes", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatal(rec.Code)
	}
}

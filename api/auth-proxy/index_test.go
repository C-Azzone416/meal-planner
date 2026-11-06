package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerFailsClosedWithoutConfiguration(t *testing.T) {
	t.Setenv("NEON_AUTH_BASE_URL", "")
	rec := httptest.NewRecorder()
	Handler(rec, httptest.NewRequest(http.MethodGet, "/api/auth-proxy?path=get-session", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

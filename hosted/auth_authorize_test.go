package hosted

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthorizeAcceptsOnlyConfiguredIdentity(t *testing.T) {
	fixture := newBearerFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/authorize", nil)
	req.Header.Set("Authorization", "Bearer "+fixture.token(t, fixture.claims()))
	rec := httptest.NewRecorder()

	AuthorizeWithConfig(fixture.config()).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if cache := rec.Header().Get("Cache-Control"); cache != "no-store" {
		t.Fatalf("cache-control = %q", cache)
	}
}

func TestAuthorizeRejectsMissingBearer(t *testing.T) {
	fixture := newBearerFixture(t)
	rec := httptest.NewRecorder()
	AuthorizeWithConfig(fixture.config()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/authorize", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuthorizeRejectsOtherMethods(t *testing.T) {
	fixture := newBearerFixture(t)
	rec := httptest.NewRecorder()
	AuthorizeWithConfig(fixture.config()).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/authorize", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

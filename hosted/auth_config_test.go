package hosted

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthConfigDisabledWithoutBaseURL(t *testing.T) {
	t.Setenv("NEON_AUTH_BASE_URL", "")
	t.Setenv("AUTH_ALLOWED_EMAIL", testAllowedEmail)
	t.Setenv("NEON_AUTH_ENDPOINT_ID", testEndpointID)
	rec := httptest.NewRecorder()
	AuthConfig(rec, httptest.NewRequest(http.MethodGet, "/api/auth-config", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got AuthConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.BaseURL != "" {
		t.Fatalf("response = %+v, want disabled with no base URL", got)
	}
}

func TestAuthConfigDisabledWithoutIdentityPolicy(t *testing.T) {
	t.Setenv("NEON_AUTH_BASE_URL", "https://auth.invalid/example/auth")
	t.Setenv("AUTH_ALLOWED_EMAIL", "")
	t.Setenv("NEON_AUTH_ENDPOINT_ID", testEndpointID)
	rec := httptest.NewRecorder()
	AuthConfig(rec, httptest.NewRequest(http.MethodGet, "/api/auth-config", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got AuthConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.BaseURL != "" {
		t.Fatalf("response = %+v, want disabled without an identity policy", got)
	}
}

func TestAuthConfigDisabledWithoutEnvironmentBinding(t *testing.T) {
	t.Setenv("NEON_AUTH_BASE_URL", "https://auth.invalid/example/auth")
	t.Setenv("AUTH_ALLOWED_EMAIL", testAllowedEmail)
	t.Setenv("NEON_AUTH_ENDPOINT_ID", "")
	rec := httptest.NewRecorder()
	AuthConfig(rec, httptest.NewRequest(http.MethodGet, "/api/auth-config", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got AuthConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.BaseURL != "" {
		t.Fatalf("response = %+v, want disabled without environment binding", got)
	}
}

func TestAuthConfigReturnsNormalizedPublicBaseURL(t *testing.T) {
	t.Setenv("NEON_AUTH_BASE_URL", "https://auth.invalid/example/auth/")
	t.Setenv("AUTH_ALLOWED_EMAIL", testAllowedEmail)
	t.Setenv("NEON_AUTH_ENDPOINT_ID", testEndpointID)
	rec := httptest.NewRecorder()
	AuthConfig(rec, httptest.NewRequest(http.MethodGet, "/api/auth-config", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got AuthConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.BaseURL != "https://auth.invalid/example/auth" {
		t.Fatalf("response = %+v", got)
	}
	if cache := rec.Header().Get("Cache-Control"); cache != "no-store" {
		t.Fatalf("cache-control = %q", cache)
	}
}

func TestAuthConfigRejectsUnsafeBaseURL(t *testing.T) {
	t.Setenv("NEON_AUTH_BASE_URL", "http://auth.invalid/example/auth")
	t.Setenv("AUTH_ALLOWED_EMAIL", testAllowedEmail)
	t.Setenv("NEON_AUTH_ENDPOINT_ID", testEndpointID)
	rec := httptest.NewRecorder()
	AuthConfig(rec, httptest.NewRequest(http.MethodGet, "/api/auth-config", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestAuthConfigRejectsOtherMethods(t *testing.T) {
	rec := httptest.NewRecorder()
	AuthConfig(rec, httptest.NewRequest(http.MethodPost, "/api/auth-config", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

package hosted

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
	t.Setenv("NEON_AUTH_BASE_URL", "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth")
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
	t.Setenv("NEON_AUTH_BASE_URL", "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth")
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

func TestAuthConfigReturnsSameOriginProxyWithoutExposingProviderURL(t *testing.T) {
	t.Setenv("NEON_AUTH_BASE_URL", "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth/")
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
	if !got.Enabled || got.BaseURL != publicAuthProxyBaseURL {
		t.Fatalf("response = %+v", got)
	}
	if strings.Contains(rec.Body.String(), "neonauth") {
		t.Fatalf("response exposed the provider URL: %s", rec.Body.String())
	}
	if cache := rec.Header().Get("Cache-Control"); cache != "no-store" {
		t.Fatalf("cache-control = %q", cache)
	}
}

func TestAuthConfigRejectsProviderEndpointMismatch(t *testing.T) {
	t.Setenv("NEON_AUTH_BASE_URL", "https://ep-other.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth")
	t.Setenv("AUTH_ALLOWED_EMAIL", testAllowedEmail)
	t.Setenv("NEON_AUTH_ENDPOINT_ID", testEndpointID)
	rec := httptest.NewRecorder()
	AuthConfig(rec, httptest.NewRequest(http.MethodGet, "/api/auth-config", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
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

func TestAuthPolicyFailsClosedAcrossRuntimeEntryPoints(t *testing.T) {
	const validURL = testIssuer + "/neondb/auth"
	cases := []struct {
		name, url, endpoint, email string
		configStatus               int
	}{
		{"missing email", validURL, testEndpointID, "", 200},
		{"blank email", validURL, testEndpointID, "  ", 200},
		{"missing endpoint", validURL, "", testAllowedEmail, 200},
		{"missing URL", "", testEndpointID, testAllowedEmail, 200},
		{"bare query", validURL + "?", testEndpointID, testAllowedEmail, 500},
		{"bare fragment", validURL + "#", testEndpointID, testAllowedEmail, 500},
		{"query", validURL + "?a=b", testEndpointID, testAllowedEmail, 500},
		{"endpoint mismatch", validURL, "ep-wrong", testAllowedEmail, 500},
		{"uppercase local", validURL, testEndpointID, "Allowed@example.invalid", 500},
		{"uppercase domain", validURL, testEndpointID, "allowed@EXAMPLE.invalid", 500},
		{"quoted local", validURL, testEndpointID, `"allowed"@example.invalid`, 500},
		{"Unicode local", validURL, testEndpointID, "álowed@example.invalid", 500},
		{"Unicode domain", validURL, testEndpointID, "allowed@éxample.invalid", 500},
		{"single-label domain", validURL, testEndpointID, "allowed@example", 500},
		{"leading domain hyphen", validURL, testEndpointID, "allowed@-example.invalid", 500},
		{"trailing domain hyphen", validURL, testEndpointID, "allowed@example-.invalid", 500},
		{"leading local dot", validURL, testEndpointID, ".allowed@example.invalid", 500},
		{"trailing local dot", validURL, testEndpointID, "allowed.@example.invalid", 500},
		{"duplicate local dot", validURL, testEndpointID, "allowed..user@example.invalid", 500},
		{"padded config email", validURL, testEndpointID, " allowed@example.invalid ", 500},
		{"local length", validURL, testEndpointID, strings.Repeat("a", 65) + "@example.invalid", 500},
		{"total length", validURL, testEndpointID, "a@" + strings.Repeat("b", 245) + ".invalid", 500},
		{"secret placeholder", validURL, testEndpointID, "[SENSITIVE]", 500},
		{"malformed email", validURL, testEndpointID, "not-an-email", 500},
		{"display name", validURL, testEndpointID, "User <allowed@example.invalid>", 500},
		{"email list", validURL, testEndpointID, "a@example.invalid,b@example.invalid", 500},
		{"email comment", validURL, testEndpointID, "allowed@example.invalid (User)", 500},
		{"email newline", validURL, testEndpointID, "allowed@\nexample.invalid", 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NEON_AUTH_BASE_URL", tc.url)
			t.Setenv("NEON_AUTH_ENDPOINT_ID", tc.endpoint)
			t.Setenv("AUTH_ALLOWED_EMAIL", tc.email)
			rec := httptest.NewRecorder()
			AuthConfig(rec, httptest.NewRequest("GET", "/api/auth-config", nil))
			if rec.Code != tc.configStatus || strings.Contains(rec.Body.String(), `"enabled":true`) {
				t.Fatalf("config: %d %s", rec.Code, rec.Body.String())
			}
			client := authProxyDoer(func(*http.Request) (*http.Response, error) {
				t.Error("invalid policy reached upstream")
				return nil, errors.New("unexpected upstream")
			})
			for route, methods := range authProxyRoutes {
				for method := range methods {
					rec = httptest.NewRecorder()
					AuthProxy(AuthProxyConfig{BaseURL: tc.url, ExpectedEndpoint: tc.endpoint, AllowedEmail: tc.email, Client: client}).ServeHTTP(rec, httptest.NewRequest(method, "https://meal.invalid/api/auth-proxy/"+route, nil))
					if rec.Code != 503 {
						t.Errorf("proxy %s %s: %d", method, route, rec.Code)
					}
				}
			}
			_, err := VerifyBearer(context.Background(), "", BearerAuthConfig{BaseURL: tc.url, ExpectedEndpoint: tc.endpoint, AllowedEmail: tc.email, Client: client})
			if !errors.Is(err, ErrAuthConfiguration) {
				t.Errorf("bearer error: %v", err)
			}
			rec = httptest.NewRecorder()
			AuthBeforeCreate(AuthWebhookConfig{BaseURL: tc.url, ExpectedEndpoint: tc.endpoint, AllowedEmail: tc.email, Client: client}).ServeHTTP(rec, httptest.NewRequest("POST", "/api/auth-before-create", nil))
			if rec.Code != 503 {
				t.Errorf("webhook status: %d", rec.Code)
			}
		})
	}
}

func TestAuthPolicyAcceptsDeploymentEmailShape(t *testing.T) {
	for _, email := range []string{
		"allowed@example.invalid", "first.last+tag_123%test-x@sub-domain.example.invalid",
		strings.Repeat("a", 64) + "@example.invalid",
		"a@" + strings.Repeat("b", 244) + ".invalid",
	} {
		if _, err := normalizeAuthPolicy(testIssuer+"/neondb/auth", testEndpointID, email); err != nil {
			t.Errorf("deployment-valid email rejected: %v", err)
		}
	}
}

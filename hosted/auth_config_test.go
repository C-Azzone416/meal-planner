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
		{"empty port", testIssuer + ":/neondb/auth", testEndpointID, testAllowedEmail, 500},
		{"default port", testIssuer + ":443/neondb/auth", testEndpointID, testAllowedEmail, 500},
		{"double colon", testIssuer + "::/neondb/auth", testEndpointID, testAllowedEmail, 500},
		{"trailing host dot", testIssuer + "./neondb/auth", testEndpointID, testAllowedEmail, 500},
		{"bracketed host", "https://[" + strings.TrimPrefix(testIssuer, "https://") + "]/neondb/auth", testEndpointID, testAllowedEmail, 500},
		{"userinfo", strings.Replace(validURL, "https://", "https://user@", 1), testEndpointID, testAllowedEmail, 500},
		{"escaped authority", strings.Replace(validURL, "ep-test", "%65p-test", 1), testEndpointID, testAllowedEmail, 500},
		{"Unicode authority", strings.Replace(validURL, "west", "weſt", 1), testEndpointID, testAllowedEmail, 500},
		{"Unicode lowercasing authority", strings.Replace(validURL, testEndpointID, "ep-Kelvin", 1), "ep-kelvin", testAllowedEmail, 500},
		{"escaped Unicode authority", strings.Replace(validURL, testEndpointID, "ep-%E2%84%AAelvin", 1), "ep-kelvin", testAllowedEmail, 500},
		{"authority suffix", strings.Replace(validURL, ".tech/", ".tech.evil.invalid/", 1), testEndpointID, testAllowedEmail, 500},
		{"backslash authority", testIssuer + `\evil.invalid/neondb/auth`, testEndpointID, testAllowedEmail, 500},
		{"endpoint repeated hyphen", strings.Replace(validURL, testEndpointID, "ep--test", 1), "ep--test", testAllowedEmail, 500},
		{"endpoint trailing hyphen", strings.Replace(validURL, testEndpointID, "ep-test-", 1), "ep-test-", testAllowedEmail, 500},
		{"endpoint leading hyphen", strings.Replace(validURL, testEndpointID, "-ep-test", 1), "-ep-test", testAllowedEmail, 500},
		{"endpoint length", strings.Replace(validURL, testEndpointID, "ep-"+strings.Repeat("x", 61), 1), "ep-" + strings.Repeat("x", 61), testAllowedEmail, 500},
		{"uppercase endpoint policy", validURL, strings.ToUpper(testEndpointID), testAllowedEmail, 500},
		{"region repeated hyphen", strings.Replace(validURL, "us-west", "us--west", 1), testEndpointID, testAllowedEmail, 500},
		{"region leading hyphen", strings.Replace(validURL, "us-west", "-us-west", 1), testEndpointID, testAllowedEmail, 500},
		{"region trailing hyphen", strings.Replace(validURL, "west-2", "west-2-", 1), testEndpointID, testAllowedEmail, 500},
		{"region label length", strings.Replace(validURL, "us-west-2", strings.Repeat("a", 64), 1), testEndpointID, testAllowedEmail, 500},
		{"escaped database", testIssuer + "/%6eeondb/auth", testEndpointID, testAllowedEmail, 500},
		{"escaped path slash", testIssuer + "/neondb%2fauth", testEndpointID, testAllowedEmail, 500},
		{"dot path", testIssuer + "/../auth", testEndpointID, testAllowedEmail, 500},
		{"double path slash", testIssuer + "//neondb/auth", testEndpointID, testAllowedEmail, 500},
		{"uppercase auth path", testIssuer + "/neondb/Auth", testEndpointID, testAllowedEmail, 500},
		{"internal whitespace", strings.Replace(validURL, ".neonauth", " .neonauth", 1), testEndpointID, testAllowedEmail, 500},
		{"URL length", testIssuer + "/" + strings.Repeat("x", 256) + "/auth", testEndpointID, testAllowedEmail, 500},
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

func TestAuthPolicyCanonicalURLAcrossRuntimeEntryPoints(t *testing.T) {
	for _, baseURL := range []string{
		testIssuer + "/neondb/auth", "  " + testIssuer + "/neondb/auth/\n",
		strings.ToUpper(testIssuer) + "/neondb/auth///",
		testIssuer + "/custom_DB-2/auth/",
	} {
		t.Run(baseURL, func(t *testing.T) {
			canonical, _, err := normalizeAuthMembership(baseURL, " "+testEndpointID+" ", testAllowedEmail, nil)
			if err != nil || !strings.HasPrefix(canonical, testIssuer+"/") || strings.HasSuffix(canonical, "/") {
				t.Fatalf("canonical URL = %q, %v", canonical, err)
			}
			t.Setenv("NEON_AUTH_BASE_URL", baseURL)
			t.Setenv("NEON_AUTH_ENDPOINT_ID", testEndpointID)
			t.Setenv("AUTH_ALLOWED_EMAIL", testAllowedEmail)
			rec := httptest.NewRecorder()
			AuthConfig(rec, httptest.NewRequest("GET", "/api/auth-config", nil))
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"enabled":true`) {
				t.Fatalf("config: %d %s", rec.Code, rec.Body.String())
			}
			f := newBearerFixture(t)
			config := f.config()
			config.BaseURL = baseURL
			if _, err := VerifyBearer(t.Context(), "Bearer "+f.token(t, f.claims()), config); err != nil {
				t.Fatalf("canonical provider issuer rejected: %v", err)
			}
			proxyCalls := 0
			proxyConfig := AuthProxyConfig{BaseURL: baseURL, ExpectedEndpoint: testEndpointID, AllowedEmail: testAllowedEmail, Client: authProxyDoer(func(r *http.Request) (*http.Response, error) {
				proxyCalls++
				if r.URL.String() != canonical+"/get-session" {
					t.Errorf("proxy URL = %s", r.URL)
				}
				return f.client.Do(r)
			})}
			rec = httptest.NewRecorder()
			AuthProxy(proxyConfig).ServeHTTP(rec, httptest.NewRequest("GET", "https://meal.invalid/api/auth-proxy/get-session", nil))
			if proxyCalls != 1 || rec.Code != 200 {
				t.Fatalf("proxy calls/status = %d/%d", proxyCalls, rec.Code)
			}
			webhook := newWebhookFixture(t)
			webhookConfig := webhook.config()
			webhookConfig.BaseURL = baseURL
			rec = httptest.NewRecorder()
			AuthBeforeCreate(webhookConfig).ServeHTTP(rec, webhook.request(webhook.payload(testAllowedEmail)))
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"allowed":true`) {
				t.Fatalf("webhook: %d %s", rec.Code, rec.Body.String())
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
		if _, _, err := normalizeAuthMembership(testIssuer+"/neondb/auth", testEndpointID, email, nil); err != nil {
			t.Errorf("deployment-valid email rejected: %v", err)
		}
	}
}

func TestAuthEmailIdentityRejectsUnicodeCaseFoldAcrossEntryPoints(t *testing.T) {
	for _, tc := range []struct {
		allowed, email string
		accepted       bool
	}{
		{"kelvin@example.invalid", "Kelvin@example.invalid", false},
		{"sam@example.invalid", "ſam@example.invalid", false},
		{"sam@example.invalid", "sam@exampⅼe.invalid", false},
		{"kelvin@example.invalid", "  KELVIN@example.invalid  ", true},
		{"sam@example.invalid", "SAM@example.invalid", true},
	} {
		t.Run(tc.email, func(t *testing.T) {
			bearer := newBearerFixture(t)
			claims := bearer.claims()
			claims["email"] = tc.email
			config := bearer.config()
			config.AllowedEmail = tc.allowed
			_, err := VerifyBearer(t.Context(), "Bearer "+bearer.token(t, claims), config)
			if (err == nil) != tc.accepted {
				t.Errorf("bearer: %v", err)
			}
			webhook := newWebhookFixture(t)
			webhookConfig := webhook.config()
			webhookConfig.AllowedEmail = tc.allowed
			rec := httptest.NewRecorder()
			AuthBeforeCreate(webhookConfig).ServeHTTP(rec, webhook.request(webhook.payload(tc.email)))
			var response beforeCreateResponse
			if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &response) != nil || response.Allowed != tc.accepted {
				t.Errorf("webhook: %d %s", rec.Code, rec.Body.String())
			}
			for route := range authProxyEmailRoutes {
				calls := 0
				proxy := AuthProxyConfig{BaseURL: testIssuer + "/neondb/auth", ExpectedEndpoint: testEndpointID, AllowedEmail: tc.allowed, Client: authProxyDoer(func(r *http.Request) (*http.Response, error) { calls++; return bearer.client.Do(r) })}
				body, _ := json.Marshal(map[string]string{"email": tc.email})
				rec = httptest.NewRecorder()
				AuthProxy(proxy).ServeHTTP(rec, httptest.NewRequest("POST", "https://meal.invalid/api/auth-proxy/"+route, strings.NewReader(string(body))))
				if (calls == 1) != tc.accepted {
					t.Errorf("proxy %s: calls=%d", route, calls)
				}
			}
		})
	}
}

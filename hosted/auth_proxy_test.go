package hosted

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type authProxyDoer func(*http.Request) (*http.Response, error)

func (f authProxyDoer) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestAuthProxyForwardsAllowedRouteAndRehomesCookie(t *testing.T) {
	var upstream *http.Request
	var upstreamBody string
	config := AuthProxyConfig{
		BaseURL:          "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth",
		ExpectedEndpoint: testEndpointID,
		AllowedEmail:     "allowed@example.invalid",
		Client: authProxyDoer(func(request *http.Request) (*http.Response, error) {
			upstream = request
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			upstreamBody = string(body)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"user":{"id":"test"}}`)),
				Header: http.Header{
					"Content-Type": []string{"application/json"},
					"Set-Cookie": []string{
						"__Secure-neon-auth.session_token=opaque; Path=/example/auth; Domain=auth.invalid; HttpOnly; Secure; SameSite=None; Partitioned",
						"__Secure-neon-auth.session_data=opaque-data; Expires=Wed, 01 Jan 2027 00:00:00 GMT; Path=/; HttpOnly; Secure",
						"unrelated-provider-cookie=do-not-forward; Path=/",
					},
				},
			}, nil
		}),
	}
	requestBody := `{"email":"allowed@example.invalid","password":"redacted"}`
	req := httptest.NewRequest(http.MethodPost, "https://meal.invalid/api/auth-proxy?path=sign-in/email&returnTo=%2Fweek", strings.NewReader(requestBody))
	req.Host = "meal.invalid"
	req.Header.Set("Origin", "https://meal.invalid")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "_vercel_sso_nonce=private; app-cookie=private; __Secure-neon-auth.session_token=prior")
	rec := httptest.NewRecorder()

	AuthProxy(config).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	if upstream == nil || upstream.URL.String() != "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth/sign-in/email?returnTo=%2Fweek" {
		t.Fatalf("upstream URL = %v", upstream)
	}
	if upstreamBody != requestBody {
		t.Fatalf("upstream body = %q", upstreamBody)
	}
	if upstream.Header.Get("Origin") != "https://meal.invalid" || upstream.Header.Get("Cookie") == "" {
		t.Fatalf("required headers were not forwarded: %#v", upstream.Header)
	}
	if upstream.Header.Get("Cookie") != "__Secure-neon-auth.session_token=prior" {
		t.Fatalf("unexpected cookies forwarded: %q", upstream.Header.Get("Cookie"))
	}
	cookie := rec.Header().Get("Set-Cookie")
	for _, required := range []string{"Path=/", "HttpOnly", "Secure", "SameSite=Lax"} {
		if !strings.Contains(cookie, required) {
			t.Fatalf("cookie %q is missing %q", cookie, required)
		}
	}
	for _, forbidden := range []string{"Domain=", "Partitioned", "SameSite=None"} {
		if strings.Contains(cookie, forbidden) {
			t.Fatalf("cookie %q contains %q", cookie, forbidden)
		}
	}
	if len(rec.Header().Values("Set-Cookie")) != 2 {
		t.Fatalf("set-cookie headers = %#v", rec.Header().Values("Set-Cookie"))
	}
	if !strings.Contains(rec.Header().Values("Set-Cookie")[1], "Expires=Wed, 01 Jan 2027 00:00:00 GMT") {
		t.Fatalf("expiry was not preserved: %#v", rec.Header().Values("Set-Cookie"))
	}
}

func TestAuthProxyReturnsGenericProviderErrorsForEmailOutsidePolicy(t *testing.T) {
	providerCalled := false
	config := AuthProxyConfig{
		BaseURL:          "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth",
		ExpectedEndpoint: testEndpointID,
		AllowedEmail:     "allowed@example.invalid",
		Client: authProxyDoer(func(*http.Request) (*http.Response, error) {
			providerCalled = true
			return nil, nil
		}),
	}
	tests := map[string]struct {
		status int
		body   string
	}{
		"email-otp/reset-password": {http.StatusBadRequest, `{"code":"INVALID_OTP","message":"Invalid OTP"}`},
		"sign-in/email":            {http.StatusUnauthorized, `{"code":"INVALID_EMAIL_OR_PASSWORD","message":"Invalid email or password"}`},
	}
	for path, expected := range tests {
		t.Run(path, func(t *testing.T) {
			providerCalled = false
			req := httptest.NewRequest(http.MethodPost, "https://meal.invalid/api/auth-proxy?path="+path, strings.NewReader(`{"email":"blocked@example.invalid"}`))
			req.Host = "meal.invalid"
			req.Header.Set("Origin", "https://meal.invalid")
			rec := httptest.NewRecorder()
			AuthProxy(config).ServeHTTP(rec, req)
			if rec.Code != expected.status || rec.Body.String() != expected.body {
				t.Fatalf("status/body = %d %q", rec.Code, rec.Body.String())
			}
			if providerCalled {
				t.Fatal("provider was called for a blocked identity")
			}
		})
	}
}

func TestAuthProxySilentlyDropsBlockedEmailDelivery(t *testing.T) {
	providerCalled := false
	config := AuthProxyConfig{
		BaseURL:          "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth",
		ExpectedEndpoint: testEndpointID,
		AllowedEmail:     "allowed@example.invalid",
		Client: authProxyDoer(func(*http.Request) (*http.Response, error) {
			providerCalled = true
			return nil, nil
		}),
	}
	for _, path := range []string{"email-otp/request-password-reset"} {
		t.Run(path, func(t *testing.T) {
			providerCalled = false
			req := httptest.NewRequest(http.MethodPost, "https://meal.invalid/api/auth-proxy?path="+path, strings.NewReader(`{"email":"blocked@example.invalid"}`))
			req.Host = "meal.invalid"
			req.Header.Set("Origin", "https://meal.invalid")
			rec := httptest.NewRecorder()
			AuthProxy(config).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || rec.Body.String() != `{"success":true}` {
				t.Fatalf("status/body = %d %q", rec.Code, rec.Body.String())
			}
			if providerCalled {
				t.Fatal("provider was called for a blocked identity")
			}
		})
	}
}

func TestAuthProxyEmailRoutesFailClosedWithoutAllowedIdentity(t *testing.T) {
	config := AuthProxyConfig{
		BaseURL:          "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth",
		ExpectedEndpoint: testEndpointID,
	}
	req := httptest.NewRequest(http.MethodPost, "https://meal.invalid/api/auth-proxy?path=sign-in/email", strings.NewReader(`{"email":"allowed@example.invalid","password":"private"}`))
	req.Host = "meal.invalid"
	req.Header.Set("Origin", "https://meal.invalid")
	rec := httptest.NewRecorder()
	AuthProxy(config).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestAuthProxyRejectsUnknownRoutes(t *testing.T) {
	rec := httptest.NewRecorder()
	AuthProxy(AuthProxyConfig{BaseURL: "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth", ExpectedEndpoint: testEndpointID, AllowedEmail: testAllowedEmail}).ServeHTTP(
		rec,
		httptest.NewRequest(http.MethodPost, "/api/auth-proxy?path=admin/create-user", nil),
	)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAuthProxyRejectsLegacyPasswordlessRoutes(t *testing.T) {
	for _, path := range []string{"email-otp/send-verification-otp", "sign-in/email-otp"} {
		t.Run(path, func(t *testing.T) {
			providerCalled := false
			rec := httptest.NewRecorder()
			AuthProxy(AuthProxyConfig{
				BaseURL:          "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth",
				ExpectedEndpoint: testEndpointID,
				AllowedEmail:     "allowed@example.invalid",
				Client: authProxyDoer(func(*http.Request) (*http.Response, error) {
					providerCalled = true
					return nil, nil
				}),
			}).ServeHTTP(
				rec,
				httptest.NewRequest(http.MethodPost, "https://meal.invalid/api/auth-proxy?path="+path, strings.NewReader(`{"email":"allowed@example.invalid"}`)),
			)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", rec.Code)
			}
			if providerCalled {
				t.Fatal("provider was called for a legacy passwordless route")
			}
		})
	}
}

func TestAuthProxyRejectsCrossOriginRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://meal.invalid/api/auth-proxy?path=sign-in/email", strings.NewReader(`{"email":"allowed@example.invalid","password":"private"}`))
	req.Host = "meal.invalid"
	req.Header.Set("Origin", "https://evil.invalid")
	rec := httptest.NewRecorder()
	AuthProxy(AuthProxyConfig{BaseURL: "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth", ExpectedEndpoint: testEndpointID, AllowedEmail: testAllowedEmail}).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestAuthProxySynthesizesOriginForOriginlessSessionGet(t *testing.T) {
	var upstreamOrigin string
	config := AuthProxyConfig{
		BaseURL:          "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth",
		ExpectedEndpoint: testEndpointID,
		AllowedEmail:     testAllowedEmail,
		Client: authProxyDoer(func(request *http.Request) (*http.Response, error) {
			upstreamOrigin = request.Header.Get("Origin")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`null`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}),
	}
	req := httptest.NewRequest(http.MethodGet, "https://meal.invalid/api/auth-proxy?path=get-session", nil)
	req.Host = "meal.invalid"
	rec := httptest.NewRecorder()
	AuthProxy(config).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	if upstreamOrigin != "https://meal.invalid" {
		t.Fatalf("origin = %q", upstreamOrigin)
	}
}

func TestAuthProxyDoesNotEmitSessionHeadersWhenCookieValidationFails(t *testing.T) {
	config := AuthProxyConfig{
		BaseURL:          "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth",
		ExpectedEndpoint: testEndpointID,
		AllowedEmail:     testAllowedEmail,
		Client: authProxyDoer(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Header: http.Header{
					"Content-Type":   []string{"application/json"},
					"Set-Auth-Jwt":   []string{"private-jwt"},
					"Set-Auth-Token": []string{"private-token"},
					"Set-Cookie":     []string{"__Secure-neon-auth.invalid-cookie"},
				},
			}, nil
		}),
	}
	req := httptest.NewRequest(http.MethodGet, "https://meal.invalid/api/auth-proxy?path=get-session", nil)
	req.Host = "meal.invalid"
	rec := httptest.NewRecorder()
	AuthProxy(config).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	for _, header := range []string{"Set-Auth-Jwt", "Set-Auth-Token", "Set-Cookie"} {
		if value := rec.Header().Get(header); value != "" {
			t.Fatalf("%s leaked: %q", header, value)
		}
	}
}

func TestAuthProxyFailsClosedWithoutProviderConfig(t *testing.T) {
	rec := httptest.NewRecorder()
	AuthProxy(AuthProxyConfig{}).ServeHTTP(
		rec,
		httptest.NewRequest(http.MethodGet, "/api/auth-proxy?path=get-session", nil),
	)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestAuthProxyRejectsProviderEndpointMismatch(t *testing.T) {
	rec := httptest.NewRecorder()
	AuthProxy(AuthProxyConfig{
		BaseURL:          "https://ep-other.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth",
		ExpectedEndpoint: testEndpointID,
	}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth-proxy?path=get-session", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestRewriteAuthProxyCookieAddsSafeDefaults(t *testing.T) {
	got, err := rewriteAuthProxyCookie("__Secure-neon-auth.session_token=opaque")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"Path=/", "SameSite=Lax", "Secure", "HttpOnly"} {
		if !strings.Contains(got, required) {
			t.Fatalf("cookie %q is missing %q", got, required)
		}
	}
	if strings.Contains(got, "Domain=") {
		t.Fatalf("cookie = %q", got)
	}
}

func TestAuthProxyPreservesRawQueryAndFiltersPath(t *testing.T) {
	got := authProxyUpstreamQuery("path=get-session&redirect=https%3A%2F%2Fmeal.invalid%2Fa%2Bb&flag")
	if got != "redirect=https%3A%2F%2Fmeal.invalid%2Fa%2Bb&flag" {
		t.Fatalf("query = %q", got)
	}
}

// All identities and provider responses in these regression cases are synthetic.
func TestAuthProxyRejectsAmbiguousEmailPayloads(t *testing.T) {
	payloads := []string{
		`{"email":"blocked@example.invalid","Email":"allowed@example.invalid"}`,
		`{"Email":"allowed@example.invalid","email":"blocked@example.invalid"}`,
		`{"email":"allowed@example.invalid","EMAIL":"allowed@example.invalid"}`,
		`{"Email":"allowed@example.invalid"}`,
		`{"email":"blocked@example.invalid","email":"allowed@example.invalid"}`,
		`{"email":"allowed@example.invalid","email":"blocked@example.invalid"}`,
		`{"email":"allowed@example.invalid","email":"allowed@example.invalid"}`,
		`{"email":"blocked@example.invalid","\u0065mail":"allowed@example.invalid"}`,
		`{"email":"blocked@example.invalid"}`,
		`{"email":null}`, `{"email":42}`, `{"email":[]}`, `{"email":{}}`,
		`{}`, `null`, `[]`, `"allowed@example.invalid"`,
		`{"nested":{"email":"allowed@example.invalid"}}`,
		`{"email":"allowed@example.invalid"} {}`,
		`{"email":"allowed@example.invalid",}`,
	}
	for path := range authProxyEmailRoutes {
		for _, body := range payloads {
			t.Run(path+"/"+body, func(t *testing.T) {
				calls := 0
				config := AuthProxyConfig{
					BaseURL:          "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth",
					ExpectedEndpoint: testEndpointID,
					AllowedEmail:     "allowed@example.invalid",
					Client: authProxyDoer(func(*http.Request) (*http.Response, error) {
						calls++
						return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
					}),
				}
				req := httptest.NewRequest(http.MethodPost, "https://meal.invalid/api/auth-proxy?path="+path, strings.NewReader(body))
				rec := httptest.NewRecorder()
				AuthProxy(config).ServeHTTP(rec, req)
				if calls != 0 {
					t.Fatalf("provider called %d times for rejected payload", calls)
				}
				expected := authProxyBlockedEmailResponses[path]
				if authProxySilentEmailRoutes[path] {
					expected = authProxyBlockedResponse{http.StatusOK, `{"success":true}`}
				}
				if rec.Code != expected.Status || rec.Body.String() != expected.Body {
					t.Fatalf("non-generic rejection: %d %s", rec.Code, rec.Body.String())
				}
			})
		}
	}
}

func TestAuthProxyForwardsCanonicalEmailAndSDKFields(t *testing.T) {
	for path := range authProxyEmailRoutes {
		for _, emailKey := range []string{`email`, `\u0065mail`} {
			t.Run(path+"/"+emailKey, func(t *testing.T) {
				body := `{"` + emailKey + `":"  ALLOWED@example.invalid  ","password":"synthetic-password","otp":"123456","rememberMe":false,"callbackURL":"/week","metadata":{"email":"nested@example.invalid","number":9007199254740993}}`
				calls := 0
				config := AuthProxyConfig{
					BaseURL:          "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth",
					ExpectedEndpoint: testEndpointID,
					AllowedEmail:     "allowed@example.invalid",
					Client: authProxyDoer(func(request *http.Request) (*http.Response, error) {
						calls++
						forwarded, err := io.ReadAll(request.Body)
						if err != nil {
							t.Fatal(err)
						}
						var got, want map[string]json.RawMessage
						if err := json.Unmarshal(forwarded, &got); err != nil {
							t.Fatal(err)
						}
						if err := json.Unmarshal([]byte(body), &want); err != nil {
							t.Fatal(err)
						}
						want["email"] = json.RawMessage(`"allowed@example.invalid"`)
						if !reflect.DeepEqual(got, want) {
							t.Fatalf("forwarded payload = %s", forwarded)
						}
						return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"success":true}`))}, nil
					}),
				}
				req := httptest.NewRequest(http.MethodPost, "https://meal.invalid/api/auth-proxy?path="+path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				AuthProxy(config).ServeHTTP(rec, req)
				if calls != 1 || rec.Code != http.StatusOK {
					t.Fatalf("calls/status = %d/%d", calls, rec.Code)
				}
			})
		}
	}
}

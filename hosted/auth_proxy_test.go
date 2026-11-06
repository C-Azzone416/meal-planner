package hosted

import (
	"io"
	"net/http"
	"net/http/httptest"
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
		BaseURL: "https://auth.invalid/example/auth",
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
	requestBody := `{"email":"allowed@example.invalid","otp":"redacted"}`
	req := httptest.NewRequest(http.MethodPost, "https://meal.invalid/api/auth-proxy?path=sign-in/email-otp&returnTo=%2Fweek", strings.NewReader(requestBody))
	req.Host = "meal.invalid"
	req.Header.Set("Origin", "https://meal.invalid")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "_vercel_sso_nonce=private; app-cookie=private; __Secure-neon-auth.session_token=prior")
	rec := httptest.NewRecorder()

	AuthProxy(config).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	if upstream == nil || upstream.URL.String() != "https://auth.invalid/example/auth/sign-in/email-otp?returnTo=%2Fweek" {
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

func TestAuthProxyRejectsUnknownRoutes(t *testing.T) {
	rec := httptest.NewRecorder()
	AuthProxy(AuthProxyConfig{BaseURL: "https://auth.invalid/example/auth"}).ServeHTTP(
		rec,
		httptest.NewRequest(http.MethodPost, "/api/auth-proxy?path=admin/create-user", nil),
	)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAuthProxyRejectsCrossOriginRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://meal.invalid/api/auth-proxy?path=sign-in/email-otp", strings.NewReader(`{}`))
	req.Host = "meal.invalid"
	req.Header.Set("Origin", "https://evil.invalid")
	rec := httptest.NewRecorder()
	AuthProxy(AuthProxyConfig{BaseURL: "https://auth.invalid/example/auth"}).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
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

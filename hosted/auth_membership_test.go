package hosted

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func membershipJSON(emails ...string) *string {
	raw, _ := json.Marshal(emails)
	value := string(raw)
	return &value
}

func invalidMembershipInputs() map[string]string {
	return map[string]string{
		"empty": "", "blank": " \t\r\n", "empty array": "[]", "null": "null",
		"object": `{}`, "string": `"allowed@example.invalid"`, "CSV": `allowed@example.invalid,other@example.invalid`,
		"null member": `["allowed@example.invalid",null]`, "number": `[42]`, "boolean": `[true]`,
		"object member": `[{}]`, "nested": `[["allowed@example.invalid"]]`, "empty member": `[""]`,
		"duplicate":         `["allowed@example.invalid","allowed@example.invalid"]`,
		"escaped duplicate": `["allowed@example.invalid","\u0061llowed@example.invalid"]`,
		"uppercase":         `["Allowed@example.invalid"]`, "padding": `[" allowed@example.invalid "]`,
		"internal whitespace": `["allowed@ example.invalid"]`, "unicode padding": `["\u00a0allowed@example.invalid"]`,
		"unicode alias": `["\u212aelvin@example.invalid"]`, "surrogate": `["\ud800@example.invalid"]`,
		"malformed UTF8": "[\"\xff@example.invalid\"]", "trailing JSON": `["allowed@example.invalid"][]`,
		"trailing comma": `["allowed@example.invalid",]`, "display name": `["User <allowed@example.invalid>"]`,
		"domain": `["@example.invalid"]`, "wildcard": `["*@example.invalid"]`, "single label": `["allowed@example"]`,
		"comment": `["allowed@example.invalid (User)"]`, "dot local": `["allowed..user@example.invalid"]`,
		"local too long":     `["` + strings.Repeat("a", 65) + `@example.invalid"]`,
		"address too long":   `["a@` + strings.Repeat("b", 245) + `.invalid"]`,
		"too many":           *membershipJSON(syntheticMembers(maxAuthMembers + 1)...),
		"too large":          strings.Repeat(" ", maxAuthMembershipBytes) + `["allowed@example.invalid"]`,
		"secret placeholder": `[SENSITIVE]`,
	}
}

func syntheticMembers(count int) []string {
	emails := make([]string, count)
	for i := range emails {
		emails[i] = fmt.Sprintf("member-%d@example.invalid", i)
	}
	return emails
}

func TestAuthMembershipSupportedInputs(t *testing.T) {
	for name, raw := range map[string]*string{
		"legacy":                      nil,
		"two members":                 membershipJSON(testAllowedEmail, testOtherEmail),
		"JSON whitespace and escapes": new(" \n[\"\\u0061llowed@example.invalid\",\"other@example.invalid\"]\t"),
		"member limit":                membershipJSON(syntheticMembers(maxAuthMembers)...),
		"byte limit":                  new(strings.Repeat(" ", maxAuthMembershipBytes-len(*membershipJSON(testAllowedEmail))) + *membershipJSON(testAllowedEmail)),
	} {
		t.Run(name, func(t *testing.T) {
			legacy := ""
			if raw == nil {
				legacy = testAllowedEmail
			}
			members, err := parseAuthMembership(legacy, raw)
			if err != nil || len(members) == 0 {
				t.Fatalf("membership failed: %v", err)
			}
			if name == "member limit" {
				return
			}
			for _, email := range []string{testAllowedEmail, " \t\r\nALLOWED@Example.Invalid\n"} {
				if canonical, ok := members.email(email); !ok || canonical != testAllowedEmail {
					t.Fatal("canonical member denied")
				}
			}
			for _, email := range []string{"unlisted@example.invalid", "\u00a0" + testAllowedEmail, "aſlowed@example.invalid", "allowed@ example.invalid", testAllowedEmail + "\v"} {
				if _, ok := members.email(email); ok {
					t.Fatal("invalid identity accepted")
				}
			}
		})
	}
}

func TestAuthMembershipInvalidConfiguration(t *testing.T) {
	for name, raw := range invalidMembershipInputs() {
		t.Run(name, func(t *testing.T) {
			for _, legacy := range []string{"", testAllowedEmail} {
				if _, err := parseAuthMembership(legacy, &raw); !errors.Is(err, ErrAuthConfiguration) {
					t.Fatal("invalid membership accepted")
				}
			}
		})
	}
	if _, err := parseAuthMembership(testAllowedEmail, membershipJSON(testAllowedEmail, testOtherEmail)); !errors.Is(err, ErrAuthConfiguration) {
		t.Fatal("conflicting formats accepted")
	}
}

func configureMembershipEnvironment(t *testing.T, raw string) {
	t.Helper()
	t.Setenv("NEON_AUTH_BASE_URL", testIssuer+"/neondb/auth")
	t.Setenv("NEON_AUTH_ENDPOINT_ID", testEndpointID)
	t.Setenv("AUTH_ALLOWED_EMAIL", "")
	t.Setenv("AUTH_ALLOWED_EMAILS", raw)
}

func TestAuthMembershipConfigurationFailsClosedEverywhere(t *testing.T) {
	inputs := invalidMembershipInputs()
	inputs["conflicting formats"] = *membershipJSON(testAllowedEmail, testOtherEmail)
	for name, raw := range inputs {
		t.Run(name, func(t *testing.T) {
			configureMembershipEnvironment(t, raw)
			if name == "conflicting formats" {
				t.Setenv("AUTH_ALLOWED_EMAIL", testAllowedEmail)
			}
			client := httpDoerFunc(func(*http.Request) (*http.Response, error) {
				t.Error("invalid membership reached provider")
				return nil, errors.New("unexpected provider call")
			})
			rec := httptest.NewRecorder()
			AuthConfig(rec, httptest.NewRequest("GET", "/api/auth-config", nil))
			if rec.Code != 500 || strings.Contains(rec.Body.String(), `"enabled":true`) {
				t.Fatal("invalid policy enabled readiness")
			}
			if strings.Contains(rec.Body.String(), testAllowedEmail) {
				t.Fatal("private membership exposed")
			}
			config := BearerAuthConfigFromEnvironment()
			config.Client = client
			for route, methods := range authProxyRoutes {
				for method := range methods {
					rec = httptest.NewRecorder()
					AuthProxy(AuthProxyConfig{BaseURL: config.BaseURL, ExpectedEndpoint: config.ExpectedEndpoint, AllowedEmail: config.AllowedEmail, AllowedEmailsJSON: config.AllowedEmailsJSON, Client: client}).ServeHTTP(rec, httptest.NewRequest(method, "https://meal.invalid/api/auth-proxy/"+route, nil))
					if rec.Code != 503 {
						t.Fatalf("proxy %s enabled invalid membership", route)
					}
				}
			}
			rec = httptest.NewRecorder()
			AuthBeforeCreateFromEnvironment(rec, httptest.NewRequest("POST", "/api/auth/before-create", nil))
			if rec.Code != 503 {
				t.Fatal("webhook enabled invalid membership")
			}
			store := &recipeFake{}
			for _, handler := range []http.Handler{AuthorizeWithConfig(config), RequireUserWithConfig(config, RecipesHandler(store))} {
				rec = httptest.NewRecorder()
				handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/recipes", nil))
				if rec.Code != 503 || store.calls != 0 {
					t.Fatal("protected handler enabled invalid membership")
				}
			}
		})
	}
}

func TestAuthMultipleMembersAcrossAuthorizationPoints(t *testing.T) {
	raw := membershipJSON(testAllowedEmail, testOtherEmail)
	configureMembershipEnvironment(t, *raw)
	rec := httptest.NewRecorder()
	AuthConfig(rec, httptest.NewRequest("GET", "/api/auth-config", nil))
	if rec.Code != 200 || rec.Body.String() != `{"enabled":true,"baseUrl":"/api/auth-proxy"}`+"\n" {
		t.Fatal("unexpected public readiness response")
	}
	bearer := newBearerFixture(t)
	webhook := newWebhookFixture(t)
	for i, email := range []string{testAllowedEmail, testOtherEmail, "unlisted@example.invalid"} {
		t.Run(fmt.Sprintf("identity-%d", i), func(t *testing.T) {
			allowed := i < 2
			for route := range authProxyEmailRoutes {
				calls := 0
				client := httpDoerFunc(func(req *http.Request) (*http.Response, error) {
					calls++
					var payload map[string]any
					if json.NewDecoder(req.Body).Decode(&payload) != nil || payload["email"] != email {
						t.Error("proxy did not forward checked canonical identity")
					}
					// Synthetic provider: these two accounts already exist. No create route.
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"success":true}`))}, nil
				})
				proxy := AuthProxyConfig{BaseURL: bearer.config().BaseURL, ExpectedEndpoint: testEndpointID, AllowedEmailsJSON: raw, Client: client}
				body, _ := json.Marshal(map[string]string{"email": " \t" + strings.ToUpper(email) + "\r\n", "password": "synthetic-test-only"})
				rec := httptest.NewRecorder()
				AuthProxy(proxy).ServeHTTP(rec, httptest.NewRequest("POST", "https://meal.invalid/api/auth-proxy/"+route, strings.NewReader(string(body))))
				if (calls == 1) != allowed {
					t.Fatalf("proxy %s calls=%d", route, calls)
				}
			}
			wc := webhook.config()
			wc.AllowedEmail = ""
			wc.AllowedEmailsJSON = raw
			rec := httptest.NewRecorder()
			AuthBeforeCreate(wc).ServeHTTP(rec, webhook.request(webhook.payload(email)))
			var response beforeCreateResponse
			if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &response) != nil || response.Allowed != allowed {
				t.Fatal("webhook membership mismatch")
			}
			claims := bearer.claims()
			claims["email"] = email
			claims["sub"] = fmt.Sprintf("synthetic-user-%d", i)
			token := "Bearer " + bearer.token(t, claims)
			config := bearer.config()
			config.AllowedEmail = ""
			config.AllowedEmailsJSON = raw
			rec = httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/api/auth/authorize", nil)
			req.Header.Set("Authorization", token)
			AuthorizeWithConfig(config).ServeHTTP(rec, req)
			want := 401
			if allowed {
				want = 204
			}
			if rec.Code != want {
				t.Fatalf("session authorization=%d", rec.Code)
			}
			assertMembershipRecipeRequests(t, func(next http.Handler) http.Handler { return RequireUserWithConfig(config, next) }, token, allowed, RecipeOwner{Issuer: testIssuer, Subject: claims["sub"].(string)})
		})
	}
}

// Exercise every manual-recipe operation through the real bearer wrapper,
// including direct requests with no browser session or prior authorize call.
func assertMembershipRecipeRequests(t *testing.T, wrap func(http.Handler) http.Handler, token string, allowed bool, owner RecipeOwner) {
	t.Helper()
	body := recipeBody(t, syntheticRecipeContent())
	for _, operation := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/api/recipes", "", 200},
		{"GET", "/api/recipes?id=" + recipeTestID, "", 200},
		{"POST", "/api/recipes", body, 201},
		{"PUT", "/api/recipes?id=" + recipeTestID, strings.Replace(body, `{"content":`, `{"expectedRevision":1,"content":`, 1), 200},
	} {
		store := &recipeFake{}
		req := httptest.NewRequest(operation.method, operation.path, strings.NewReader(operation.body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", recipeTestKey)
		rec := httptest.NewRecorder()
		wrap(RecipesHandler(store)).ServeHTTP(rec, req)
		want := 401
		if allowed {
			want = operation.status
		}
		if rec.Code != want {
			t.Fatalf("recipe %s: status=%d want=%d", operation.method, rec.Code, want)
		}
		if allowed && (store.calls != 1 || store.owner != owner) || !allowed && store.calls != 0 {
			t.Fatal("recipe storage membership/owner mismatch")
		}
	}
}

func TestAuthMembershipEnvironmentIsolation(t *testing.T) {
	fixture := newBearerFixture(t)
	shared := "shared@example.invalid"
	productionIssuer := strings.Replace(testIssuer, testEndpointID, "ep-production-synthetic", 1)
	preview := fixture.config()
	preview.AllowedEmail = ""
	preview.AllowedEmailsJSON = membershipJSON(testAllowedEmail, shared)
	production := preview
	production.BaseURL = productionIssuer + "/neondb/auth"
	production.ExpectedEndpoint = "ep-production-synthetic"
	production.AllowedEmailsJSON = membershipJSON(testOtherEmail, shared)
	// Deliberately reuse the signing key and subject, so the issuer/audience and
	// membership boundaries, rather than an incidental key mismatch, decide.
	for _, source := range []struct {
		name, issuer string
		config       BearerAuthConfig
	}{
		{"preview", testIssuer, preview}, {"production", productionIssuer, production},
	} {
		for _, email := range []string{testAllowedEmail, testOtherEmail, shared} {
			claims := fixture.claims()
			claims["iss"] = source.issuer
			claims["aud"] = source.issuer
			claims["email"] = email
			token := "Bearer " + fixture.token(t, claims)
			for _, target := range []struct {
				name   string
				config BearerAuthConfig
			}{{"preview", preview}, {"production", production}} {
				t.Run(source.name+"/"+target.name+"/"+email, func(t *testing.T) {
					listed := email == shared || target.name == "preview" && email == testAllowedEmail || target.name == "production" && email == testOtherEmail
					allowed := source.name == target.name && listed
					req := httptest.NewRequest("GET", "/api/auth/authorize", nil)
					req.Header.Set("Authorization", token)
					rec := httptest.NewRecorder()
					AuthorizeWithConfig(target.config).ServeHTTP(rec, req)
					want := 401
					if allowed {
						want = 204
					}
					if rec.Code != want {
						t.Fatalf("authorization=%d want=%d", rec.Code, want)
					}
					assertMembershipRecipeRequests(t, func(next http.Handler) http.Handler { return RequireUserWithConfig(target.config, next) }, token, allowed, RecipeOwner{Issuer: source.issuer, Subject: "user-test"})
				})
			}
		}
	}
}

func TestAuthMembershipRemovalDeniesPreviouslyIssuedTokensOnNextRequest(t *testing.T) {
	fixture := newBearerFixture(t)
	fixture.now = time.Now().UTC()
	configureMembershipEnvironment(t, *membershipJSON(testAllowedEmail, testOtherEmail))
	previous := http.DefaultTransport
	http.DefaultTransport = jwksRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != testIssuer+"/neondb/auth/.well-known/jwks.json" {
			t.Error("unexpected provider request")
		}
		return fixture.client.Do(req)
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	// Create the wrappers once, before membership changes; they must not retain
	// either the old set or a previous token authorization result.
	wrap := RequireUser
	authorized := http.HandlerFunc(Authorize)
	tokens := make([]string, 2)
	for i, email := range []string{testAllowedEmail, testOtherEmail} {
		claims := fixture.claims()
		claims["email"] = email
		claims["sub"] = fmt.Sprintf("synthetic-user-%d", i)
		tokens[i] = "Bearer " + fixture.token(t, claims)
	}
	protectedCalls := 0
	protected := RequireUser(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { protectedCalls++; w.WriteHeader(204) }))
	check := func(removed bool) {
		t.Helper()
		for i, token := range tokens {
			allowed := !removed || i == 1
			for _, handler := range []http.Handler{authorized, protected} {
				req := httptest.NewRequest("GET", "/api/auth/authorize", nil)
				req.Header.Set("Authorization", token)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				want := 401
				if allowed {
					want = 204
				}
				if rec.Code != want {
					t.Fatalf("removed=%v identity=%d status=%d", removed, i, rec.Code)
				}
			}
			assertMembershipRecipeRequests(t, wrap, token, allowed, RecipeOwner{Issuer: testIssuer, Subject: fmt.Sprintf("synthetic-user-%d", i)})
		}
	}
	check(false)
	t.Setenv("AUTH_ALLOWED_EMAILS", *membershipJSON(testOtherEmail))
	// No clock advance, token reissue, process restart or cache flush: the same
	// still-unexpired tokens are checked on the very next protected requests.
	check(true)
	if protectedCalls != 3 {
		t.Fatal("removed token reached protected handler")
	}
}

func TestAuthMembershipLegacyEnvironmentMigrationAndRollback(t *testing.T) {
	configureMembershipEnvironment(t, *membershipJSON(testAllowedEmail))
	// Absence, rather than an explicitly empty new setting, selects legacy.
	if err := os.Unsetenv("AUTH_ALLOWED_EMAILS"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTH_ALLOWED_EMAIL", testAllowedEmail)
	check := func(want int) {
		t.Helper()
		rec := httptest.NewRecorder()
		AuthConfig(rec, httptest.NewRequest("GET", "/api/auth-config", nil))
		if rec.Code != want {
			t.Fatalf("readiness=%d want=%d", rec.Code, want)
		}
		config := BearerAuthConfigFromEnvironment()
		members, err := parseAuthMembership(config.AllowedEmail, config.AllowedEmailsJSON)
		if want == 200 {
			if _, ok := members.email(testAllowedEmail); err != nil || !ok {
				t.Fatal("existing member lost during migration")
			}
		} else if err == nil {
			t.Fatal("conflicting membership accepted")
		}
	}
	check(200)
	t.Setenv("AUTH_ALLOWED_EMAILS", *membershipJSON(testAllowedEmail, testOtherEmail))
	check(500)
	t.Setenv("AUTH_ALLOWED_EMAIL", "")
	check(200)
	t.Setenv("AUTH_ALLOWED_EMAILS", "")
	check(500)
	if err := os.Unsetenv("AUTH_ALLOWED_EMAILS"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTH_ALLOWED_EMAIL", testAllowedEmail)
	check(200)
}

func TestAuthMembershipDoesNotProvisionAccountsOrBroadenSignup(t *testing.T) {
	raw := membershipJSON(testAllowedEmail, "not-provisioned@example.invalid")
	for _, email := range []string{testAllowedEmail, "not-provisioned@example.invalid"} {
		for route := range authProxyEmailRoutes {
			client := httpDoerFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path != "/neondb/auth/"+route {
					t.Error("unexpected provider operation")
				}
				// Only the first account was provisioned by an administrator. Membership
				// cannot make the second account's sign-in or password setup succeed.
				// Requesting recovery remains generic even when no account exists.
				status := 200
				if email != testAllowedEmail {
					switch route {
					case "sign-in/email":
						status = 401
					case "email-otp/reset-password":
						status = 400
					}
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			})
			handler := AuthProxy(AuthProxyConfig{BaseURL: testIssuer + "/neondb/auth", ExpectedEndpoint: testEndpointID, AllowedEmailsJSON: raw, Client: client})
			req := httptest.NewRequest("POST", "https://meal.invalid/api/auth-proxy/"+route, strings.NewReader(`{"email":"`+email+`"}`))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			want := 200
			if email != testAllowedEmail {
				switch route {
				case "sign-in/email":
					want = 401
				case "email-otp/reset-password":
					want = 400
				}
			}
			if rec.Code != want {
				t.Fatal("provider eligibility response changed")
			}
		}
	}
	client := httpDoerFunc(func(*http.Request) (*http.Response, error) {
		t.Error("signup/provisioning reached provider")
		return nil, errors.New("unexpected provider call")
	})
	for _, route := range []string{"sign-up/email", "admin/create-user", "email-otp/send-verification-otp", "sign-in/email-otp"} {
		rec := httptest.NewRecorder()
		AuthProxy(AuthProxyConfig{BaseURL: testIssuer + "/neondb/auth", ExpectedEndpoint: testEndpointID, AllowedEmailsJSON: raw, Client: client}).ServeHTTP(rec, httptest.NewRequest("POST", "https://meal.invalid/api/auth-proxy/"+route, strings.NewReader(`{"email":"allowed@example.invalid"}`)))
		if rec.Code != 404 {
			t.Fatalf("unavailable route enabled: %s", route)
		}
	}
}

func TestAuthMembershipEnvironmentProxyAndWebhook(t *testing.T) {
	configureMembershipEnvironment(t, *membershipJSON(testAllowedEmail, testOtherEmail))
	webhook := newWebhookFixture(t)
	webhook.now = time.Now().UTC()
	previous := http.DefaultTransport
	http.DefaultTransport = jwksRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == testIssuer+"/neondb/auth/.well-known/jwks.json" {
			return webhook.client.Do(req)
		}
		if req.URL.String() != testIssuer+"/neondb/auth/sign-in/email" {
			t.Error("unexpected provider request")
		}
		var payload map[string]string
		if json.NewDecoder(req.Body).Decode(&payload) != nil || payload["email"] != testAllowedEmail && payload["email"] != testOtherEmail {
			t.Error("unlisted or malformed sign-in reached provider")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	for i, email := range []string{testAllowedEmail, testOtherEmail, "unlisted@example.invalid"} {
		rec := httptest.NewRecorder()
		AuthProxyFromEnvironment(rec, httptest.NewRequest("POST", "https://meal.invalid/api/auth-proxy/sign-in/email", strings.NewReader(`{"email":"`+email+`"}`)))
		want := 200
		if i == 2 {
			want = 401
		}
		if rec.Code != want {
			t.Fatalf("environment sign-in=%d want=%d", rec.Code, want)
		}
		rec = httptest.NewRecorder()
		AuthBeforeCreateFromEnvironment(rec, webhook.request(webhook.payload(email)))
		var response beforeCreateResponse
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &response) != nil || response.Allowed != (i < 2) {
			t.Fatal("environment webhook membership mismatch")
		}
	}
}

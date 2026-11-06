package hosted

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	testIssuer   = "https://auth.invalid"
	testAudience = "https://auth.invalid"
)

type bearerFixture struct {
	privateKey ed25519.PrivateKey
	client     HTTPDoer
	now        time.Time
}

func newBearerFixture(t *testing.T) *bearerFixture {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	jwksBody, err := json.Marshal(jwksDocument{Keys: []jwk{{
		KeyType: "OKP",
		Curve:   "Ed25519",
		KeyID:   testKeyID,
		X:       base64.RawURLEncoding.EncodeToString(publicKey),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return &bearerFixture{
		privateKey: privateKey,
		now:        time.Unix(1_800_000_000, 0).UTC(),
		client: httpDoerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       ioNopCloser{Reader: bytes.NewReader(jwksBody)},
				Header:     make(http.Header),
			}, nil
		}),
	}
}

type ioNopCloser struct{ *bytes.Reader }

func (ioNopCloser) Close() error { return nil }

func (f *bearerFixture) config() BearerAuthConfig {
	return BearerAuthConfig{
		BaseURL:      "https://auth.invalid/example/auth",
		JWKSURL:      "https://jwks.invalid/.well-known/jwks.json",
		AllowedEmail: testAllowedEmail,
		Client:       f.client,
		Now:          func() time.Time { return f.now },
	}
}

func (f *bearerFixture) claims() map[string]any {
	return map[string]any{
		"iss":            testIssuer,
		"aud":            testAudience,
		"exp":            f.now.Add(time.Hour).Unix(),
		"nbf":            f.now.Add(-time.Minute).Unix(),
		"sub":            "user-test",
		"email":          testAllowedEmail,
		"email_verified": true,
	}
}

func (f *bearerFixture) token(t *testing.T, claims map[string]any) string {
	t.Helper()
	headerJSON, err := json.Marshal(bearerHeader{Algorithm: "EdDSA", KeyID: testKeyID})
	if err != nil {
		t.Fatal(err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	header := base64.RawURLEncoding.EncodeToString(headerJSON)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := header + "." + payload
	signature := base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.privateKey, []byte(signingInput)))
	return signingInput + "." + signature
}

func TestVerifyBearerAcceptsConfiguredVerifiedIdentity(t *testing.T) {
	fixture := newBearerFixture(t)
	identity, err := VerifyBearer(t.Context(), "Bearer "+fixture.token(t, fixture.claims()), fixture.config())
	if err != nil {
		t.Fatal(err)
	}
	if identity.Subject != "user-test" || identity.Email != testAllowedEmail {
		t.Fatalf("identity = %+v", identity)
	}
}

func TestVerifyBearerAcceptsNeonCamelCaseVerificationClaim(t *testing.T) {
	fixture := newBearerFixture(t)
	claims := fixture.claims()
	delete(claims, "email_verified")
	claims["emailVerified"] = true
	if _, err := VerifyBearer(t.Context(), "Bearer "+fixture.token(t, claims), fixture.config()); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyBearerBindsIssuerAndAudienceToAuthOrigin(t *testing.T) {
	fixture := newBearerFixture(t)
	config := fixture.config()
	claims := fixture.claims()
	claims["iss"] = "https://auth.invalid"
	claims["aud"] = "https://auth.invalid"
	if _, err := VerifyBearer(t.Context(), "Bearer "+fixture.token(t, claims), config); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyBearerRejectsIdentityPolicyFailures(t *testing.T) {
	fixture := newBearerFixture(t)
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "wrong issuer", mutate: func(c map[string]any) { c["iss"] = "https://other.invalid" }},
		{name: "wrong audience", mutate: func(c map[string]any) { c["aud"] = "other-app" }},
		{name: "expired", mutate: func(c map[string]any) { c["exp"] = fixture.now.Add(-time.Second).Unix() }},
		{name: "not active", mutate: func(c map[string]any) { c["nbf"] = fixture.now.Add(time.Minute).Unix() }},
		{name: "unverified email", mutate: func(c map[string]any) { c["email_verified"] = false }},
		{name: "wrong email", mutate: func(c map[string]any) { c["email"] = testOtherEmail }},
		{name: "missing subject", mutate: func(c map[string]any) { delete(c, "sub") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := fixture.claims()
			test.mutate(claims)
			_, err := VerifyBearer(t.Context(), "Bearer "+fixture.token(t, claims), fixture.config())
			if !errorsIs(err, ErrInvalidBearer) {
				t.Fatalf("error = %v, want invalid bearer", err)
			}
		})
	}
}

func TestVerifyBearerAcceptsAudienceArray(t *testing.T) {
	fixture := newBearerFixture(t)
	claims := fixture.claims()
	claims["aud"] = []string{"another-audience", testAudience}
	if _, err := VerifyBearer(t.Context(), "Bearer "+fixture.token(t, claims), fixture.config()); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyBearerRejectsBadSignature(t *testing.T) {
	fixture := newBearerFixture(t)
	token := fixture.token(t, fixture.claims())
	parts := strings.Split(token, ".")
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	signature[0] ^= 0xff
	parts[2] = base64.RawURLEncoding.EncodeToString(signature)
	token = strings.Join(parts, ".")
	if _, err := VerifyBearer(t.Context(), "Bearer "+token, fixture.config()); !errorsIs(err, ErrInvalidBearer) {
		t.Fatalf("error = %v, want invalid bearer", err)
	}
}

func TestVerifyBearerFailsClosedOnMissingPolicy(t *testing.T) {
	fixture := newBearerFixture(t)
	config := fixture.config()
	config.AllowedEmail = ""
	if _, err := VerifyBearer(t.Context(), "Bearer "+fixture.token(t, fixture.claims()), config); !errorsIs(err, ErrAuthConfiguration) {
		t.Fatalf("error = %v, want auth configuration error", err)
	}
}

func TestRequireUserAddsIdentityToContext(t *testing.T) {
	fixture := newBearerFixture(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := UserFromContext(r.Context())
		if !ok || identity.Subject != "user-test" {
			t.Fatalf("identity = %+v, ok=%v", identity, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/private", nil)
	req.Header.Set("Authorization", "Bearer "+fixture.token(t, fixture.claims()))
	rec := httptest.NewRecorder()
	RequireUserWithConfig(fixture.config(), next).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestRequireUserRejectsMissingBearer(t *testing.T) {
	fixture := newBearerFixture(t)
	rec := httptest.NewRecorder()
	RequireUserWithConfig(fixture.config(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("protected handler must not run")
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/private", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func errorsIs(err, target error) bool {
	return err != nil && (err == target || errors.Is(err, target))
}

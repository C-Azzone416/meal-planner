package hosted

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	testIssuer   = "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech"
	testAudience = "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech"
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
		BaseURL:          "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth",
		ExpectedEndpoint: testEndpointID,
		JWKSURL:          "https://jwks.invalid/.well-known/jwks.json",
		AllowedEmail:     testAllowedEmail,
		Client:           f.client,
		Now:              func() time.Time { return f.now },
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
	return f.rawToken(headerJSON, claimsJSON)
}

func (f *bearerFixture) rawToken(headerJSON, claimsJSON []byte) string {
	header := base64.RawURLEncoding.EncodeToString(headerJSON)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := header + "." + payload
	signature := base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.privateKey, []byte(signingInput)))
	return signingInput + "." + signature
}

func TestVerifyBearerRejectsAmbiguousClaimNames(t *testing.T) {
	f := newBearerFixture(t)
	header := []byte(`{"alg":"EdDSA","kid":"test-key"}`)
	claims := f.claims()
	claims["emailVerified"] = true
	claims["iat"] = f.now.Unix()
	claims["jti"] = "test-token"
	raw, _ := json.Marshal(claims)
	for name, value := range claims {
		valueJSON, _ := json.Marshal(value)
		for label, mutated := range map[string]string{
			"uppercase only":    strings.Replace(string(raw), `"`+name+`":`, `"`+strings.ToUpper(name)+`":`, 1),
			"uppercase alias":   string(raw[:len(raw)-1]) + `,"` + strings.ToUpper(name) + `":` + string(valueJSON) + `}`,
			"duplicate":         string(raw[:len(raw)-1]) + `,"` + name + `":` + string(valueJSON) + `}`,
			"escaped duplicate": string(raw[:len(raw)-1]) + fmt.Sprintf(`,"\u%04x%s":%s}`, name[0], name[1:], valueJSON),
			"null overwrite":    string(raw[:len(raw)-1]) + `,"` + name + `":null}`,
		} {
			t.Run(name+"/"+label, func(t *testing.T) {
				if _, err := VerifyBearer(t.Context(), "Bearer "+f.rawToken(header, []byte(mutated)), f.config()); !errors.Is(err, ErrInvalidBearer) {
					t.Fatalf("ambiguous claim accepted: %v", err)
				}
			})
		}
	}
	// Exact judge reproduction, including both orders. A future lowercase nbf
	// must never be replaced by an already-active case alias or duplicate.
	future := strings.Replace(string(raw), `"nbf":1799999940`, `"nbf":1800000060`, 1)
	for _, claimJSON := range []string{
		future[:len(future)-1] + `,"NBF":1799999940}`,
		`{"NBF":1799999940,` + future[1:],
		future[:len(future)-1] + `,"nbf":1799999940}`,
		strings.Replace(string(raw), `"iss":`, `"iſſ":`, 1),
	} {
		if _, err := VerifyBearer(t.Context(), "Bearer "+f.rawToken(header, []byte(claimJSON)), f.config()); !errors.Is(err, ErrInvalidBearer) {
			t.Fatalf("claim alias/overwrite accepted: %v", err)
		}
	}
}

func TestVerifyBearerRejectsAmbiguousHeaderNamesAndExtensions(t *testing.T) {
	f := newBearerFixture(t)
	claims, _ := json.Marshal(f.claims())
	const header = `{"alg":"EdDSA","kid":"test-key","typ":"JWT","b64":true}`
	for name := range authHeaderFields {
		for label, mutated := range map[string]string{
			"alias":     header[:len(header)-1] + `,"` + strings.ToUpper(name) + `":"ignored"}`,
			"duplicate": header[:len(header)-1] + `,"` + name + `":"ignored","` + name + `":"ignored"}`,
		} {
			t.Run(name+"/"+label, func(t *testing.T) {
				if _, err := VerifyBearer(t.Context(), "Bearer "+f.rawToken([]byte(mutated), claims), f.config()); !errors.Is(err, ErrInvalidBearer) {
					t.Fatalf("ambiguous header accepted: %v", err)
				}
			})
		}
	}
	for _, raw := range []string{
		`{"ALG":"EdDSA","kid":"test-key"}`, `{"alg":"EdDSA","KID":"test-key"}`,
		`{"alg":"none","alg":"EdDSA","kid":"test-key"}`,
		`{"alg":"EdDSA","kid":"wrong","\u006bid":"test-key"}`,
		`{"alg":"EdDSA","Kid":"test-key"}`,
		`{"alg":"EdDSA","kid":"test-key","crit":[]}`,
		`{"alg":"EdDSA","kid":"test-key","crit":["extension"],"extension":true}`,
		`{"alg":"EdDSA","kid":"test-key","crit":null}`,
		`{"alg":"EdDSA","kid":"test-key","b64":false}`,
		`{"alg":"EdDSA","kid":"test-key","b64":null}`,
		`{"alg":"EdDSA","kid":"test-key","b64":"true"}`,
		`{"alg":"EdDSA","kid":"test-key","typ":null}`,
		`{"alg":"EdDSA","kid":"test-key","cty":42}`,
		`{"alg":"EdDSA","kid":"test-key"} {}`, `null`, `[]`,
	} {
		if _, err := VerifyBearer(t.Context(), "Bearer "+f.rawToken([]byte(raw), claims), f.config()); !errors.Is(err, ErrInvalidBearer) {
			t.Errorf("unsupported header accepted %s: %v", raw, err)
		}
	}
	for _, name := range []string{"alg", "kid", "typ", "cty", "b64"} {
		for _, value := range []any{nil, 42, []string{"value"}, map[string]string{"value": "EdDSA"}} {
			header := map[string]any{"alg": "EdDSA", "kid": testKeyID}
			header[name] = value
			raw, _ := json.Marshal(header)
			if _, err := VerifyBearer(t.Context(), "Bearer "+f.rawToken(raw, claims), f.config()); !errors.Is(err, ErrInvalidBearer) {
				t.Errorf("invalid %s header type accepted: %v", name, err)
			}
		}
	}
}

func TestVerifyBearerRejectsClaimTypeCoercion(t *testing.T) {
	f := newBearerFixture(t)
	for _, name := range []string{"iss", "aud", "sub", "exp", "nbf", "iat", "jti", "email", "emailVerified", "email_verified"} {
		for _, value := range []any{nil, []any{nil}, map[string]any{"value": true}} {
			t.Run(name+"/"+fmt.Sprint(value), func(t *testing.T) {
				claims := f.claims()
				claims[name] = value
				if _, err := VerifyBearer(t.Context(), "Bearer "+f.token(t, claims), f.config()); !errors.Is(err, ErrInvalidBearer) {
					t.Fatalf("invalid claim type accepted: %v", err)
				}
			})
		}
	}
	claims := f.claims()
	claims["aud"] = []any{nil, testAudience}
	if _, err := VerifyBearer(t.Context(), "Bearer "+f.token(t, claims), f.config()); !errors.Is(err, ErrInvalidBearer) {
		t.Fatalf("audience null element accepted: %v", err)
	}
	for _, name := range []string{"iat", "jti", "emailVerified", "email_verified"} {
		claims := f.claims()
		claims[name] = 42
		if name == "iat" {
			claims[name] = "1800000000"
		}
		if _, err := VerifyBearer(t.Context(), "Bearer "+f.token(t, claims), f.config()); !errors.Is(err, ErrInvalidBearer) {
			t.Errorf("coerced %s accepted: %v", name, err)
		}
	}
}

func TestVerifyBearerPreservesProviderCompatibility(t *testing.T) {
	f := newBearerFixture(t)
	for _, verified := range []map[string]any{
		{"email_verified": true}, {"emailVerified": true}, {"emailVerified": true, "email_verified": true},
	} {
		claims := f.claims()
		delete(claims, "email_verified")
		for key, value := range verified {
			claims[key] = value
		}
		claims["iat"] = json.Number("1.8e9")
		claims["jti"] = "token-id"
		claims["email"] = strings.ToUpper(testAllowedEmail)
		claims["name"] = "Sample Person 🍽"
		claims["profile"] = map[string]any{"image": nil, "number": json.Number("9007199254740993")}
		raw, _ := json.Marshal(claims)
		raw = bytes.Replace(raw, []byte(`"iss":`), []byte(`"\u0069ss":`), 1)
		header := []byte(`{"alg":"EdDSA","\u006bid":"test-key","typ":"JWT","b64":true,"providerExtension":{"value":null}}`)
		if _, err := VerifyBearer(t.Context(), "Bearer "+f.rawToken(header, raw), f.config()); err != nil {
			t.Fatalf("supported provider claims rejected: %v", err)
		}
	}
	for _, snake := range []bool{true, false} {
		claims := f.claims()
		claims["email_verified"], claims["emailVerified"] = snake, !snake
		if _, err := VerifyBearer(t.Context(), "Bearer "+f.token(t, claims), f.config()); !errors.Is(err, ErrInvalidBearer) {
			t.Fatalf("conflicting verification claims accepted: %v", err)
		}
	}
}

func TestVerifyBearerRejectsMalformedUnicode(t *testing.T) {
	f := newBearerFixture(t)
	header := []byte(`{"alg":"EdDSA","kid":"test-key"}`)
	claims, _ := json.Marshal(f.claims())
	for _, value := range []string{`"\ud800"`, `"\udfff"`, `"\ud800x"`, `"\ud800\u0061"`, "\"\xff\""} {
		badClaims := append(append([]byte{}, claims[:len(claims)-1]...), []byte(`,"extension":`+value+`}`)...)
		badHeader := append(append([]byte{}, header[:len(header)-1]...), []byte(`,"extension":`+value+`}`)...)
		for _, token := range []string{f.rawToken(header, badClaims), f.rawToken(badHeader, claims)} {
			if _, err := VerifyBearer(t.Context(), "Bearer "+token, f.config()); !errors.Is(err, ErrInvalidBearer) {
				t.Fatalf("lossy Unicode accepted: %v", err)
			}
		}
	}
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
	claims["iss"] = testIssuer
	claims["aud"] = testAudience
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

func TestVerifyBearerFailsClosedWithoutEnvironmentBinding(t *testing.T) {
	fixture := newBearerFixture(t)
	config := fixture.config()
	config.ExpectedEndpoint = ""
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

func TestVerifyBearerNumericDateSemantics(t *testing.T) {
	f := newBearerFixture(t)
	tests := []struct {
		name, claim string
		value       any
		nanos       int
		valid       bool
	}{
		{"future fractional nbf", "nbf", json.Number("1800000000.9"), 0, false},
		{"nbf equality", "nbf", json.Number("1800000000.9"), 900000000, true},
		{"nbf before fraction", "nbf", json.Number("1800000000.9"), 899999999, false},
		{"nbf after fraction", "nbf", json.Number("1800000000.9"), 900000001, true},
		{"subnanosecond nbf", "nbf", json.Number("1800000000.0000000001"), 0, false},
		{"fractional exp", "exp", json.Number("1800000000.9"), 0, true},
		{"exp equality", "exp", json.Number("1800000000.9"), 900000000, false},
		{"exp before fraction", "exp", json.Number("1800000000.9"), 899999999, true},
		{"exp after fraction", "exp", json.Number("1800000000.9"), 900000001, false},
		{"subnanosecond exp", "exp", json.Number("1800000000.0000000001"), 0, true},
		{"scientific exp", "exp", json.Number("1.8000000009e9"), 0, true},
		{"negative nbf", "nbf", json.Number("-0.9"), 0, true},
	}
	for _, claim := range []string{"exp", "nbf"} {
		for _, value := range []any{"4102444800", "1", nil, true, []int{1}, map[string]int{"value": 1}, json.Number("1e1000"), json.Number("-1e1000"), json.Number("1e1001"), json.Number("1e-1001"), json.Number("9223372036854775808"), json.Number("-9223372036854775809")} {
			tests = append(tests, struct {
				name, claim string
				value       any
				nanos       int
				valid       bool
			}{"invalid type or range", claim, value, 0, false})
		}
	}
	for _, tc := range tests {
		t.Run(tc.claim+"/"+tc.name, func(t *testing.T) {
			f.now = time.Unix(1800000000, int64(tc.nanos))
			claims := f.claims()
			claims[tc.claim] = tc.value
			_, err := VerifyBearer(context.Background(), "Bearer "+f.token(t, claims), f.config())
			if tc.valid && err != nil {
				t.Fatalf("valid date rejected: %v", err)
			}
			if !tc.valid && !errors.Is(err, ErrInvalidBearer) {
				t.Fatalf("invalid date %v accepted: %v", tc.value, err)
			}
		})
	}
	t.Run("nbf optional", func(t *testing.T) {
		claims := f.claims()
		delete(claims, "nbf")
		if _, err := VerifyBearer(context.Background(), "Bearer "+f.token(t, claims), f.config()); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("exp required", func(t *testing.T) {
		claims := f.claims()
		delete(claims, "exp")
		if _, err := VerifyBearer(context.Background(), "Bearer "+f.token(t, claims), f.config()); !errors.Is(err, ErrInvalidBearer) {
			t.Fatalf("error: %v", err)
		}
	})
}

func TestNumericDateRepresentationBounds(t *testing.T) {
	for _, raw := range []string{"9223372036854775807", "-9223372036854775808", "1e-1000", "-0", "1.5e+1"} {
		if _, err := numericDate(json.RawMessage(raw)); err != nil {
			t.Errorf("valid %s: %v", raw, err)
		}
	}
	for _, raw := range []string{"9223372036854775807.000000001", "-9223372036854775808.000000001", "1e999999999", "1e-999999999", "NaN", "Infinity", "1 2", "01", "1/2", "0." + strings.Repeat("0", 128) + "1"} {
		if _, err := numericDate(json.RawMessage(raw)); err == nil {
			t.Errorf("accepted invalid %s", raw)
		}
	}
}

// Invalid signature encodings previously triggered a provider request before
// rejection (and became a 503 during an outage). They must be rejected locally.
func TestVerifyBearerRejectsMalformedSignatureWithoutProviderIO(t *testing.T) {
	f := newBearerFixture(t)
	token := strings.Split(f.token(t, f.claims()), ".")
	for _, signature := range []string{"!", "AA", base64.RawURLEncoding.EncodeToString(make([]byte, 63)), base64.RawURLEncoding.EncodeToString(make([]byte, 65)), strings.Repeat("A", 86) + "="} {
		t.Run(signature, func(t *testing.T) {
			calls := 0
			config := f.config()
			config.Client = httpDoerFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("synthetic provider outage")
			})
			_, err := VerifyBearer(t.Context(), "Bearer "+token[0]+"."+token[1]+"."+signature, config)
			if !errors.Is(err, ErrInvalidBearer) || calls != 0 {
				t.Fatalf("err=%v provider calls=%d", err, calls)
			}
		})
	}
	// Well-formed signatures still require provider verification and fail closed.
	calls := 0
	config := f.config()
	config.Client = httpDoerFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("synthetic provider outage")
	})
	_, err := VerifyBearer(t.Context(), "Bearer "+strings.Join(token, "."), config)
	if !errors.Is(err, ErrAuthConfiguration) || calls != 1 {
		t.Fatalf("err=%v provider calls=%d", err, calls)
	}
}

func TestVerifyBearerRejectsHopelessClaimsBeforeProviderIO(t *testing.T) {
	f := newBearerFixture(t)
	cases := map[string]map[string]any{}
	for _, name := range []string{"expired", "future", "wrong issuer", "wrong audience", "invalid exp", "invalid iat"} {
		claims := f.claims()
		switch name {
		case "expired":
			claims["exp"] = f.now.Unix()
		case "future":
			claims["nbf"] = f.now.Add(time.Hour).Unix()
		case "wrong issuer":
			claims["iss"] = "https://other.example.test"
		case "wrong audience":
			claims["aud"] = "https://other.example.test"
		case "invalid exp":
			claims["exp"] = "tomorrow"
		case "invalid iat":
			claims["iat"] = "yesterday"
		}
		cases[name] = claims
	}
	for name, claims := range cases {
		t.Run(name, func(t *testing.T) {
			calls := 0
			config := f.config()
			config.Client = httpDoerFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("synthetic outage") })
			_, err := VerifyBearer(t.Context(), "Bearer "+f.token(t, claims), config)
			if !errors.Is(err, ErrInvalidBearer) || calls != 0 {
				t.Fatalf("err=%v provider calls=%d", err, calls)
			}
		})
	}
}

func TestVerifyBearerPrivateMembershipRemainsAfterSignature(t *testing.T) {
	f := newBearerFixture(t)
	for _, email := range []string{testAllowedEmail, "not-a-member@example.test"} {
		calls := 0
		config := f.config()
		config.Client = httpDoerFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("synthetic outage") })
		claims := f.claims()
		claims["email"] = email
		_, err := VerifyBearer(t.Context(), "Bearer "+f.token(t, claims), config)
		if !errors.Is(err, ErrAuthConfiguration) || calls != 1 {
			t.Fatalf("private membership changed pre-verification behavior: %v calls=%d", err, calls)
		}
	}
}

func TestVerifyBearerRechecksExpiryAfterKeyRetrieval(t *testing.T) {
	f := newBearerFixture(t)
	claims := f.claims()
	claims["exp"] = f.now.Add(time.Second).Unix()
	config := f.config()
	original := config.Client
	config.Client = httpDoerFunc(func(r *http.Request) (*http.Response, error) {
		response, err := original.Do(r)
		f.now = f.now.Add(2 * time.Second)
		return response, err
	})
	if _, err := VerifyBearer(t.Context(), "Bearer "+f.token(t, claims), config); !errors.Is(err, ErrInvalidBearer) {
		t.Fatalf("expired during retrieval: %v", err)
	}
}

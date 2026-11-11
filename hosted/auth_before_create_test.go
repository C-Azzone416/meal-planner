package hosted

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	testAllowedEmail = "allowed@example.invalid"
	testOtherEmail   = "other@example.invalid"
	testEndpointID   = "ep-test-environment"
	testKeyID        = "test-key"
)

type webhookFixture struct {
	privateKey ed25519.PrivateKey
	now        time.Time
	client     HTTPDoer
}

type httpDoerFunc func(*http.Request) (*http.Response, error)

func (f httpDoerFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

func newWebhookFixture(t *testing.T) *webhookFixture {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &webhookFixture{
		privateKey: privateKey,
		now:        time.Unix(1_800_000_000, 0).UTC(),
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
	fixture.client = httpDoerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(jwksBody)),
			Header:     make(http.Header),
		}, nil
	})
	return fixture
}

func (f *webhookFixture) config() AuthWebhookConfig {
	return AuthWebhookConfig{
		BaseURL:          "https://ep-test-environment.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth",
		AllowedEmail:     testAllowedEmail,
		ExpectedEndpoint: testEndpointID,
		JWKSURL:          "https://jwks.invalid/.well-known/jwks.json",
		Client:           f.client,
		Now:              func() time.Time { return f.now },
	}
}

func (f *webhookFixture) payload(email string) []byte {
	return []byte(fmt.Sprintf(`{"event_id":"event-test","event_type":"%s","context":{"endpoint_id":"%s"},"user":{"email":%q}}`, beforeCreateEventType, testEndpointID, email))
}

func (f *webhookFixture) request(body []byte) *http.Request {
	// Exercise the documented Neon webhook profile throughout this suite.
	typ := "JWS"
	headerJSON, _ := json.Marshal(detachedJWSHeader{Algorithm: "EdDSA", KeyID: testKeyID, Type: &typ})
	return f.requestHeader(body, headerJSON)
}

func (f *webhookFixture) requestHeader(body, headerJSON []byte) *http.Request {
	timestamp := strconv.FormatInt(f.now.UnixMilli(), 10)
	header := base64.RawURLEncoding.EncodeToString(headerJSON)
	payloadBase64 := base64.RawURLEncoding.EncodeToString(body)
	timestamped := timestamp + "." + payloadBase64
	signingInput := header + "." + base64.RawURLEncoding.EncodeToString([]byte(timestamped))
	signature := base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.privateKey, []byte(signingInput)))

	req := httptest.NewRequest(http.MethodPost, "/api/auth/before-create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Neon-Signature", header+".."+signature)
	req.Header.Set("X-Neon-Signature-Kid", testKeyID)
	req.Header.Set("X-Neon-Timestamp", timestamp)
	req.Header.Set("X-Neon-Event-Type", beforeCreateEventType)
	req.Header.Set("X-Neon-Event-Id", "event-test")
	return req
}

func TestAuthBeforeCreateAcceptsNeonDocumentedJWSHeader(t *testing.T) {
	// https://neon.com/docs/auth/guides/webhooks#request-headers documents
	// this protected header for the common webhook signature envelope,
	// including user.before_create. Only the illustrative kid is substituted;
	// all signatures, identities and keys below are synthetic.
	const sample = "eyJhbGciOiJFZERTQSIsInR5cCI6IkpXUyIsImtpZCI6IjAxZGVjNTJiIn0"
	header, err := base64.RawURLEncoding.DecodeString(sample)
	if err != nil {
		t.Fatal(err)
	}
	if string(header) != `{"alg":"EdDSA","typ":"JWS","kid":"01dec52b"}` {
		t.Fatalf("unexpected documented header: %s", header)
	}
	header = bytes.Replace(header, []byte("01dec52b"), []byte(testKeyID), 1)
	f := newWebhookFixture(t)
	for _, email := range []string{testAllowedEmail, testOtherEmail} {
		t.Run(email, func(t *testing.T) {
			rec := httptest.NewRecorder()
			AuthBeforeCreate(f.config()).ServeHTTP(rec, f.requestHeader(f.payload(email), header))
			want := "{\"allowed\":true}\n"
			if email == testOtherEmail {
				want = "{\"allowed\":false,\"error_message\":\"This app is private and is not accepting signups for this email.\",\"error_code\":\"SIGNUP_NOT_ALLOWED\"}\n"
			}
			if rec.Code != http.StatusOK || rec.Body.String() != want {
				t.Fatalf("status = %d, body = %q, want 200 and %q", rec.Code, rec.Body.String(), want)
			}
		})
	}
}

func TestAuthBeforeCreateJWSRetainsSignatureAndEnvelopeRestrictions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*webhookFixture, *http.Request) *http.Request
		status int
	}{
		{"unsigned", func(_ *webhookFixture, req *http.Request) *http.Request {
			req.Header.Del("X-Neon-Signature")
			return req
		}, http.StatusBadRequest},
		{"changed raw body", func(f *webhookFixture, req *http.Request) *http.Request {
			// Semantically identical JSON must not survive a byte change.
			req.Body = io.NopCloser(bytes.NewReader(append(f.payload(testAllowedEmail), '\n')))
			return req
		}, http.StatusUnauthorized},
		{"changed fresh timestamp", func(f *webhookFixture, req *http.Request) *http.Request {
			req.Header.Set("X-Neon-Timestamp", strconv.FormatInt(f.now.Add(time.Second).UnixMilli(), 10))
			return req
		}, http.StatusUnauthorized},
		{"different signing key", func(f *webhookFixture, _ *http.Request) *http.Request {
			other := newWebhookFixture(t)
			return other.request(f.payload(testAllowedEmail))
		}, http.StatusUnauthorized},
		{"wrong protected key ID", func(f *webhookFixture, _ *http.Request) *http.Request {
			return f.requestHeader(f.payload(testAllowedEmail), []byte(`{"alg":"EdDSA","typ":"JWS","kid":"other-key"}`))
		}, http.StatusUnauthorized},
		{"empty protected key ID", func(f *webhookFixture, _ *http.Request) *http.Request {
			return f.requestHeader(f.payload(testAllowedEmail), []byte(`{"alg":"EdDSA","typ":"JWS","kid":""}`))
		}, http.StatusUnauthorized},
		{"wrong algorithm", func(f *webhookFixture, _ *http.Request) *http.Request {
			return f.requestHeader(f.payload(testAllowedEmail), []byte(`{"alg":"HS256","typ":"JWS","kid":"test-key"}`))
		}, http.StatusUnauthorized},
		{"non-detached payload", func(_ *webhookFixture, req *http.Request) *http.Request {
			req.Header.Set("X-Neon-Signature", strings.Replace(req.Header.Get("X-Neon-Signature"), "..", ".e30.", 1))
			return req
		}, http.StatusUnauthorized},
		{"wrong event header", func(_ *webhookFixture, req *http.Request) *http.Request {
			req.Header.Set("X-Neon-Event-Type", "user.created")
			return req
		}, http.StatusBadRequest},
		{"signed wrong event", func(f *webhookFixture, _ *http.Request) *http.Request {
			body := bytes.Replace(f.payload(testAllowedEmail), []byte(beforeCreateEventType), []byte("user.created"), 1)
			return f.request(body)
		}, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newWebhookFixture(t)
			req := tc.mutate(f, f.request(f.payload(testAllowedEmail)))
			rec := httptest.NewRecorder()
			AuthBeforeCreate(f.config()).ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

func TestAuthBeforeCreateRejectsAmbiguousSignedIdentityJSON(t *testing.T) {
	f := newWebhookFixture(t)
	valid := string(f.payload(testAllowedEmail))
	for _, field := range []struct{ name, value string }{
		{"event_id", `"event-test"`}, {"event_type", `"user.before_create"`},
		{"context", `{"endpoint_id":"ep-test-environment"}`}, {"user", `{"email":"allowed@example.invalid"}`},
		{"endpoint_id", `"ep-test-environment"`}, {"email", `"allowed@example.invalid"`},
	} {
		original := `"` + field.name + `":` + field.value
		for label, replacement := range map[string]string{
			"case alias":        `"` + strings.ToUpper(field.name) + `":` + field.value,
			"duplicate":         original + `,` + original,
			"escaped duplicate": original + fmt.Sprintf(`,"\u%04x%s":%s`, field.name[0], field.name[1:], field.value),
			"null":              `"` + field.name + `":null`,
			"null overwrite":    original + `,"` + field.name + `":null`,
			"wrong type":        `"` + field.name + `":42`,
		} {
			t.Run(field.name+"/"+label, func(t *testing.T) {
				body := []byte(strings.Replace(valid, original, replacement, 1))
				rec := httptest.NewRecorder()
				AuthBeforeCreate(f.config()).ServeHTTP(rec, f.request(body))
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, %s", rec.Code, rec.Body.String())
				}
			})
		}
	}
	for _, extension := range []string{`"\ud800"`, `"\udfff"`, "\"\xff\"", `{"key":1,"key":2}`} {
		body := []byte(valid[:len(valid)-1] + `,"extension":` + extension + `}`)
		rec := httptest.NewRecorder()
		AuthBeforeCreate(f.config()).ServeHTTP(rec, f.request(body))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("lossy extension accepted: %d", rec.Code)
		}
	}
}

func TestAuthBeforeCreateRejectsAmbiguousJWSHeaders(t *testing.T) {
	f := newWebhookFixture(t)
	for _, header := range []string{
		`{"ALG":"EdDSA","kid":"test-key"}`, `{"alg":"EdDSA","KID":"test-key"}`,
		`{"alg":"none","alg":"EdDSA","kid":"test-key"}`,
		`{"alg":"EdDSA","kid":"wrong","\u006bid":"test-key"}`,
		`{"alg":"EdDSA","Kid":"test-key"}`,
		`{"alg":"EdDSA","kid":"test-key","crit":["extension"],"extension":true}`,
		`{"alg":"EdDSA","kid":"test-key","b64":false}`,
		`{"alg":"EdDSA","kid":"test-key","extension":"\udfff"}`,
		`{"alg":"EdDSA","kid":"test-key","extension":"` + string([]byte{0xff}) + `"}`,
		`{"alg":"EdDSA","kid":"test-key","extension":"` + strings.Repeat("x", maxBearerTokenBytes) + `"}`,
	} {
		rec := httptest.NewRecorder()
		AuthBeforeCreate(f.config()).ServeHTTP(rec, f.requestHeader(f.payload(testAllowedEmail), []byte(header)))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("header accepted: %d", rec.Code)
		}
	}
}

func TestAuthBeforeCreatePreservesProviderExtensions(t *testing.T) {
	f := newWebhookFixture(t)
	body := []byte(`{"event_id":"event-test","event_type":"user.before_create","context":{"\u0065ndpoint_id":"ep-test-environment","provider":null},"user":{"email":"ALLOWED@example.invalid","name":"Sample \ud83c\udf7d","image":null},"extra":{"count":9007199254740993}}`)
	header := []byte(`{"alg":"EdDSA","kid":"test-key","typ":"JWS","b64":true,"providerExtension":null}`)
	rec := httptest.NewRecorder()
	AuthBeforeCreate(f.config()).ServeHTTP(rec, f.requestHeader(body, header))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"allowed":true`) {
		t.Fatalf("status = %d, %s", rec.Code, rec.Body.String())
	}
}

func TestSigningKeyRejectsAmbiguousJWKS(t *testing.T) {
	f := newWebhookFixture(t)
	x := base64.RawURLEncoding.EncodeToString(f.privateKey.Public().(ed25519.PublicKey))
	const prefix = `{"kty":"OKP","crv":"Ed25519","kid":"test-key","x":"`
	key := prefix + x + `","alg":"EdDSA","use":"sig","key_ops":["verify"]}`
	valid := `{"keys":[` + key + `]}`
	check := func(t *testing.T, body string, accepted bool) {
		t.Helper()
		client := httpDoerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		got, err := fetchEd25519PublicKey(t.Context(), client, "https://jwks.invalid/keys", testKeyID)
		if accepted && (err != nil || !bytes.Equal(got, f.privateKey.Public().(ed25519.PublicKey))) {
			t.Fatalf("valid key rejected: %v", err)
		}
		if !accepted && err == nil {
			t.Fatal("ambiguous key accepted")
		}
	}
	for _, field := range []struct{ name, value string }{
		{"keys", `[` + key + `]`}, {"kty", `"OKP"`}, {"crv", `"Ed25519"`},
		{"kid", `"test-key"`}, {"x", `"` + x + `"`}, {"alg", `"EdDSA"`}, {"use", `"sig"`}, {"key_ops", `["verify"]`},
	} {
		original := `"` + field.name + `":` + field.value
		for label, replacement := range map[string]string{
			"uppercase":         `"` + strings.ToUpper(field.name) + `":` + field.value,
			"duplicate":         original + `,` + original,
			"escaped duplicate": original + fmt.Sprintf(`,"\u%04x%s":%s`, field.name[0], field.name[1:], field.value),
			"null":              `"` + field.name + `":null`,
			"wrong type":        `"` + field.name + `":42`,
		} {
			t.Run(field.name+"/"+label, func(t *testing.T) { check(t, strings.Replace(valid, original, replacement, 1), false) })
		}
	}
	for _, bad := range []string{
		`{"keys":[` + key + `,` + key + `]}`,
		strings.Replace(valid, `"alg":"EdDSA"`, `"alg":"RS256"`, 1),
		strings.Replace(valid, `"alg":"EdDSA"`, `"alg":""`, 1),
		strings.Replace(valid, `"use":"sig"`, `"use":"enc"`, 1),
		strings.Replace(valid, `"use":"sig"`, `"use":""`, 1),
		strings.Replace(valid, `["verify"]`, `[]`, 1),
		strings.Replace(valid, `["verify"]`, `["sign"]`, 1),
		strings.Replace(valid, `["verify"]`, `["verify","encrypt"]`, 1),
		strings.Replace(valid, `["verify"]`, `["verify","verify"]`, 1),
		strings.Replace(valid, `["verify"]`, `[null,"verify"]`, 1),
		strings.Replace(valid, `"keys":`, `"Keys":`, 1),
		strings.Replace(valid, `"kid":`, `"Kid":`, 1),
		`{"keys":[` + key + `],"extra":"\ud800"}`,
		`{"keys":[` + key + `],"extra":"` + string([]byte{0xff}) + `"}`,
	} {
		check(t, bad, false)
	}
	check(t, valid, true)
	check(t, `{"keys":[`+prefix+x+`"}],"metadata":{"image":null,"name":"Test 🍽"}}`, true)
	check(t, strings.Replace(valid, `"kid":`, `"\u006bid":`, 1), true)
	// Rotation remains supported: unrelated IDs do not make selection ambiguous.
	check(t, `{"keys":[`+strings.Replace(key, testKeyID, "previous-key", 1)+`,`+key+`]}`, true)
}

func TestAuthBeforeCreateAllowsOnlyConfiguredEmail(t *testing.T) {
	fixture := newWebhookFixture(t)
	for _, test := range []struct {
		name    string
		email   string
		allowed bool
	}{
		{name: "exact", email: testAllowedEmail, allowed: true},
		{name: "case insensitive", email: strings.ToUpper(testAllowedEmail), allowed: true},
		{name: "different address", email: testOtherEmail, allowed: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			AuthBeforeCreate(fixture.config()).ServeHTTP(rec, fixture.request(fixture.payload(test.email)))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			var got beforeCreateResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Allowed != test.allowed {
				t.Fatalf("allowed = %v, want %v", got.Allowed, test.allowed)
			}
			if !test.allowed && (got.ErrorCode == "" || got.ErrorMessage == "") {
				t.Fatalf("rejection response missing error details: %+v", got)
			}
		})
	}
}

func TestAuthBeforeCreateFailsClosedWhenConfigurationIsMissing(t *testing.T) {
	fixture := newWebhookFixture(t)
	config := fixture.config()
	config.AllowedEmail = ""
	rec := httptest.NewRecorder()
	AuthBeforeCreate(config).ServeHTTP(rec, fixture.request(fixture.payload(testAllowedEmail)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestAuthBeforeCreateFailsClosedWithoutEnvironmentBinding(t *testing.T) {
	fixture := newWebhookFixture(t)
	config := fixture.config()
	config.ExpectedEndpoint = ""
	rec := httptest.NewRecorder()
	AuthBeforeCreate(config).ServeHTTP(rec, fixture.request(fixture.payload(testAllowedEmail)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestAuthBeforeCreateRejectsStaleAndFutureTimestamps(t *testing.T) {
	fixture := newWebhookFixture(t)
	for _, drift := range []time.Duration{-6 * time.Minute, 6 * time.Minute} {
		req := fixture.request(fixture.payload(testAllowedEmail))
		fixture.now = fixture.now.Add(drift)
		rec := httptest.NewRecorder()
		AuthBeforeCreate(fixture.config()).ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("drift %v: status = %d, want 401", drift, rec.Code)
		}
		fixture.now = time.Unix(1_800_000_000, 0).UTC()
	}
}

func TestAuthBeforeCreateRejectsBadSignature(t *testing.T) {
	fixture := newWebhookFixture(t)
	req := fixture.request(fixture.payload(testAllowedEmail))
	req.Header.Set("X-Neon-Signature", req.Header.Get("X-Neon-Signature")+"corrupt")
	rec := httptest.NewRecorder()
	AuthBeforeCreate(fixture.config()).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuthBeforeCreateRejectsEnvelopeMismatch(t *testing.T) {
	fixture := newWebhookFixture(t)
	body := fixture.payload(testAllowedEmail)
	req := fixture.request(body)
	req.Header.Set("X-Neon-Event-Id", "different-event")
	rec := httptest.NewRecorder()
	AuthBeforeCreate(fixture.config()).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAuthBeforeCreateRejectsWrongEndpoint(t *testing.T) {
	fixture := newWebhookFixture(t)
	config := fixture.config()
	body := bytes.Replace(fixture.payload(testAllowedEmail), []byte(testEndpointID), []byte("ep-other-environment"), 1)
	rec := httptest.NewRecorder()
	AuthBeforeCreate(config).ServeHTTP(rec, fixture.request(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAuthBeforeCreateRejectsOversizedBody(t *testing.T) {
	fixture := newWebhookFixture(t)
	config := fixture.config()
	config.MaxBodyBytes = 16
	body := fixture.payload(testAllowedEmail)
	rec := httptest.NewRecorder()
	AuthBeforeCreate(config).ServeHTTP(rec, fixture.request(body))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestAuthBeforeCreateFailsClosedOnJWKSFailure(t *testing.T) {
	fixture := newWebhookFixture(t)
	config := fixture.config()
	config.Client = httpDoerFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("unavailable")
	})
	rec := httptest.NewRecorder()
	AuthBeforeCreate(config).ServeHTTP(rec, fixture.request(fixture.payload(testAllowedEmail)))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestAuthBeforeCreateRejectsMalformedSignedJSON(t *testing.T) {
	fixture := newWebhookFixture(t)
	body := []byte(`{"event_id":`)
	rec := httptest.NewRecorder()
	AuthBeforeCreate(fixture.config()).ServeHTTP(rec, fixture.request(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAuthBeforeCreateRejectsOtherMethods(t *testing.T) {
	rec := httptest.NewRecorder()
	AuthBeforeCreate(AuthWebhookConfig{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/before-create", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

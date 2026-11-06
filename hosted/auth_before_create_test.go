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
		BaseURL:          "https://auth.invalid/example/auth",
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
	timestamp := strconv.FormatInt(f.now.UnixMilli(), 10)
	headerJSON, _ := json.Marshal(detachedJWSHeader{Algorithm: "EdDSA", KeyID: testKeyID})
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
	config.ExpectedEndpoint = "ep-other-environment"
	rec := httptest.NewRecorder()
	AuthBeforeCreate(config).ServeHTTP(rec, fixture.request(fixture.payload(testAllowedEmail)))
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

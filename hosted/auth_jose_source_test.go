package hosted

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuthJOSETypeProfile(t *testing.T) {
	bearer, webhook := newBearerFixture(t), newWebhookFixture(t)
	claims, err := json.Marshal(bearer.claims())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, metadata                  string
		bearerAccepted, webhookAccepted bool
	}{
		{"omitted", ``, true, true},
		{"JWT", `,"typ":"JWT"`, true, false},
		{"JWS", `,"typ":"JWS"`, false, true},
		{"escaped JWT", `,"t\u0079p":"\u004aWT"`, true, false},
		{"escaped JWS", `,"t\u0079p":"\u004a\u0057\u0053"`, false, true},
		{"encoded bearer payload", `,"typ":"JWT","b64":true`, true, false},
		{"encoded webhook payload", `,"typ":"JWS","b64":true`, false, true},
		{"bearer provider metadata", `,"typ":"JWT","provider":{"nullable":null,"label":"\ud83e\udd6f"}`, true, false},
		{"webhook provider metadata", `,"typ":"JWS","provider":{"nullable":null,"label":"\ud83e\udd6f"}`, false, true},
		{"empty type", `,"typ":""`, false, false},
		{"refresh token", `,"typ":"refresh+jwt"`, false, false},
		{"access media type", `,"typ":"at+jwt"`, false, false},
		{"full JWT media type", `,"typ":"application/jwt"`, false, false},
		{"full JWS media type", `,"typ":"application/jose"`, false, false},
		{"lowercase JWT", `,"typ":"jwt"`, false, false},
		{"lowercase JWS", `,"typ":"jws"`, false, false},
		{"mixed case JWT", `,"typ":"Jwt"`, false, false},
		{"mixed case JWS", `,"typ":"Jws"`, false, false},
		{"padded JWT", `,"typ":" JWT "`, false, false},
		{"padded JWS", `,"typ":" JWS "`, false, false},
		{"Unicode padded JWT", `,"typ":"\u00a0JWT"`, false, false},
		{"Unicode padded JWS", `,"typ":"\u00a0JWS"`, false, false},
		{"Unicode case alias JWS", `,"typ":"JWſ"`, false, false},
		{"malformed type Unicode", `,"typ":"\ud800"`, false, false},
		{"invalid UTF-8 type", ",\"typ\":\"JW\xff\"", false, false},
		{"null type", `,"typ":null`, false, false},
		{"number type", `,"typ":42`, false, false},
		{"bool type", `,"typ":true`, false, false},
		{"array type", `,"typ":["JWT"]`, false, false},
		{"object type", `,"typ":{"value":"JWS"}`, false, false},
		{"case bearer type alias", `,"TYP":"JWT"`, false, false},
		{"case webhook type alias", `,"TYP":"JWS"`, false, false},
		{"duplicate bearer type", `,"typ":"JWT","typ":"JWT"`, false, false},
		{"duplicate webhook type", `,"typ":"JWS","typ":"JWS"`, false, false},
		{"escaped duplicate bearer type", `,"typ":"JWT","t\u0079p":"JWT"`, false, false},
		{"escaped duplicate webhook type", `,"typ":"JWS","t\u0079p":"JWS"`, false, false},
		{"bearer then webhook type", `,"typ":"JWT","typ":"JWS"`, false, false},
		{"webhook then bearer type", `,"typ":"JWS","typ":"JWT"`, false, false},
		{"conflicting type first", `,"typ":"refresh+jwt","typ":"JWT"`, false, false},
		{"conflicting type last", `,"typ":"JWS","typ":"refresh+jwt"`, false, false},
		{"null bearer type overwrite", `,"typ":"JWT","typ":null`, false, false},
		{"null webhook type overwrite", `,"typ":"JWS","typ":null`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Whitespace and escapes deliberately survive in the signed bytes.
			header := []byte(`{ "alg" : "EdDSA", "kid" : "test-key"` + tc.metadata + ` }`)
			_, err := VerifyBearer(t.Context(), "Bearer "+bearer.rawToken(header, claims), bearer.config())
			if tc.bearerAccepted && err != nil || !tc.bearerAccepted && !errors.Is(err, ErrInvalidBearer) {
				t.Errorf("bearer error = %v, accepted = %t", err, tc.bearerAccepted)
			}
			rec := httptest.NewRecorder()
			AuthBeforeCreate(webhook.config()).ServeHTTP(rec, webhook.requestHeader(webhook.payload(testAllowedEmail), header))
			want := http.StatusUnauthorized
			if tc.webhookAccepted {
				want = http.StatusOK
			}
			if rec.Code != want || tc.webhookAccepted && rec.Body.String() != "{\"allowed\":true}\n" {
				t.Errorf("webhook status = %d, body = %s, want = %d", rec.Code, rec.Body.String(), want)
			}
		})
	}
}

func TestAuthJOSESharedSignatureRestrictions(t *testing.T) {
	bearer, webhook := newBearerFixture(t), newWebhookFixture(t)
	claims, _ := json.Marshal(bearer.claims())
	for _, typ := range []string{"", `,"typ":"JWT"`, `,"typ":"JWS"`} {
		for _, metadata := range []string{
			`,"cty":"JWT"`, `,"cty":"JWS"`, `,"c\u0074y":"\u004aWT"`,
			`,"cty":"application/json"`, `,"cty":"text/plain"`, `,"cty":""`, `,"cty":null`,
			`,"cty":42`, `,"cty":true`, `,"cty":["JWT"]`, `,"cty":{"value":"JWT"}`,
			`,"CTY":"JWT"`, `,"cty":"JWT","cty":"JWT"`, `,"cty":"JWT","c\u0074y":"JWT"`,
			`,"crit":[]`, `,"crit":["typ"]`, `,"crit":["b64"],"b64":true`,
			`,"crit":["custom"],"custom":true`, `,"crit":null`, `,"crit":"typ"`, `,"CRIT":[]`,
			`,"b64":false`, `,"b64":null`, `,"b64":"true"`, `,"B64":true`,
			`,"b64":false,"b64":true`, `,"b64":true,"b64":false`,
			`,"b64":true,"b\u00364":true`, `,"crit":["b64"],"b64":false`,
		} {
			t.Run(typ+metadata, func(t *testing.T) {
				header := []byte(`{"alg":"EdDSA","kid":"test-key"` + typ + metadata + `}`)
				if _, err := VerifyBearer(t.Context(), "Bearer "+bearer.rawToken(header, claims), bearer.config()); !errors.Is(err, ErrInvalidBearer) {
					t.Errorf("unsupported bearer metadata accepted: %v", err)
				}
				rec := httptest.NewRecorder()
				AuthBeforeCreate(webhook.config()).ServeHTTP(rec, webhook.requestHeader(webhook.payload(testAllowedEmail), header))
				if rec.Code != http.StatusUnauthorized {
					t.Errorf("unsupported webhook metadata accepted: %d", rec.Code)
				}
			})
		}
	}
}

func TestAuthJOSEDoesNotReserializeSignedHeaders(t *testing.T) {
	bearer, webhook := newBearerFixture(t), newWebhookFixture(t)
	claims, _ := json.Marshal(bearer.claims())
	header := []byte(`{"alg":"EdDSA","kid":"test-key","typ":"JWT"}`)
	// This replacement has the same decoded fields but was not signed.
	replacement := base64.RawURLEncoding.EncodeToString([]byte(`{ "alg":"EdDSA","kid":"test-key","t\u0079p":"JWT" }`))
	parts := strings.Split(bearer.rawToken(header, claims), ".")
	parts[0] = replacement
	if _, err := VerifyBearer(t.Context(), "Bearer "+strings.Join(parts, "."), bearer.config()); !errors.Is(err, ErrInvalidBearer) {
		t.Fatalf("changed bearer header accepted: %v", err)
	}
	header = []byte(`{"alg":"EdDSA","kid":"test-key","typ":"JWS"}`)
	replacement = base64.RawURLEncoding.EncodeToString([]byte(`{ "alg":"EdDSA","kid":"test-key","t\u0079p":"JWS" }`))
	req := webhook.requestHeader(webhook.payload(testAllowedEmail), header)
	parts = strings.Split(req.Header.Get("X-Neon-Signature"), ".")
	parts[0] = replacement
	req.Header.Set("X-Neon-Signature", strings.Join(parts, "."))
	rec := httptest.NewRecorder()
	AuthBeforeCreate(webhook.config()).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("changed webhook header accepted: %d", rec.Code)
	}
}

type jwksRoundTripper func(*http.Request) (*http.Response, error)

func (f jwksRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type authKeyConsumer struct {
	name                 string
	publicKey            ed25519.PublicKey
	success, unavailable int
	verify               func(HTTPDoer) int
}

// Exercise both production callers with the source derived from BaseURL.
func authKeyConsumers(t *testing.T) []authKeyConsumer {
	t.Helper()
	bearer, webhook := newBearerFixture(t), newWebhookFixture(t)
	token := bearer.token(t, bearer.claims())
	return []authKeyConsumer{
		{"bearer", bearer.privateKey.Public().(ed25519.PublicKey), http.StatusNoContent, http.StatusServiceUnavailable, func(client HTTPDoer) int {
			config := bearer.config()
			config.JWKSURL, config.Client = "", client
			req := httptest.NewRequest(http.MethodGet, "/api/private", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			RequireUserWithConfig(config, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				identity, ok := UserFromContext(r.Context())
				if !ok || identity.Subject != "user-test" || identity.Email != testAllowedEmail {
					t.Error("protected handler has no verified identity")
				}
				w.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(rec, req)
			return rec.Code
		}},
		{"webhook", webhook.privateKey.Public().(ed25519.PublicKey), http.StatusOK, http.StatusBadGateway, func(client HTTPDoer) int {
			config := webhook.config()
			config.JWKSURL, config.Client = "", client
			rec := httptest.NewRecorder()
			AuthBeforeCreate(config).ServeHTTP(rec, webhook.request(webhook.payload(testAllowedEmail)))
			if rec.Code == http.StatusOK && !strings.Contains(rec.Body.String(), `"allowed":true`) {
				t.Error("valid webhook did not allow the configured identity")
			}
			return rec.Code
		}},
	}
}

func authKeyDocument(publicKey ed25519.PublicKey) string {
	return fmt.Sprintf(`{"keys":[{"kty":"OKP","crv":"Ed25519","kid":%q,"x":%q,"alg":"EdDSA","use":"sig","key_ops":["verify"]}]}`, testKeyID, base64.RawURLEncoding.EncodeToString(publicKey))
}

func TestAuthJWKSRefusesRedirectsBeforeDestinationContact(t *testing.T) {
	const source = testIssuer + "/neondb/auth/.well-known/jwks.json"
	for _, consumer := range authKeyConsumers(t) {
		for _, target := range []struct{ name, location string }{
			{"same URL loop", source},
			{"same origin path", testIssuer + "/other/keys"},
			{"same path query", source + "?version=2"},
			{"cross origin HTTPS", "https://other.invalid/keys"},
			{"cross origin HTTP", "http://other.invalid/keys"},
			{"same origin downgrade", strings.Replace(source, "https:", "http:", 1)},
			{"absolute path", "/other/keys"},
			{"relative path", "../keys"},
			{"scheme relative", "//other.invalid/keys"},
			{"multi hop", "/next-hop"},
		} {
			// Includes every Go-followed status (301/302/303/307/308) and all
			// other 3xx responses, none of which may supply trusted keys.
			for status := 300; status < 400; status++ {
				for _, mode := range []string{"default", "injected default", "injected permissive"} {
					t.Run(fmt.Sprintf("%s/%s/%d/%s", consumer.name, target.name, status, mode), func(t *testing.T) {
						var contacts []string
						redirectChecks := 0
						transport := jwksRoundTripper(func(req *http.Request) (*http.Response, error) {
							contacts = append(contacts, req.URL.String())
							if len(contacts) == 1 || target.name == "same URL loop" || target.name == "multi hop" && len(contacts) == 2 {
								location := target.location
								if len(contacts) == 2 {
									location = "https://other.invalid/final-keys"
								}
								return &http.Response{StatusCode: status, Header: http.Header{"Location": {location}}, Body: io.NopCloser(strings.NewReader(authKeyDocument(consumer.publicKey))), Request: req}, nil
							}
							return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(authKeyDocument(consumer.publicKey))), Request: req}, nil
						})
						var client HTTPDoer
						if mode == "default" {
							previous := http.DefaultTransport
							http.DefaultTransport = transport
							t.Cleanup(func() { http.DefaultTransport = previous })
						} else {
							provided := &http.Client{Transport: transport, Timeout: 11 * time.Second}
							if mode == "injected permissive" {
								provided.CheckRedirect = func(*http.Request, []*http.Request) error {
									redirectChecks++
									if redirectChecks > 10 {
										return errors.New("test redirect loop limit")
									}
									return nil
								}
							}
							client = provided
						}
						if got := consumer.verify(client); got != consumer.unavailable {
							t.Errorf("status = %d, want unavailable %d", got, consumer.unavailable)
						}
						if len(contacts) != 1 || contacts[0] != source {
							t.Errorf("redirect destination contacted: %v", contacts)
						}
						if redirectChecks != 0 {
							t.Errorf("caller redirect policy overrode JWKS policy: %d calls", redirectChecks)
						}
					})
				}
			}
		}
	}
}

func TestAuthJWKSDirectResponsePreservesClientAndRotation(t *testing.T) {
	const source = testIssuer + "/neondb/auth/.well-known/jwks.json"
	for _, consumer := range authKeyConsumers(t) {
		for _, mode := range []string{"default", "injected"} {
			t.Run(consumer.name+"/"+mode, func(t *testing.T) {
				calls, redirectChecks := 0, 0
				wantTimeout := 3 * time.Second
				if mode == "injected" {
					wantTimeout = 11 * time.Second
				}
				transport := jwksRoundTripper(func(req *http.Request) (*http.Response, error) {
					calls++
					if req.URL.String() != source || req.Header.Get("Accept") != "application/json" {
						t.Errorf("unexpected request: %s, Accept=%q", req.URL, req.Header.Get("Accept"))
					}
					deadline, ok := req.Context().Deadline()
					if remaining := time.Until(deadline); !ok || remaining <= wantTimeout-time.Second || remaining > wantTimeout {
						t.Errorf("timeout not retained: deadline=%v, want %v", deadline, wantTimeout)
					}
					// Distinct key IDs, provider extras and declared verify use survive.
					body := strings.Replace(authKeyDocument(consumer.publicKey), `"keys":[`, `"provider":{"nullable":null},"keys":[{"kid":"previous-key","kty":"RSA","use":"enc"},`, 1)
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
				})
				var client HTTPDoer
				jar, err := cookiejar.New(nil)
				if err != nil {
					t.Fatal(err)
				}
				provided := &http.Client{Transport: transport, Timeout: wantTimeout, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
					redirectChecks++
					return nil
				}}
				if mode == "default" {
					previous := http.DefaultTransport
					http.DefaultTransport = transport
					t.Cleanup(func() { http.DefaultTransport = previous })
				} else {
					client = provided
				}
				if got := consumer.verify(client); got != consumer.success || calls != 1 {
					t.Fatalf("direct key rotation rejected: status=%d calls=%d", got, calls)
				}
				if provided.Timeout != wantTimeout || provided.Jar != jar || provided.Transport == nil {
					t.Fatal("caller client settings changed")
				}
				if err := provided.CheckRedirect(nil, nil); err != nil || redirectChecks != 1 {
					t.Fatal("caller redirect policy was mutated")
				}
			})
		}
	}
}

func TestAuthJWKSChecksReportedFinalURL(t *testing.T) {
	const source = testIssuer + "/neondb/auth/.well-known/jwks.json"
	for _, consumer := range authKeyConsumers(t) {
		for _, target := range []string{"omitted request", "nil URL", source, source + "?other", testIssuer + "/other", "https://other.invalid/keys", "http://other.invalid/keys"} {
			t.Run(consumer.name+"/"+target, func(t *testing.T) {
				client := httpDoerFunc(func(*http.Request) (*http.Response, error) {
					response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(authKeyDocument(consumer.publicKey)))}
					if target != "omitted request" {
						response.Request = &http.Request{}
						if target != "nil URL" {
							response.Request.URL, _ = url.Parse(target)
						}
					}
					return response, nil
				})
				want := consumer.unavailable
				if target == source || target == "omitted request" {
					want = consumer.success
				}
				if got := consumer.verify(client); got != want {
					t.Fatalf("status=%d, want %d", got, want)
				}
			})
		}
	}
}

func TestAuthJWKSDirectResponseRetainsKeyAndBodyRestrictions(t *testing.T) {
	for _, consumer := range authKeyConsumers(t) {
		valid := authKeyDocument(consumer.publicKey)
		key := strings.TrimSuffix(strings.TrimPrefix(valid, `{"keys":[`), `]}`)
		for _, tc := range []struct {
			name, body string
			accepted   bool
		}{
			{"at size limit", valid + strings.Repeat(" ", int(defaultJWKSBodyMax)-len(valid)), true},
			{"over size limit", valid + strings.Repeat(" ", int(defaultJWKSBodyMax)+1-len(valid)), false},
			{"duplicate selected key", `{"keys":[` + key + `,` + key + `]}`, false},
			{"wrong algorithm", strings.Replace(valid, `"alg":"EdDSA"`, `"alg":"RS256"`, 1), false},
			{"encryption use", strings.Replace(valid, `"use":"sig"`, `"use":"enc"`, 1), false},
			{"signing operation", strings.Replace(valid, `"key_ops":["verify"]`, `"key_ops":["sign","verify"]`, 1), false},
		} {
			t.Run(consumer.name+"/"+tc.name, func(t *testing.T) {
				client := &http.Client{Transport: jwksRoundTripper(func(req *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(tc.body)), Request: req}, nil
				})}
				want := consumer.unavailable
				if tc.accepted {
					want = consumer.success
				}
				if got := consumer.verify(client); got != want {
					t.Fatalf("status=%d, want %d", got, want)
				}
			})
		}
	}
}

func TestJWKSInjectedClientPreservesTLS(t *testing.T) {
	fixture := newBearerFixture(t)
	publicKey := fixture.privateKey.Public().(ed25519.PublicKey)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil {
			t.Error("key request did not use TLS")
		}
		_, _ = io.WriteString(w, authKeyDocument(publicKey))
	}))
	defer server.Close()
	client := server.Client()
	transport := client.Transport.(*http.Transport)
	tlsConfig := transport.TLSClientConfig
	client.Timeout = 11 * time.Second
	key, err := fetchEd25519PublicKey(t.Context(), client, server.URL, testKeyID)
	if err != nil || !bytes.Equal(key, publicKey) {
		t.Fatalf("client TLS settings were not retained: %v", err)
	}
	if client.Transport != transport || transport.TLSClientConfig != tlsConfig || client.Timeout != 11*time.Second || client.CheckRedirect != nil {
		t.Fatal("caller TLS/transport, timeout or redirect settings were mutated")
	}
}

func TestJWKSConcurrentSharedClientIsNotMutated(t *testing.T) {
	fixture := newBearerFixture(t)
	publicKey := fixture.privateKey.Public().(ed25519.PublicKey)
	var calls atomic.Int32
	client := &http.Client{Transport: jwksRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(authKeyDocument(publicKey))), Request: req}, nil
	})}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			key, err := fetchEd25519PublicKey(t.Context(), client, "https://keys.invalid/keys", testKeyID)
			if err != nil || !bytes.Equal(key, publicKey) {
				t.Errorf("concurrent direct fetch failed: %v", err)
			}
			if client.CheckRedirect != nil {
				t.Error("shared caller redirect policy was mutated")
			}
		})
	}
	wg.Wait()
	if calls.Load() != 16 {
		t.Fatalf("transport calls = %d, want 16", calls.Load())
	}
}

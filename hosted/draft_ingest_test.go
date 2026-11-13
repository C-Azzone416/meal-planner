package hosted

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const syntheticDraftToken = "mp_lc1_pv_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE"
const syntheticDraftBody = `{"content_schema_version":1,"content":{"title":"Synthetic incomplete"}}`

type draftRepoFake struct {
	authErr, errorSubmit   error
	access                 DraftAccess
	submission             DraftSubmission
	key                    string
	authCalls, submitCalls int
	replay                 bool
}

func (f *draftRepoFake) Authenticate(_ context.Context, a DraftAccess) error {
	f.authCalls++
	f.access = a
	return f.authErr
}
func (f *draftRepoFake) Submit(_ context.Context, a DraftAccess, key string, s DraftSubmission) (DraftResponse, bool, error) {
	f.submitCalls++
	f.access = a
	f.key = key
	f.submission = s
	if f.errorSubmit != nil {
		return DraftResponse{}, false, f.errorSubmit
	}
	response, err := newDraftResponse(RecipeOwner{Issuer: a.Issuer, Subject: "synthetic-owner"}, s, time.Date(2026, 11, 13, 1, 0, 0, 0, time.UTC))
	return response, f.replay, err
}
func draftTestConfig() DraftIngestConfig {
	return DraftIngestConfig{Enabled: true, Environment: "preview", Issuer: "https://auth.preview.example.invalid", Origin: "https://athome-meals-dev.vercel.app", AllowedEmails: []string{"synthetic@example.invalid"}}
}
func draftRequest(body string) *http.Request {
	r := httptest.NewRequest("POST", "/api/draft-submissions", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+syntheticDraftToken)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "synthetic-operation-01")
	return r
}
func TestDraftIngestPreservesUnreviewedIncompleteContent(t *testing.T) {
	for _, name := range []string{"full", "incomplete"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "contracts", "testdata", "fixtures", "hosted-drafts-v1", name+".submit.json"))
			if err != nil {
				t.Fatal(err)
			}
			f := &draftRepoFake{}
			w := httptest.NewRecorder()
			DraftIngestHandler(f, draftTestConfig()).ServeHTTP(w, draftRequest(string(raw)))
			if w.Code != 201 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			var response DraftResponse
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Draft.Status != "draft" || response.Draft.Origin != "agent" || response.Draft.ApprovedRecipeID != nil || response.Draft.Review.State != "not_started" || response.Draft.Review.Candidate != nil {
				t.Fatal("not unreviewed")
			}
			if !reflect.DeepEqual(response.Draft.Content, f.submission.Content) || !reflect.DeepEqual(response.Receipt.SubmissionContent, f.submission.Content) {
				t.Fatal("lossy snapshot")
			}
			if f.access.TokenHash != sha256.Sum256([]byte(syntheticDraftToken)) || f.key != "synthetic-operation-01" {
				t.Fatal("invalid access")
			}
			if strings.Contains(w.Body.String(), syntheticDraftToken) || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("response credential/cache/CORS leak")
			}
		})
	}
}
func TestDraftIngestFailClosed(t *testing.T) {
	cases := []struct {
		name              string
		mutate            func(*http.Request, *DraftIngestConfig, *draftRepoFake)
		status, authCalls int
	}{
		{"disabled", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) { c.Enabled = false }, 404, 0},
		{"production", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) { c.Environment = "production" }, 404, 0},
		{"missing issuer", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) { c.Issuer = "" }, 404, 0},
		{"missing membership", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) { c.AllowedEmails = nil }, 404, 0},
		{"bad origin config", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) { c.Origin = "https://attacker.invalid" }, 404, 0},
		{"get", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) { r.Method = "GET" }, 405, 0},
		{"preflight", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) { r.Method = "OPTIONS" }, 405, 0},
		{"approval", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) { r.Method = "PUT" }, 405, 0},
		{"query", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) { r.URL.RawQuery = "owner=attacker" }, 400, 0},
		{"empty query", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) { r.URL.ForceQuery = true }, 400, 0},
		{"wrong origin", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) {
			r.Header.Set("Origin", "https://attacker.invalid")
		}, 403, 0},
		{"null origin", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) { r.Header.Set("Origin", "null") }, 403, 0},
		{"multiple origins", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) {
			r.Header["Origin"] = []string{c.Origin, c.Origin}
		}, 403, 0},
		{"no bearer", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) { r.Header.Del("Authorization") }, 401, 0},
		{"cookie only", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) {
			r.Header.Del("Authorization")
			r.Header.Set("Cookie", "session=synthetic")
		}, 401, 0},
		{"JWT", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) {
			r.Header.Set("Authorization", "Bearer eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJhIn0.signature")
		}, 401, 0},
		{"duplicate bearer", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) {
			r.Header.Add("Authorization", "Bearer "+syntheticDraftToken)
		}, 401, 0},
		{"combined bearer", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) {
			r.Header.Set("Authorization", "Bearer "+syntheticDraftToken+", Bearer "+syntheticDraftToken)
		}, 401, 0},
		{"wrong token env", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) {
			r.Header.Set("Authorization", strings.Replace(r.Header.Get("Authorization"), "_pv_", "_prod_", 1))
		}, 401, 0},
		{"expired/revoked", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) { f.authErr = ErrDraftUnauthorized }, 401, 1},
		{"auth database failure", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) {
			f.authErr = errors.New("secret database failure")
		}, 503, 1},
		{"missing content type", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) { r.Header.Del("Content-Type") }, 415, 1},
		{"wrong content type", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) {
			r.Header.Set("Content-Type", "text/plain")
		}, 415, 1},
		{"duplicate content type", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) {
			r.Header.Add("Content-Type", "application/json")
		}, 415, 1},
		{"compressed", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) {
			r.Header.Set("Content-Encoding", "gzip")
		}, 415, 1},
		{"missing key", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) { r.Header.Del("Idempotency-Key") }, 400, 1},
		{"duplicate key header", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) {
			r.Header.Add("Idempotency-Key", "different-operation")
		}, 400, 1},
		{"large key", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) {
			r.Header.Set("Idempotency-Key", strings.Repeat("a", 129))
		}, 400, 1},
		{"unicode key", func(r *http.Request, c *DraftIngestConfig, f *draftRepoFake) {
			r.Header.Set("Idempotency-Key", "operation-caf\u00e9-0001")
		}, 400, 1},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r := draftRequest(syntheticDraftBody)
			c := draftTestConfig()
			f := &draftRepoFake{}
			tt.mutate(r, &c, f)
			w := httptest.NewRecorder()
			DraftIngestHandler(f, c).ServeHTTP(w, r)
			if w.Code != tt.status || f.authCalls != tt.authCalls || f.submitCalls != 0 {
				t.Fatalf("status=%d auth=%d submit=%d body=%s", w.Code, f.authCalls, f.submitCalls, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "secret database") {
				t.Fatal("error leak")
			}
		})
	}
}
func TestDraftIngestBadBodiesAndNoWrites(t *testing.T) {
	bad := []string{`null`, `[]`, `{}`, `{"content_schema_version":null,"content":{}}`, `{"content_schema_version":1.0,"content":{}}`, `{"content_schema_version":1,"content":null}`, `{"content_schema_version":1,"content":{},"owner":"injected"}`, `{"content_schema_version":1,"Content":{}}`, `{"content_schema_version":1,"content":{"title":"a","title":"b"}}`, `{"content_schema_version":1,"content":{"title":"a","approved":true}}`, `{"content_schema_version":1,"content":{"title":"\ud800"}}`, syntheticDraftBody + syntheticDraftBody, strings.Repeat(" ", 262145)}
	paths, _ := filepath.Glob(filepath.Join("..", "contracts", "testdata", "fixtures", "hosted-drafts-v1", "rejected", "*.json"))
	for _, path := range paths {
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		bad = append(bad, string(b))
	}
	for _, body := range bad {
		f := &draftRepoFake{}
		w := httptest.NewRecorder()
		DraftIngestHandler(f, draftTestConfig()).ServeHTTP(w, draftRequest(body))
		if w.Code < 400 || f.submitCalls != 0 {
			t.Fatalf("accepted bad body, code %d", w.Code)
		}
	}
	f := &draftRepoFake{}
	w := httptest.NewRecorder()
	DraftIngestHandler(f, draftTestConfig()).ServeHTTP(w, draftRequest(`{"content_schema_version":2,"content":{}}`))
	if w.Code != 422 || f.submitCalls != 0 {
		t.Fatal("unsupported schema was not locked out")
	}
}
func TestDraftIngestStoreResults(t *testing.T) {
	for _, tt := range []struct {
		err    error
		replay bool
		status int
	}{{nil, true, 200}, {ErrDraftUnauthorized, false, 401}, {ErrDraftConflict, false, 409}, {ErrDraftQuota, false, 429}, {errors.New("postgres://synthetic-secret"), false, 503}} {
		f := &draftRepoFake{errorSubmit: tt.err, replay: tt.replay}
		w := httptest.NewRecorder()
		DraftIngestHandler(f, draftTestConfig()).ServeHTTP(w, draftRequest(syntheticDraftBody))
		if w.Code != tt.status || strings.Contains(w.Body.String(), "synthetic-secret") {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
}
func TestDraftIngestEnvironmentLocked(t *testing.T) {
	t.Setenv("NEON_AUTH_BASE_URL", "https://ep-synthetic.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth")
	t.Setenv("NEON_AUTH_ENDPOINT_ID", "ep-synthetic")
	t.Setenv("AUTH_ALLOWED_EMAIL", "")
	t.Setenv("AUTH_ALLOWED_EMAILS", `["synthetic@example.invalid"]`)
	t.Setenv("MEAL_DRAFT_INGEST_ENABLED", "true")
	t.Setenv("MEAL_DRAFT_INGEST_ORIGIN", "https://athome-meals-dev.vercel.app")
	for _, env := range []string{"production", "development", "", "Preview"} {
		t.Setenv("VERCEL_ENV", env)
		if DraftIngestConfigFromEnvironment().Enabled {
			t.Fatal("enabled outside preview")
		}
	}
	t.Setenv("VERCEL_ENV", "preview")
	if !DraftIngestConfigFromEnvironment().Enabled {
		t.Fatal("valid Preview config rejected")
	}
	t.Setenv("AUTH_ALLOWED_EMAILS", `[]`)
	if DraftIngestConfigFromEnvironment().Enabled {
		t.Fatal("empty membership accepted")
	}
}

func TestDraftGrantCannotAuthorizeOwnerAPI(t *testing.T) {
	called := false
	config := BearerAuthConfig{BaseURL: "https://ep-synthetic.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth", ExpectedEndpoint: "ep-synthetic", AllowedEmail: "owner@example.invalid"}
	handler := RequireUserWithConfig(config, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, draftRequest(syntheticDraftBody))
	if called || w.Code != 401 {
		t.Fatal("scoped submission token authorized owner API")
	}
}

package hosted

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

type workflowFake struct {
	intent     DraftIntent
	draft      DraftResponse
	lastAccess DraftAccess
	lastOwner  RecipeOwner
	reviewErr  error
}

func (f *workflowFake) CreateIntent(_ context.Context, a DraftAccess, _ string, s DraftSubmission) (DraftIntent, bool, error) {
	f.lastAccess = a
	f.intent.Submission = s
	return f.intent, false, nil
}
func (f *workflowFake) PollIntent(_ context.Context, a DraftAccess, _ string) (DraftIntent, error) {
	f.lastAccess = a
	return f.intent, nil
}
func (f *workflowFake) ListIntents(_ context.Context, o RecipeOwner) ([]DraftIntent, error) {
	f.lastOwner = o
	return []DraftIntent{f.intent}, nil
}
func (f *workflowFake) GetIntent(_ context.Context, o RecipeOwner, _ string) (DraftIntent, error) {
	f.lastOwner = o
	return f.intent, nil
}
func (f *workflowFake) DecideIntent(_ context.Context, o RecipeOwner, _ string, _ int64, _ string, _ bool, a DraftAccess) (DraftIntent, error) {
	f.lastOwner = o
	f.lastAccess = a
	return f.intent, nil
}
func (f *workflowFake) ListHostedDrafts(_ context.Context, o RecipeOwner) ([]HostedDraftSummary, error) {
	f.lastOwner = o
	return []HostedDraftSummary{}, nil
}
func (f *workflowFake) GetHostedDraft(_ context.Context, o RecipeOwner, _ string) (DraftResponse, error) {
	f.lastOwner = o
	return f.draft, nil
}
func (f *workflowFake) ReviewHostedDraft(_ context.Context, o RecipeOwner, _ string, _, _ int64) (DraftResponse, error) {
	f.lastOwner = o
	return f.draft, f.reviewErr
}
func (f *workflowFake) MarkHostedDraftApproved(_ context.Context, o RecipeOwner, _ string, _, _ int64, _ string) (DraftResponse, error) {
	f.lastOwner = o
	return f.draft, f.reviewErr
}

func testWorkflowConfig() DraftWorkflowConfig {
	return DraftWorkflowConfig{Enabled: true, Environment: "preview", Issuer: "https://auth.example.invalid", Origin: "https://athome-meals-dev.vercel.app", MembershipEpoch: "synthetic_epoch_1", AllowedEmails: []string{"owner@example.invalid"}}
}
func testGrant() string {
	return "mp_lc1_pv_" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
}
func testIntent() DraftIntent {
	return DraftIntent{ID: "11111111-1111-4111-8111-111111111111", Revision: 1, State: "pending", ContentHash: strings.Repeat("a", 64), ExpiresAt: time.Now().Add(time.Minute)}
}

func TestDraftWorkflowSourceGateCannotBeOpenedByEnvironment(t *testing.T) {
	t.Setenv("VERCEL_ENV", "preview")
	t.Setenv("MEAL_DRAFT_WORKFLOW_ENABLED", "true")
	t.Setenv("MEAL_DRAFT_INGEST_ORIGIN", "https://athome-meals-dev.vercel.app")
	if got := DraftWorkflowConfigFromEnvironment(); got.Enabled {
		t.Fatal("environment opened the closed source gate")
	}
}

func TestDraftIntentCreateIsProposalOnlyAndUsesProposeScope(t *testing.T) {
	f := &workflowFake{intent: testIntent()}
	body := `{"content_schema_version":1,"content":{"title":"Unknown stays unknown","ingredients":[],"instructions":[],"sources":[],"guidance":[]}}`
	r := httptest.NewRequest(http.MethodPost, "/api/draft-send-intents", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+testGrant())
	r.Header.Set("Idempotency-Key", "1234567890abcdef")
	w := httptest.NewRecorder()
	DraftIntentCreateHandler(f, testWorkflowConfig()).ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if f.lastAccess.RequiredScope != "draft:propose" {
		t.Fatalf("scope %q", f.lastAccess.RequiredScope)
	}
	if strings.Contains(w.Body.String(), "Unknown stays unknown") || strings.Contains(w.Body.String(), "submission") {
		t.Fatal("proposal content leaked in grant response")
	}
	if !strings.Contains(w.Body.String(), `"api_version":"draft-intent-v1"`) || !strings.Contains(w.Body.String(), `"path":"/drafts/incoming/11111111-1111-4111-8111-111111111111"`) {
		t.Fatal("locked interface missing")
	}
}
func TestDraftIntentPollPendingIs202AndSecretFree(t *testing.T) {
	f := &workflowFake{intent: testIntent()}
	r := httptest.NewRequest(http.MethodPost, "/api/draft-send-intents/poll", strings.NewReader(`{"intent_id":"11111111-1111-4111-8111-111111111111"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+testGrant())
	w := httptest.NewRecorder()
	DraftIntentPollHandler(f, testWorkflowConfig()).ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatalf("status %d", w.Code)
	}
	if strings.Contains(w.Body.String(), testGrant()) {
		t.Fatal("grant leaked")
	}
}
func TestOwnerDraftRequiresVerifiedIdentityBeforeLookup(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/draft-review/drafts?id=11111111-1111-4111-8111-111111111111", nil)
	OwnerDraftHandler(&workflowFake{}, nil, testWorkflowConfig()).ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("status %d", w.Code)
	}
}
func TestOwnerReviewBindsVerifiedOwnerAndRevision(t *testing.T) {
	f := &workflowFake{draft: DraftResponse{Draft: HostedDraft{Revision: 1, Review: DraftReview{Revision: 2}}}}
	r := httptest.NewRequest(http.MethodPost, "/api/draft-review/drafts", strings.NewReader(`{"draft_id":"11111111-1111-4111-8111-111111111111","expected_draft_revision":1,"expected_review_revision":2}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://athome-meals-dev.vercel.app")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r = r.WithContext(context.WithValue(r.Context(), bearerContextKey{}, UserIdentity{Issuer: "issuer", Subject: "owner"}))
	w := httptest.NewRecorder()
	OwnerDraftHandler(f, nil, testWorkflowConfig()).ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if f.lastOwner.Subject != "owner" {
		t.Fatal("owner not derived from verified context")
	}
}
func TestOwnerMutationRejectsMissingCSRFHeaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/draft-review/drafts", strings.NewReader(`{"draft_id":"11111111-1111-4111-8111-111111111111","expected_draft_revision":1,"expected_review_revision":1}`))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(context.WithValue(r.Context(), bearerContextKey{}, UserIdentity{Issuer: "issuer", Subject: "owner"}))
	w := httptest.NewRecorder()
	OwnerDraftHandler(&workflowFake{}, nil, testWorkflowConfig()).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("status %d", w.Code)
	}
}
func TestCanonicalUnknownAndExplicitZeroRemainDistinct(t *testing.T) {
	zero := 0
	c := draft.Content{Title: "x", Ingredients: []draft.Ingredient{}, Instructions: []draft.Step{}, Sources: []draft.Source{}, Guidance: []draft.Guidance{}, Estimates: &draft.Estimates{ActiveMinutes: &draft.MinutesEstimate{Value: &zero, Basis: &draft.Basis{Kind: draft.BasisAgentEstimate}}}}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"value":0`)) {
		t.Fatal("explicit zero lost")
	}
	c.Estimates = nil
	b, err = json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("active_minutes")) {
		t.Fatal("unknown estimate was fabricated")
	}
}

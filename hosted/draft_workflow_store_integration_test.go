//go:build integration

package hosted

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func workflowTestStore(t *testing.T) (*PostgresDraftWorkflowStore, DraftAccess) {
	store, access, _ := workflowTestStoreWithDSN(t)
	return store, access
}

func workflowTestStoreWithDSN(t *testing.T) (*PostgresDraftWorkflowStore, DraftAccess, string) {
	t.Helper()
	recipes, dsn := recipeTestDatabase(t)
	for _, name := range []string{"004_draft_ingest.sql", "005_draft_pairing.sql", "006_draft_review_workflow.sql"} {
		migration, err := os.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = recipes.db.Exec(string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	pairings := &PostgresDraftPairingStore{db: recipes.db}
	config := testPairingConfig()
	created, verifier := createApprovedPairing(t, pairings, config)
	approved, err := pairings.PollPairing(context.Background(), config, DraftPairingPollRequest{PairingID: created.PairingID, PollToken: created.PollToken, CodeVerifier: verifier})
	if err != nil {
		t.Fatal(err)
	}
	access := DraftAccess{TokenHash: sha256.Sum256([]byte(approved.Grant)), Environment: config.Environment, Issuer: config.Issuer, AllowedEmails: []string{"owner@example.test"}, RequiredScope: "draft:propose", MembershipEpoch: config.MembershipEpoch}
	return NewPostgresDraftWorkflowStore(NewPostgresDraftStore(recipes.db)), access, dsn
}

func addWorkflowTestOwner(t *testing.T, store *PostgresDraftWorkflowStore, config DraftPairingConfig, subject, email, verifier string) DraftAccess {
	t.Helper()
	pairings := &PostgresDraftPairingStore{db: store.drafts.db}
	challenge := sha256.Sum256([]byte(verifier))
	created, err := pairings.CreatePairing(context.Background(), config, DraftPairingCreateRequest{
		CodeChallenge:       base64.RawURLEncoding.EncodeToString(challenge[:]),
		CodeChallengeMethod: "S256",
		ClientLabel:         "Synthetic isolated account",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = pairings.ApprovePairing(context.Background(), config, UserIdentity{Issuer: config.Issuer, Subject: subject, Email: email}, DraftPairingApproveRequest{UserCode: created.UserCode, ChallengeFingerprint: created.ChallengeFingerprint}); err != nil {
		t.Fatal(err)
	}
	approved, err := pairings.PollPairing(context.Background(), config, DraftPairingPollRequest{PairingID: created.PairingID, PollToken: created.PollToken, CodeVerifier: verifier})
	if err != nil {
		t.Fatal(err)
	}
	return DraftAccess{
		TokenHash:       sha256.Sum256([]byte(approved.Grant)),
		Environment:     config.Environment,
		Issuer:          config.Issuer,
		AllowedEmails:   []string{email},
		RequiredScope:   "draft:propose",
		MembershipEpoch: config.MembershipEpoch,
	}
}

func TestDraftWorkflowPostgresOwnerIsolationAcrossIntentReviewAndSave(t *testing.T) {
	store, accessA := workflowTestStore(t)
	ctx := context.Background()
	config := testPairingConfig()
	ownerA := RecipeOwner{Issuer: accessA.Issuer, Subject: "owner-a"}
	ownerB := RecipeOwner{Issuer: accessA.Issuer, Subject: "owner-b"}
	accessB := addWorkflowTestOwner(t, store, config, ownerB.Subject, "owner-b@example.test", stringsRepeat("b", 43))

	intentA, replayed, err := store.CreateIntent(ctx, accessA, "synthetic-owner-isolation", syntheticDraftSubmission())
	if err != nil || replayed {
		t.Fatalf("create A intent: replay=%v err=%v", replayed, err)
	}
	missingID := "99999999-9999-4999-8999-999999999999"
	if _, err = store.PollIntent(ctx, accessB, intentA.ID); !errors.Is(err, ErrDraftIntentNotFound) {
		t.Fatalf("B polled A intent: %v", err)
	}
	if _, err = store.PollIntent(ctx, accessB, missingID); !errors.Is(err, ErrDraftIntentNotFound) {
		t.Fatalf("missing poll was distinguishable: %v", err)
	}
	if items, listErr := store.ListIntents(ctx, ownerB); listErr != nil || len(items) != 0 {
		t.Fatalf("B listed A intent: items=%d err=%v", len(items), listErr)
	}
	if _, err = store.GetIntent(ctx, ownerB, intentA.ID); !errors.Is(err, ErrDraftIntentNotFound) {
		t.Fatalf("B read A intent: %v", err)
	}
	if _, err = store.DecideIntent(ctx, ownerB, intentA.ID, intentA.Revision, intentA.ContentHash, true, accessB); !errors.Is(err, ErrDraftIntentNotFound) {
		t.Fatalf("B decided A intent: %v", err)
	}

	accepted, err := store.DecideIntent(ctx, ownerA, intentA.ID, intentA.Revision, intentA.ContentHash, true, accessA)
	if err != nil || accepted.DraftID == nil {
		t.Fatalf("A approve: %#v %v", accepted, err)
	}
	draftID := *accepted.DraftID
	draftABefore, err := store.GetHostedDraft(ctx, ownerA, draftID)
	if err != nil {
		t.Fatal(err)
	}
	if items, listErr := store.ListHostedDrafts(ctx, ownerB); listErr != nil || len(items) != 0 {
		t.Fatalf("B listed A draft: items=%d err=%v", len(items), listErr)
	}
	if _, err = store.GetHostedDraft(ctx, ownerB, draftID); !errors.Is(err, ErrRecipeNotFound) {
		t.Fatalf("B read A draft: %v", err)
	}
	if _, err = store.ReviewHostedDraft(ctx, ownerB, draftID, 1, 1); !errors.Is(err, ErrRecipeNotFound) {
		t.Fatalf("B reviewed A draft: %v", err)
	}
	if _, err = store.MarkHostedDraftApproved(ctx, ownerB, draftID, 1, 1, "88888888-8888-4888-8888-888888888888"); !errors.Is(err, ErrRecipeNotFound) {
		t.Fatalf("B linked A draft: %v", err)
	}

	projection, err := json.Marshal(syntheticRecipeContent())
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"draft_id":                 draftID,
		"expected_draft_revision":  1,
		"expected_review_revision": 1,
		"projection":               json.RawMessage(projection),
	})
	if err != nil {
		t.Fatal(err)
	}
	workflowConfig := DraftWorkflowConfig{Enabled: true, Environment: "preview", Issuer: accessA.Issuer, Origin: draftPreviewOrigin, MembershipEpoch: accessA.MembershipEpoch, AllowedEmails: []string{"owner@example.test", "owner-b@example.test"}}
	request := httptest.NewRequest(http.MethodPost, "/api/draft-review/drafts", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", draftPreviewOrigin)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request = request.WithContext(context.WithValue(request.Context(), bearerContextKey{}, UserIdentity{Issuer: ownerB.Issuer, Subject: ownerB.Subject, Email: "owner-b@example.test"}))
	response := httptest.NewRecorder()
	recipes := NewPostgresRecipeStore(store.drafts.db)
	OwnerDraftHandler(store, recipes, workflowConfig).ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"not_found"`) {
		t.Fatalf("B save response: %d %s", response.Code, response.Body.String())
	}
	if items, _, listErr := recipes.List(ctx, ownerB, "", 100); listErr != nil || len(items) != 0 {
		t.Fatalf("B save created recipe: items=%d err=%v", len(items), listErr)
	}
	var recipeRows int
	if err = store.drafts.db.QueryRow(`SELECT count(*) FROM meal_planner.recipes`).Scan(&recipeRows); err != nil || recipeRows != 0 {
		t.Fatalf("B save created an orphan or cross-owner recipe: rows=%d err=%v", recipeRows, err)
	}

	draftA, err := store.GetHostedDraft(ctx, ownerA, draftID)
	if err != nil || !reflect.DeepEqual(draftA, draftABefore) || draftA.Draft.Review.State != "not_started" || draftA.Draft.ApprovedRecipeID != nil {
		t.Fatalf("A draft changed by B: state=%q recipe=%v err=%v", draftA.Draft.Review.State, draftA.Draft.ApprovedRecipeID, err)
	}
}

func TestDraftWorkflowConcurrentApproveDenyIsAtomic(t *testing.T) {
	store, access := workflowTestStore(t)
	owner := RecipeOwner{Issuer: access.Issuer, Subject: "owner-a"}
	intent, replay, err := store.CreateIntent(context.Background(), access, "synthetic-intent-operation", syntheticDraftSubmission())
	if err != nil || replay {
		t.Fatalf("create intent: replay=%v err=%v", replay, err)
	}
	var retained, required int64
	if err = store.drafts.db.QueryRow(`SELECT retained_bytes,octet_length(submission::text)+octet_length(operation_key)+octet_length(environment)+octet_length(owner_issuer)+octet_length(owner_subject)+64+4096 FROM public.draft_send_intents WHERE id=$1`, intent.ID).Scan(&retained, &required); err != nil {
		t.Fatal(err)
	}
	if retained != required {
		t.Fatalf("retained bytes = %d, canonical requirement = %d", retained, required)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, approve := range []bool{true, false} {
		wg.Add(1)
		go func(approve bool) {
			defer wg.Done()
			<-start
			_, decideErr := store.DecideIntent(context.Background(), owner, intent.ID, intent.Revision, intent.ContentHash, approve, access)
			errs <- decideErr
		}(approve)
	}
	close(start)
	wg.Wait()
	close(errs)
	successes := 0
	for decideErr := range errs {
		if decideErr == nil {
			successes++
		} else if !errors.Is(decideErr, ErrDraftRevision) {
			t.Fatalf("unexpected decision error: %v", decideErr)
		}
	}
	if successes != 1 {
		t.Fatalf("successful terminal decisions = %d", successes)
	}
	final, err := store.GetIntent(context.Background(), owner, intent.ID)
	if err != nil {
		t.Fatal(err)
	}
	var submissions int
	if err = store.drafts.db.QueryRow(`SELECT count(*) FROM public.draft_ingest_submissions WHERE owner_issuer=$1 AND owner_subject=$2`, owner.Issuer, owner.Subject).Scan(&submissions); err != nil {
		t.Fatal(err)
	}
	if (final.State == "accepted" && submissions != 1) || (final.State == "denied" && submissions != 0) {
		t.Fatalf("intent state %q with %d hosted submissions", final.State, submissions)
	}
}

func TestDraftWorkflowApprovalTransitionFailureRollsBackHostedDraft(t *testing.T) {
	store, access := workflowTestStore(t)
	owner := RecipeOwner{Issuer: access.Issuer, Subject: "owner-a"}
	intent, _, err := store.CreateIntent(context.Background(), access, "synthetic-transition-failure", syntheticDraftSubmission())
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.drafts.db.Exec(`CREATE FUNCTION public.reject_intent_accept() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='accepted' THEN RAISE EXCEPTION 'synthetic transition failure'; END IF; RETURN NEW; END $$;
CREATE TRIGGER reject_intent_accept BEFORE UPDATE ON public.draft_send_intents FOR EACH ROW EXECUTE FUNCTION public.reject_intent_accept()`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DecideIntent(context.Background(), owner, intent.ID, intent.Revision, intent.ContentHash, true, access); !errors.Is(err, ErrDraftUnavailable) {
		t.Fatalf("approval transition failure = %v", err)
	}
	final, err := store.GetIntent(context.Background(), owner, intent.ID)
	if err != nil || final.State != "pending" {
		t.Fatalf("intent after rollback = %q, %v", final.State, err)
	}
	var submissions int
	if err = store.drafts.db.QueryRow(`SELECT count(*) FROM public.draft_ingest_submissions WHERE owner_issuer=$1 AND owner_subject=$2`, owner.Issuer, owner.Subject).Scan(&submissions); err != nil {
		t.Fatal(err)
	}
	if submissions != 0 {
		t.Fatalf("hosted drafts committed despite failed intent transition: %d", submissions)
	}
}

func TestDraftWorkflowReplayRejectsUnsupportedStoredSchema(t *testing.T) {
	store, access := workflowTestStore(t)
	owner := RecipeOwner{Issuer: access.Issuer, Subject: "owner-a"}
	submission := syntheticDraftSubmission()
	intent, _, err := store.CreateIntent(context.Background(), access, "synthetic-unsupported-replay", submission)
	if err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(submission)
	if err != nil {
		t.Fatal(err)
	}
	requestHash := sha256.Sum256(request)
	future := `{"api_version":"future","draft":{"schema_version":2,"content_schema_version":2,"id":"44444444-4444-4444-8444-444444444444"}}`
	_, err = store.drafts.db.Exec(`INSERT INTO public.draft_ingest_submissions(environment,owner_issuer,owner_subject,operation_key,request_hash,originating_grant_hash,request,response,retained_bytes,created_at) VALUES('preview',$1,$2,$3,$4,$5,$6,$7,1000000,clock_timestamp())`, owner.Issuer, owner.Subject, intent.ID, requestHash[:], access.TokenHash[:], string(request), future)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DecideIntent(context.Background(), owner, intent.ID, intent.Revision, intent.ContentHash, true, access); !errors.Is(err, ErrDraftUnsupported) {
		t.Fatalf("unsupported replay approval = %v", err)
	}
	final, err := store.GetIntent(context.Background(), owner, intent.ID)
	if err != nil || final.State != "pending" {
		t.Fatalf("unsupported replay changed intent to %q: %v", final.State, err)
	}
}

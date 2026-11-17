//go:build integration

package hosted

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/lib/pq"
)

func restrictedDraftRuntime(t *testing.T, admin *PostgresDraftWorkflowStore, dsn string) (*PostgresDraftWorkflowStore, *PostgresDraftPairingStore, string) {
	t.Helper()
	id, err := newRecipeUUID()
	if err != nil {
		t.Fatal(err)
	}
	role := "draft_runtime_test_" + strings.ReplaceAll(id, "-", "")
	quotedRole := pq.QuoteIdentifier(role)
	if _, err = admin.drafts.db.Exec(`CREATE ROLE ` + quotedRole + ` NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, cleanupErr := admin.drafts.db.Exec(`DROP OWNED BY ` + quotedRole); cleanupErr != nil {
			t.Error(cleanupErr)
		}
		if _, cleanupErr := admin.drafts.db.Exec(`DROP ROLE ` + quotedRole); cleanupErr != nil {
			t.Error(cleanupErr)
		}
	})
	if _, err = admin.drafts.db.Exec(`
CREATE SCHEMA auth;
CREATE TABLE auth.synthetic_accounts (id integer PRIMARY KEY);
CREATE TABLE public.synthetic_private_data (id integer PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	grantTemplate, err := os.ReadFile("../tools/deploy/draft-runtime-grants.sql")
	if err != nil {
		t.Fatal(err)
	}
	const marker = `:"runtime_role"`
	if strings.Count(string(grantTemplate), marker) < 2 {
		t.Fatal("draft runtime grant policy lost its role marker")
	}
	grantSQL := strings.ReplaceAll(string(grantTemplate), marker, quotedRole)
	if strings.Contains(grantSQL, marker) {
		t.Fatal("draft runtime grant policy still contains a role marker")
	}
	if _, err = admin.drafts.db.Exec(grantSQL); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(`SET ROLE ` + quotedRole); err != nil {
		t.Fatal(err)
	}
	var current string
	if err = db.QueryRow(`SELECT current_user`).Scan(&current); err != nil || current != role {
		t.Fatalf("restricted draft role is not active: current=%q err=%v", current, err)
	}
	drafts := NewPostgresDraftWorkflowStore(NewPostgresDraftStore(db))
	return drafts, &PostgresDraftPairingStore{db: db}, role
}

func ownerSaveRequest(t *testing.T, handler http.Handler, owner UserIdentity, draftID string, draftRevision, reviewRevision int64, projection RecipeContent) *httptest.ResponseRecorder {
	t.Helper()
	projectionJSON, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"draft_id":                 draftID,
		"expected_draft_revision":  draftRevision,
		"expected_review_revision": reviewRevision,
		"projection":               json.RawMessage(projectionJSON),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/draft-review/drafts", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", draftPreviewOrigin)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request = request.WithContext(context.WithValue(request.Context(), bearerContextKey{}, owner))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestDraftRuntimeLeastPrivilegeAndInterruptedSaveRecovery(t *testing.T) {
	admin, _, dsn := workflowTestStoreWithDSN(t)
	runtime, pairings, role := restrictedDraftRuntime(t, admin, dsn)
	config := testPairingConfig()
	verifier := strings.Repeat("r", 43)
	challenge := sha256.Sum256([]byte(verifier))
	created, err := pairings.CreatePairing(context.Background(), config, DraftPairingCreateRequest{
		CodeChallenge:       base64.RawURLEncoding.EncodeToString(challenge[:]),
		CodeChallengeMethod: "S256",
		ClientLabel:         "Restricted synthetic client",
	})
	if err != nil {
		t.Fatalf("restricted role create pairing: %v", err)
	}
	ownerIdentity := UserIdentity{Issuer: config.Issuer, Subject: "restricted-owner", Email: "restricted-owner@example.test"}
	if err = pairings.ApprovePairing(context.Background(), config, ownerIdentity, DraftPairingApproveRequest{UserCode: created.UserCode, ChallengeFingerprint: created.ChallengeFingerprint}); err != nil {
		t.Fatalf("restricted role approve pairing: %v", err)
	}
	approved, err := pairings.PollPairing(context.Background(), config, DraftPairingPollRequest{PairingID: created.PairingID, PollToken: created.PollToken, CodeVerifier: verifier})
	if err != nil {
		t.Fatalf("restricted role redeem pairing: %v", err)
	}
	access := DraftAccess{
		TokenHash:       sha256.Sum256([]byte(approved.Grant)),
		Environment:     config.Environment,
		Issuer:          config.Issuer,
		AllowedEmails:   []string{ownerIdentity.Email},
		RequiredScope:   "draft:propose",
		MembershipEpoch: config.MembershipEpoch,
	}
	owner := RecipeOwner{Issuer: ownerIdentity.Issuer, Subject: ownerIdentity.Subject}
	intent, replayed, err := runtime.CreateIntent(context.Background(), access, "restricted-runtime-intent", syntheticDraftSubmission())
	if err != nil || replayed {
		t.Fatalf("restricted role create intent: replay=%v err=%v", replayed, err)
	}
	accepted, err := runtime.DecideIntent(context.Background(), owner, intent.ID, intent.Revision, intent.ContentHash, true, access)
	if err != nil || accepted.DraftID == nil {
		t.Fatalf("restricted role approve intent: accepted=%#v err=%v", accepted, err)
	}
	draftID := *accepted.DraftID
	current, err := runtime.GetHostedDraft(context.Background(), owner, draftID)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := runtime.ReviewHostedDraft(context.Background(), owner, draftID, current.Draft.Revision, current.Draft.Review.Revision)
	if err != nil || ready.Draft.Review.State != "ready" {
		t.Fatalf("restricted role review draft: state=%q err=%v", ready.Draft.Review.State, err)
	}

	if _, err = admin.drafts.db.Exec(`CREATE FUNCTION public.reject_synthetic_draft_link() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='approved' THEN RAISE EXCEPTION 'synthetic link failure'; END IF; RETURN NEW; END $$;
CREATE TRIGGER reject_synthetic_draft_link BEFORE UPDATE ON public.draft_reviews FOR EACH ROW EXECUTE FUNCTION public.reject_synthetic_draft_link()`); err != nil {
		t.Fatal(err)
	}
	triggerPresent := true
	t.Cleanup(func() {
		if triggerPresent {
			_, _ = admin.drafts.db.Exec(`DROP TRIGGER IF EXISTS reject_synthetic_draft_link ON public.draft_reviews; DROP FUNCTION IF EXISTS public.reject_synthetic_draft_link()`)
		}
	})
	recipes := NewPostgresRecipeStore(admin.drafts.db)
	workflowConfig := DraftWorkflowConfig{Enabled: true, Environment: "preview", Issuer: config.Issuer, Origin: draftPreviewOrigin, MembershipEpoch: config.MembershipEpoch, AllowedEmails: []string{ownerIdentity.Email}}
	handler := OwnerDraftHandler(runtime, recipes, workflowConfig)
	projection := syntheticRecipeContent()
	first := ownerSaveRequest(t, handler, ownerIdentity, draftID, ready.Draft.Revision, ready.Draft.Review.Revision, projection)
	if first.Code != http.StatusServiceUnavailable || !strings.Contains(first.Body.String(), `"code":"unavailable"`) {
		t.Fatalf("interrupted link response: %d %s", first.Code, first.Body.String())
	}
	var recipesAfterFailure, receiptsAfterFailure int
	if err = admin.drafts.db.QueryRow(`SELECT count(*) FROM meal_planner.recipes WHERE owner_issuer=$1 AND owner_subject=$2`, owner.Issuer, owner.Subject).Scan(&recipesAfterFailure); err != nil {
		t.Fatal(err)
	}
	if err = admin.drafts.db.QueryRow(`SELECT count(*) FROM meal_planner.recipe_creates WHERE owner_issuer=$1 AND owner_subject=$2 AND operation_key=$3`, owner.Issuer, owner.Subject, draftID).Scan(&receiptsAfterFailure); err != nil {
		t.Fatal(err)
	}
	afterFailure, err := runtime.GetHostedDraft(context.Background(), owner, draftID)
	if err != nil || recipesAfterFailure != 1 || receiptsAfterFailure != 1 || afterFailure.Draft.Review.State != "ready" || afterFailure.Draft.ApprovedRecipeID != nil {
		t.Fatalf("partial save boundary: recipes=%d receipts=%d state=%q link=%v err=%v", recipesAfterFailure, receiptsAfterFailure, afterFailure.Draft.Review.State, afterFailure.Draft.ApprovedRecipeID, err)
	}

	changed := projection
	changed.Title = "Different projection must not replace committed recipe"
	conflict := ownerSaveRequest(t, handler, ownerIdentity, draftID, ready.Draft.Revision, ready.Draft.Review.Revision, changed)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), `"code":"revision_mismatch"`) {
		t.Fatalf("changed partial-save retry: %d %s", conflict.Code, conflict.Body.String())
	}
	if _, err = admin.drafts.db.Exec(`DROP TRIGGER reject_synthetic_draft_link ON public.draft_reviews; DROP FUNCTION public.reject_synthetic_draft_link()`); err != nil {
		t.Fatal(err)
	}
	triggerPresent = false
	retry := ownerSaveRequest(t, handler, ownerIdentity, draftID, ready.Draft.Revision, ready.Draft.Review.Revision, projection)
	if retry.Code != http.StatusOK {
		t.Fatalf("identical recovery retry: %d %s", retry.Code, retry.Body.String())
	}
	doubleRetry := ownerSaveRequest(t, handler, ownerIdentity, draftID, ready.Draft.Revision, ready.Draft.Review.Revision, projection)
	if doubleRetry.Code != http.StatusOK {
		t.Fatalf("completed save replay: %d %s", doubleRetry.Code, doubleRetry.Body.String())
	}
	final, err := runtime.GetHostedDraft(context.Background(), owner, draftID)
	if err != nil || final.Draft.Review.State != "approved" || final.Draft.ApprovedRecipeID == nil {
		t.Fatalf("recovered draft link: state=%q link=%v err=%v", final.Draft.Review.State, final.Draft.ApprovedRecipeID, err)
	}
	storedRecipe, err := recipes.Get(context.Background(), owner, *final.Draft.ApprovedRecipeID)
	if err != nil || !reflect.DeepEqual(storedRecipe.Content, projection) {
		t.Fatalf("recovered recipe changed: %v", err)
	}
	var recipeCount, receiptCount int
	if err = admin.drafts.db.QueryRow(`SELECT count(*) FROM meal_planner.recipes WHERE owner_issuer=$1 AND owner_subject=$2`, owner.Issuer, owner.Subject).Scan(&recipeCount); err != nil {
		t.Fatal(err)
	}
	if err = admin.drafts.db.QueryRow(`SELECT count(*) FROM meal_planner.recipe_creates WHERE owner_issuer=$1 AND owner_subject=$2 AND operation_key=$3`, owner.Issuer, owner.Subject, draftID).Scan(&receiptCount); err != nil {
		t.Fatal(err)
	}
	if recipeCount != 1 || receiptCount != 1 {
		t.Fatalf("recovery duplicated durable rows: recipes=%d receipts=%d", recipeCount, receiptCount)
	}

	denied := map[string]string{
		"legacy grant read":      `SELECT * FROM public.draft_ingest_grants`,
		"send token mutation":    `UPDATE public.draft_send_grants SET token_hash=decode(repeat('00',32),'hex')`,
		"intent owner mutation":  `UPDATE public.draft_send_intents SET owner_subject='other'`,
		"submission mutation":    `UPDATE public.draft_ingest_submissions SET response='{}'::json`,
		"review delete":          `DELETE FROM public.draft_reviews`,
		"pairing delete":         `DELETE FROM public.draft_pairings`,
		"draft truncate":         `TRUNCATE public.draft_send_intents`,
		"public schema create":   `CREATE TABLE public.forbidden (id integer)`,
		"new schema create":      `CREATE SCHEMA forbidden`,
		"recipe read":            `SELECT * FROM meal_planner.recipes`,
		"auth data read":         `SELECT * FROM auth.synthetic_accounts`,
		"unrelated public read":  `SELECT * FROM public.synthetic_private_data`,
		"migration history read": `SELECT * FROM meal_planner.schema_migrations`,
	}
	for name, statement := range denied {
		t.Run(name, func(t *testing.T) {
			_, statementErr := runtime.drafts.db.Exec(statement)
			var pgError *pq.Error
			if !errors.As(statementErr, &pgError) || pgError.Code != "42501" {
				t.Fatalf("role %s: want insufficient_privilege, got %v", role, statementErr)
			}
		})
	}
}

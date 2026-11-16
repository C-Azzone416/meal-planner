//go:build integration

package hosted

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"sync"
	"testing"
)

func workflowTestStore(t *testing.T) (*PostgresDraftWorkflowStore, DraftAccess) {
	t.Helper()
	recipes, _ := recipeTestDatabase(t)
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
	return NewPostgresDraftWorkflowStore(NewPostgresDraftStore(recipes.db)), access
}

func TestDraftWorkflowConcurrentApproveDenyIsAtomic(t *testing.T) {
	store, access := workflowTestStore(t)
	owner := RecipeOwner{Issuer: access.Issuer, Subject: "owner-a"}
	intent, replay, err := store.CreateIntent(context.Background(), access, "synthetic-intent-operation", syntheticDraftSubmission())
	if err != nil || replay {
		t.Fatalf("create intent: replay=%v err=%v", replay, err)
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

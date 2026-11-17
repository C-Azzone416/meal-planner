//go:build integration

package hosted

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

func pairingTestStore(t *testing.T) (*PostgresDraftPairingStore, DraftPairingConfig) {
	t.Helper()
	recipes, _ := recipeTestDatabase(t)
	migration, err := os.ReadFile("migrations/005_draft_pairing.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = recipes.db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	return &PostgresDraftPairingStore{db: recipes.db}, testPairingConfig()
}

func createApprovedPairing(t *testing.T, s *PostgresDraftPairingStore, c DraftPairingConfig) (DraftPairingCreated, string) {
	t.Helper()
	verifier := stringsRepeat("a", 43)
	h := sha256.Sum256([]byte(verifier))
	created, err := s.CreatePairing(context.Background(), c, DraftPairingCreateRequest{CodeChallenge: base64.RawURLEncoding.EncodeToString(h[:]), CodeChallengeMethod: "S256", ClientLabel: "Synthetic Mac"})
	if err != nil {
		t.Fatal(err)
	}
	err = s.ApprovePairing(context.Background(), c, UserIdentity{Issuer: c.Issuer, Subject: "owner-a", Email: "owner@example.test"}, DraftPairingApproveRequest{UserCode: created.UserCode, ChallengeFingerprint: created.ChallengeFingerprint})
	if err != nil {
		t.Fatal(err)
	}
	return created, verifier
}

func stringsRepeat(s string, n int) string {
	b := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		b = append(b, s...)
	}
	return string(b)
}

func syntheticPairingCreateRequest() DraftPairingCreateRequest {
	verifier := stringsRepeat("a", 43)
	h := sha256.Sum256([]byte(verifier))
	return DraftPairingCreateRequest{CodeChallenge: base64.RawURLEncoding.EncodeToString(h[:]), CodeChallengeMethod: "S256", ClientLabel: "Synthetic Mac"}
}

func TestDraftPairingPostgresCreateCapacityReopensAfterExpiry(t *testing.T) {
	s, c := pairingTestStore(t)
	ctx := context.Background()
	var first DraftPairingCreated
	for i := 0; i < maxActiveDraftPairings; i++ {
		created, err := s.CreatePairing(ctx, c, syntheticPairingCreateRequest())
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if i == 0 {
			first = created
		}
	}
	if _, err := s.CreatePairing(ctx, c, syntheticPairingCreateRequest()); !errors.Is(err, ErrDraftPairingLimit) {
		t.Fatalf("expected active cap, got %v", err)
	}
	if _, err := s.db.Exec(`UPDATE public.draft_pairings SET created_at=instant.now-interval '20 minutes',expires_at=instant.now-interval '10 minutes' FROM (SELECT clock_timestamp() AS now) instant WHERE id=$1`, first.PairingID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePairing(ctx, c, syntheticPairingCreateRequest()); err != nil {
		t.Fatalf("capacity did not reopen after expiry: %v", err)
	}
}

func TestDraftPairingPostgresHourlyCreateCapacity(t *testing.T) {
	s, c := pairingTestStore(t)
	ctx := context.Background()
	for i := 0; i < maxHourlyDraftPairings; i++ {
		created, err := s.CreatePairing(ctx, c, syntheticPairingCreateRequest())
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if _, err = s.db.Exec(`UPDATE public.draft_pairings SET created_at=instant.now-interval '20 minutes',expires_at=instant.now-interval '10 minutes' FROM (SELECT clock_timestamp() AS now) instant WHERE id=$1`, created.PairingID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreatePairing(ctx, c, syntheticPairingCreateRequest()); !errors.Is(err, ErrDraftPairingLimit) {
		t.Fatalf("expected hourly cap, got %v", err)
	}
}

func TestDraftPairingPostgresRetainedCreateCapacity(t *testing.T) {
	s, c := pairingTestStore(t)
	_, err := s.db.Exec(`INSERT INTO public.draft_pairings
 (id,environment,issuer,user_code,poll_token_hash,code_challenge,challenge_fingerprint,client_label,status,created_at,expires_at)
 SELECT ('00000000-0000-4000-8000-'||lpad(i::text,12,'0'))::uuid,$1,$2,NULL,
        decode(lpad(to_hex(i),64,'0'),'hex'),repeat('a',43),'00000000','Synthetic cap','denied',
		instant.now-interval '2 hours',instant.now-interval '110 minutes'
	 FROM generate_series(1,$3) AS i CROSS JOIN (SELECT clock_timestamp() AS now) instant`, c.Environment, c.Issuer, maxRetainedPairings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreatePairing(context.Background(), c, syntheticPairingCreateRequest()); !errors.Is(err, ErrDraftPairingLimit) {
		t.Fatalf("expected retained cap, got %v", err)
	}
}

func TestDraftPairingPostgresConcurrentCreateCapacityIsSerialized(t *testing.T) {
	s, c := pairingTestStore(t)
	results := make(chan error, maxActiveDraftPairings+5)
	var wg sync.WaitGroup
	for i := 0; i < cap(results); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreatePairing(context.Background(), c, syntheticPairingCreateRequest())
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success, limited := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrDraftPairingLimit):
			limited++
		default:
			t.Fatalf("unexpected create result: %v", err)
		}
	}
	if success != maxActiveDraftPairings || limited != 5 {
		t.Fatalf("success=%d limited=%d", success, limited)
	}
}

func TestDraftPairingPostgresLifecycleReplayRevocationAndEpoch(t *testing.T) {
	s, c := pairingTestStore(t)
	ctx := context.Background()
	verifier := stringsRepeat("a", 43)
	h := sha256.Sum256([]byte(verifier))
	created, err := s.CreatePairing(ctx, c, DraftPairingCreateRequest{CodeChallenge: base64.RawURLEncoding.EncodeToString(h[:]), CodeChallengeMethod: "S256", ClientLabel: "Synthetic Mac"})
	if err != nil || created.VerificationURI != draftPreviewOrigin+"/connect" || created.ExpiresIn != 600 {
		t.Fatal(created, err)
	}
	poll := DraftPairingPollRequest{PairingID: created.PairingID, PollToken: created.PollToken, CodeVerifier: verifier}
	if _, err = s.PollPairing(ctx, c, poll); !errors.Is(err, ErrDraftPairingPending) {
		t.Fatal("expected pending", err)
	}
	if _, err = s.PollPairing(ctx, c, poll); !errors.Is(err, ErrDraftPairingSlowDown) {
		t.Fatal("expected slow_down", err)
	}
	if err = s.ApprovePairing(ctx, c, UserIdentity{Issuer: c.Issuer, Subject: "owner-a", Email: "owner@example.test"}, DraftPairingApproveRequest{UserCode: created.UserCode, ChallengeFingerprint: created.ChallengeFingerprint}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE public.draft_pairings SET last_polled_at=clock_timestamp()-interval '6 seconds' WHERE id=$1`, created.PairingID); err != nil {
		t.Fatal(err)
	}
	approved, err := s.PollPairing(ctx, c, poll)
	if err != nil || approved.State != "approved" || approved.Scope != "draft:propose" || len(approved.Grant) != len("mp_lc1_pv_")+43 {
		t.Fatal(approved, err)
	}
	if _, err = s.PollPairing(ctx, c, poll); !errors.Is(err, ErrDraftPairingGone) {
		t.Fatal("secret replayed", err)
	}
	tokenHash := sha256.Sum256([]byte(approved.Grant))
	grant, err := s.AuthenticateDraftProposal(ctx, c, tokenHash)
	if err != nil || grant.Owner.Subject != "owner-a" {
		t.Fatal(grant, err)
	}
	items, err := s.ListDraftGrants(ctx, c, grant.Owner)
	if err != nil || len(items) != 1 || !items[0].Active {
		t.Fatal(items, err)
	}
	changed := c
	changed.MembershipEpoch = "epoch_000000000002"
	if _, err = s.AuthenticateDraftProposal(ctx, changed, tokenHash); !errors.Is(err, ErrDraftUnauthorized) {
		t.Fatal("old epoch revived", err)
	}
	if err = s.RevokeDraftGrant(ctx, c, grant.Owner, grant.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.RevokeDraftGrant(ctx, c, grant.Owner, grant.ID); err != nil {
		t.Fatal("revoke not idempotent", err)
	}
	if _, err = s.AuthenticateDraftProposal(ctx, c, tokenHash); !errors.Is(err, ErrDraftUnauthorized) {
		t.Fatal("revoked grant accepted", err)
	}
	if err = s.RevokeDraftGrant(ctx, c, RecipeOwner{Issuer: c.Issuer, Subject: "owner-b"}, grant.ID); !errors.Is(err, ErrDraftPairingMissing) {
		t.Fatal("cross owner grant visible", err)
	}
}

func TestDraftPairingPostgresConcurrentRedemptionReturnsSecretOnce(t *testing.T) {
	s, c := pairingTestStore(t)
	created, verifier := createApprovedPairing(t, s, c)
	poll := DraftPairingPollRequest{PairingID: created.PairingID, PollToken: created.PollToken, CodeVerifier: verifier}
	type result struct {
		out DraftPairingPollResult
		err error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); o, e := s.PollPairing(context.Background(), c, poll); results <- result{o, e} }()
	}
	wg.Wait()
	close(results)
	success, gone := 0, 0
	for r := range results {
		if r.err == nil && r.out.Grant != "" {
			success++
		} else if errors.Is(r.err, ErrDraftPairingGone) {
			gone++
		} else {
			t.Fatalf("unexpected: %#v %v", r.out, r.err)
		}
	}
	if success != 1 || gone != 1 {
		t.Fatalf("success=%d gone=%d", success, gone)
	}
}

func TestDraftPairingPostgresGrantExpiry(t *testing.T) {
	s, c := pairingTestStore(t)
	created, verifier := createApprovedPairing(t, s, c)
	approved, err := s.PollPairing(context.Background(), c, DraftPairingPollRequest{PairingID: created.PairingID, PollToken: created.PollToken, CodeVerifier: verifier})
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte(approved.Grant))
	if _, err = s.db.Exec(`UPDATE public.draft_send_grants SET created_at=clock_timestamp()-interval '7 days',expires_at=clock_timestamp()-interval '1 second' WHERE token_hash=$1`, h[:]); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err = s.AuthenticateDraftProposal(ctx, c, h); !errors.Is(err, ErrDraftUnauthorized) {
		t.Fatal("expired grant accepted", err)
	}
}

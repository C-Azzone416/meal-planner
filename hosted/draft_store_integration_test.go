//go:build integration

package hosted

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"github.com/lib/pq"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

func draftTestStore(t *testing.T) (*PostgresDraftStore, DraftAccess) {
	t.Helper()
	recipes, _ := recipeTestDatabase(t)
	migration, err := os.ReadFile("migrations/004_draft_ingest.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = recipes.db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	a := DraftAccess{TokenHash: sha256.Sum256([]byte("synthetic-grant-one")), Environment: "preview", Issuer: "https://preview.example.invalid/auth", AllowedEmails: []string{"owner@example.invalid"}}
	addDraftGrant(t, recipes.db, a, "owner-a")
	return NewPostgresDraftStore(recipes.db), a
}
func addDraftGrant(t *testing.T, db interface {
	Exec(string, ...any) (sql.Result, error)
}, a DraftAccess, subject string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO public.draft_ingest_grants(token_hash,environment,owner_issuer,owner_subject,email_at_approval,scope,expires_at) VALUES ($1,'preview',$2,$3,$4,'draft:create',clock_timestamp()+interval '1 day')`, a.TokenHash[:], a.Issuer, subject, a.AllowedEmails[0])
	if err != nil {
		t.Fatal(err)
	}
}
func syntheticDraftSubmission() DraftSubmission {
	return DraftSubmission{ContentSchemaVersion: 1, Content: draft.Content{Title: "Synthetic incomplete draft", Ingredients: []draft.Ingredient{}, Instructions: []draft.Step{}, Sources: []draft.Source{}, Guidance: []draft.Guidance{}}}
}
func TestDraftPostgresRetryIsolationRevocation(t *testing.T) {
	s, a := draftTestStore(t)
	ctx := context.Background()
	in := syntheticDraftSubmission()
	if err := s.Authenticate(ctx, a); err != nil {
		t.Fatal(err)
	}
	first, replay, err := s.Submit(ctx, a, "synthetic-operation-001", in)
	if err != nil || replay {
		t.Fatalf("first: %v replay=%v", err, replay)
	}
	retry, replay, err := s.Submit(ctx, a, "synthetic-operation-001", in)
	if err != nil || !replay || !reflect.DeepEqual(first, retry) {
		t.Fatalf("retry: %v replay=%v", err, replay)
	}
	otherGrant := a
	otherGrant.TokenHash = sha256.Sum256([]byte("synthetic-same-owner-other-grant"))
	addDraftGrant(t, s.db, otherGrant, "owner-a")
	if _, _, err = s.Submit(ctx, otherGrant, "synthetic-operation-001", in); !errors.Is(err, ErrDraftUnauthorized) {
		t.Fatal("cross-grant receipt disclosed", err)
	}
	changedForOther := in
	changedForOther.Content.Title = "Different body"
	if _, _, err = s.Submit(ctx, otherGrant, "synthetic-operation-001", changedForOther); !errors.Is(err, ErrDraftUnauthorized) {
		t.Fatal("cross-grant request hash disclosed", err)
	}
	changed := in
	changed.Content.Title = "Changed"
	if _, _, err = s.Submit(ctx, a, "synthetic-operation-001", changed); !errors.Is(err, ErrDraftConflict) {
		t.Fatal("conflicting key not rejected", err)
	}
	if first.Draft.Status != "draft" || first.Draft.Origin != "agent" || first.Draft.ApprovedRecipeID != nil || first.Draft.Review.State != "not_started" {
		t.Fatal("approval or origin incorrectly assigned")
	}
	b := a
	b.TokenHash = sha256.Sum256([]byte("synthetic-other-account"))
	addDraftGrant(t, s.db, b, "owner-b")
	other, _, err := s.Submit(ctx, b, "synthetic-operation-001", in)
	if err != nil || other.Draft.ID == first.Draft.ID || other.Draft.Owner.Subject != "owner-b" {
		t.Fatal("cross-account isolation failed", err)
	}
	wrong := a
	wrong.Environment = "production"
	if err = s.Authenticate(ctx, wrong); !errors.Is(err, ErrDraftUnauthorized) {
		t.Fatal("environment accepted", err)
	}
	wrong = a
	wrong.Issuer += "-other"
	if err = s.Authenticate(ctx, wrong); !errors.Is(err, ErrDraftUnauthorized) {
		t.Fatal("issuer accepted", err)
	}
	wrong = a
	wrong.AllowedEmails = []string{"other@example.invalid"}
	if err = s.Authenticate(ctx, wrong); !errors.Is(err, ErrDraftUnauthorized) {
		t.Fatal("removed membership accepted", err)
	}
	if _, err = s.db.Exec(`UPDATE public.draft_ingest_grants SET revoked_at=clock_timestamp() WHERE token_hash=$1`, a.TokenHash[:]); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Submit(ctx, a, "synthetic-operation-001", in); !errors.Is(err, ErrDraftUnauthorized) {
		t.Fatal("revoked grant replay accepted", err)
	}
}
func TestDraftPostgresConcurrentReplayAndLimits(t *testing.T) {
	s, a := draftTestStore(t)
	in := syntheticDraftSubmission()
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	ids := make(chan string, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, _, err := s.Submit(ctx, a, "concurrent-operation", in)
			if err != nil {
				errs <- err
			}
			ids <- r.Draft.ID
		}()
	}
	wg.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		t.Error(err)
	}
	var id string
	for got := range ids {
		if id == "" {
			id = got
		}
		if got != id {
			t.Fatal("duplicate created")
		}
	}
	for i := 1; i < 20; i++ {
		if _, _, err := s.Submit(ctx, a, fmt.Sprintf("operation-%08d", i), in); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Submit(ctx, a, "operation-rate-overflow", in); !errors.Is(err, ErrDraftQuota) {
		t.Fatal("rate cap bypass", err)
	}
	if _, replay, err := s.Submit(ctx, a, "concurrent-operation", in); err != nil || !replay {
		t.Fatal("quota blocked safe replay", err)
	}
	// Synthetic owner-only fixture manipulation ages records, never a deployed DB.
	if _, err := s.db.Exec(`UPDATE public.draft_ingest_submissions SET created_at=clock_timestamp()-interval '2 hours'`); err != nil {
		t.Fatal(err)
	}
	for batch := 0; batch < 4; batch++ {
		for i := 0; i < 20; i++ {
			if _, _, err := s.Submit(ctx, a, fmt.Sprintf("batch-%d-operation-%08d", batch, i), in); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.db.Exec(`UPDATE public.draft_ingest_submissions SET created_at=clock_timestamp()-interval '2 hours'`); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Submit(ctx, a, "operation-count-overflow", in); !errors.Is(err, ErrDraftQuota) {
		t.Fatal("count cap bypass", err)
	}
}
func TestDraftPostgresAuthExpiry(t *testing.T) {
	s, a := draftTestStore(t)
	ctx := context.Background()
	_, err := s.db.Exec(`UPDATE public.draft_ingest_grants SET expires_at=clock_timestamp()+interval '150 milliseconds',created_at=clock_timestamp()-interval '1 second' WHERE token_hash=$1`, a.TokenHash[:])
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, _, err = s.Submit(ctx, a, "expired-operation", syntheticDraftSubmission()); !errors.Is(err, ErrDraftUnauthorized) {
		t.Fatal("expired grant accepted", err)
	}
}

func TestDraftPostgresRetainedBytesQuota(t *testing.T) {
	s, a := draftTestStore(t)
	ctx := context.Background()
	in := syntheticDraftSubmission()
	if _, _, err := s.Submit(ctx, a, "first-operation", in); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE public.draft_ingest_submissions SET retained_bytes=16777216`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Submit(ctx, a, "over-byte-limit", in); !errors.Is(err, ErrDraftQuota) {
		t.Fatal("retained-byte limit bypass", err)
	}
	if _, replay, err := s.Submit(ctx, a, "first-operation", in); err != nil || !replay {
		t.Fatal("byte quota blocked replay", err)
	}
}
func TestDraftPostgresRestrictedRuntimeRole(t *testing.T) {
	recipes, dsn := recipeTestDatabase(t)
	migration, err := os.ReadFile("migrations/004_draft_ingest.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = recipes.db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	a := DraftAccess{TokenHash: sha256.Sum256([]byte("synthetic-restricted")), Environment: "preview", Issuer: "https://preview.example.invalid/auth", AllowedEmails: []string{"owner@example.invalid"}}
	addDraftGrant(t, recipes.db, a, "restricted-owner")
	id, _ := newRecipeUUID()
	role := "draft_test_" + strings.ReplaceAll(id, "-", "")
	quoted := pq.QuoteIdentifier(role)
	if _, err = recipes.db.Exec(`CREATE ROLE ` + quoted + ` LOGIN NOINHERIT`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = recipes.db.Exec(`DROP OWNED BY ` + quoted)
		if _, e := recipes.db.Exec(`DROP ROLE ` + quoted); e != nil {
			t.Error(e)
		}
	})
	if _, err = recipes.db.Exec(`GRANT USAGE ON SCHEMA public TO ` + quoted + `; GRANT SELECT ON public.draft_ingest_grants TO ` + quoted + `; GRANT UPDATE(last_used_at) ON public.draft_ingest_grants TO ` + quoted + `; GRANT SELECT,INSERT ON public.draft_ingest_submissions TO ` + quoted); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(dsn)
	u.User = url.User(role)
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	runtime := NewPostgresDraftStore(db)
	if _, _, err = runtime.Submit(context.Background(), a, "restricted-operation", syntheticDraftSubmission()); err != nil {
		t.Fatal("minimal runtime submit failed", err)
	}
	for _, statement := range []string{`DELETE FROM public.draft_ingest_submissions`, `UPDATE public.draft_ingest_submissions SET operation_key='wrong'`, `UPDATE public.draft_ingest_grants SET revoked_at=NULL`, `DELETE FROM public.draft_ingest_grants`, `SELECT * FROM meal_planner.recipes`, `CREATE TABLE public.forbidden_runtime_test (id int)`} {
		if _, err = db.Exec(statement); err == nil {
			t.Fatalf("restricted runtime allowed %s", statement)
		}
	}
}

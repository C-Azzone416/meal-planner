//go:build integration

package hosted

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/lib/pq"
)

// Each test gets a newly created disposable database, never a schema in the
// caller's database. Refuse non-loopback targets and all query overrides.
func recipeTestDatabase(t *testing.T) (*PostgresRecipeStore, string) {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL required for disposable Postgres integration tests")
	}
	u, e := url.Parse(raw)
	if e != nil {
		t.Fatal("invalid test database URL")
	}
	if (u.Scheme != "postgres" && u.Scheme != "postgresql") || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") || u.Fragment != "" {
		t.Fatal("integration database must be disposable loopback Postgres")
	}
	q, e := url.ParseQuery(u.RawQuery)
	if e != nil || len(q) != 1 || len(q["sslmode"]) != 1 || q.Get("sslmode") != "disable" {
		t.Fatal("test URL allows only sslmode=disable")
	}
	admin, e := sql.Open("postgres", raw)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { admin.Close() })
	id, e := newRecipeUUID()
	if e != nil {
		t.Fatal(e)
	}
	name := "recipe_test_" + strings.ReplaceAll(id, "-", "")
	if _, e = admin.Exec(`CREATE DATABASE ` + pq.QuoteIdentifier(name)); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := admin.Exec(`DROP DATABASE ` + pq.QuoteIdentifier(name) + ` WITH (FORCE)`); e != nil {
			t.Error(e)
		}
	})
	u.Path = "/" + name
	dsn := u.String()
	db, e := sql.Open("postgres", dsn)
	if e != nil {
		t.Fatal(e)
	}
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { db.Close() })
	migration, e := os.ReadFile("migrations/001_recipes.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(string(migration)); e != nil {
		t.Fatal(e)
	}
	return NewPostgresRecipeStore(db), dsn
}
func TestRecipePostgresDurabilityIsolationAndRetries(t *testing.T) {
	s, dsn := recipeTestDatabase(t)
	ctx := context.Background()
	a := RecipeOwner{"https://issuer-a.example.test", "same-subject"}
	b := RecipeOwner{"https://issuer-b.example.test", "same-subject"}
	c := syntheticRecipeContent()
	original, replay, e := s.Create(ctx, a, "create-one", c)
	if e != nil || replay {
		t.Fatalf("create: %v %v", replay, e)
	}
	if original.Status != "approved" || original.Origin != "manual" || original.Revision != 1 || original.CreatedAt.IsZero() || original.UpdatedAt.IsZero() || !validRecipeUUID(original.ID) {
		t.Fatal("incorrect server metadata")
	}
	other, _, e := s.Create(ctx, b, "create-one", c)
	if e != nil || other.ID == original.ID {
		t.Fatalf("issuer isolation: %v", e)
	}
	if _, e = s.Get(ctx, b, original.ID); !errors.Is(e, ErrRecipeNotFound) {
		t.Fatalf("foreign get: %v", e)
	}
	if _, e = s.Update(ctx, b, original.ID, 1, c); !errors.Is(e, ErrRecipeNotFound) {
		t.Fatalf("foreign update: %v", e)
	}
	missing, _ := newRecipeUUID()
	if _, e = s.Update(ctx, b, missing, 1, c); !errors.Is(e, ErrRecipeNotFound) {
		t.Fatalf("missing update: %v", e)
	}
	changed := c
	changed.Title = "Synthetic edited"
	updated, e := s.Update(ctx, a, original.ID, 1, changed)
	if e != nil || updated.Revision != 2 {
		t.Fatalf("update: %v", e)
	}
	immediate, err := s.Update(ctx, a, original.ID, 1, changed)
	if err != nil || !reflect.DeepEqual(immediate, updated) {
		t.Fatalf("immediate replay: %v", err)
	}
	latest := changed
	latest.Title = "Later edit"
	if _, e = s.Update(ctx, a, original.ID, 2, latest); e != nil {
		t.Fatal(e)
	}
	recovered, e := s.Update(ctx, a, original.ID, 1, changed)
	if !errors.Is(e, ErrRecipeConflict) || recovered.ID != "" {
		t.Fatalf("response-loss replay: %v", e)
	}
	if _, e = s.Update(ctx, a, original.ID, 1, latest); !errors.Is(e, ErrRecipeConflict) {
		t.Fatalf("stale change: %v", e)
	}
	if _, _, e = s.Create(ctx, a, "create-one", changed); !errors.Is(e, ErrRecipeIdempotencyConflict) {
		t.Fatalf("changed create: %v", e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := sql.Open("postgres", dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	s = NewPostgresRecipeStore(reopened)
	got, e := s.Get(ctx, a, original.ID)
	if e != nil || got.Revision != 3 || got.Content.Title != latest.Title {
		t.Fatalf("restart: %v", e)
	}
	got, replay, e = s.Create(ctx, a, "create-one", c)
	if e != nil || !replay || !reflect.DeepEqual(got, original) {
		t.Fatalf("durable original receipt: %v", e)
	}
	got, e = s.Update(ctx, a, original.ID, 1, changed)
	if !errors.Is(e, ErrRecipeConflict) {
		t.Fatalf("durable update receipt: %v", e)
	}
	for i := 0; i < 4; i++ {
		if _, _, e = s.Create(ctx, a, fmt.Sprintf("page-%d", i), c); e != nil {
			t.Fatal(e)
		}
	}
	seen := map[string]bool{}
	after := ""
	for {
		rows, next, e := s.List(ctx, a, after, 2)
		if e != nil {
			t.Fatal(e)
		}
		for _, r := range rows {
			if seen[r.ID] || r.ID == other.ID {
				t.Fatal("duplicate/foreign pagination")
			}
			seen[r.ID] = true
		}
		if next == "" {
			break
		}
		after = next
	}
	if len(seen) != 5 {
		t.Fatalf("pagination count %d", len(seen))
	}
}
func TestRecipePostgresConcurrentWrites(t *testing.T) {
	s, _ := recipeTestDatabase(t)
	ctx := context.Background()
	o := RecipeOwner{"https://issuer.example.test", "owner"}
	c := syntheticRecipeContent()
	const n = 12
	var wg sync.WaitGroup
	ids := make(chan string, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r, _, e := s.Create(ctx, o, "same-key", c); ids <- r.ID; errs <- e }()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	id := ""
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("duplicate create")
		}
		id = got
	}
	errs = make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cc := c
			cc.Title = fmt.Sprintf("edit-%d", i)
			_, e := s.Update(ctx, o, id, 1, cc)
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	wins := 0
	for e := range errs {
		if e == nil {
			wins++
		} else if !errors.Is(e, ErrRecipeConflict) {
			t.Fatal(e)
		}
	}
	if wins != 1 {
		t.Fatalf("CAS winners %d", wins)
	}
	got, e := s.Get(ctx, o, id)
	if e != nil || got.Revision != 2 {
		t.Fatalf("CAS final: %v", e)
	}
}
func TestRecipePostgresRollbackAndMigration(t *testing.T) {
	s, _ := recipeTestDatabase(t)
	ctx := context.Background()
	o := RecipeOwner{"issuer", "owner"}
	c := syntheticRecipeContent()
	// Inject a receipt write failure after recipe INSERT; both must roll back.
	_, e := s.db.Exec(`CREATE FUNCTION meal_planner.reject_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic receipt failure'; END $$;
 CREATE TRIGGER reject_create BEFORE INSERT ON meal_planner.recipe_creates FOR EACH ROW EXECUTE FUNCTION meal_planner.reject_receipt();`)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Create(ctx, o, "retry", c); e == nil {
		t.Fatal("expected failure")
	}
	rows, _, e := s.List(ctx, o, "", 10)
	if e != nil || len(rows) != 0 {
		t.Fatalf("partial create escaped rollback: %v", e)
	}
	if _, e = s.db.Exec(`DROP TRIGGER reject_create ON meal_planner.recipe_creates`); e != nil {
		t.Fatal(e)
	}
	saved, _, e := s.Create(ctx, o, "retry", c)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`CREATE TRIGGER reject_update BEFORE INSERT ON meal_planner.recipe_updates FOR EACH ROW EXECUTE FUNCTION meal_planner.reject_receipt()`); e != nil {
		t.Fatal(e)
	}
	c.Title = "Uncommitted"
	if _, e = s.Update(ctx, o, saved.ID, 1, c); e == nil {
		t.Fatal("expected update receipt failure")
	}
	got, e := s.Get(ctx, o, saved.ID)
	if e != nil || got.Revision != 1 || got.Content.Title != saved.Content.Title {
		t.Fatalf("partial update escaped: %v", e)
	}
	if _, e = s.db.Exec(`DROP TRIGGER reject_update ON meal_planner.recipe_updates`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Update(ctx, o, saved.ID, 1, c); e != nil {
		t.Fatal(e)
	}
	// Reapplication fails atomically; existing data remains untouched.
	migration, e := os.ReadFile("migrations/001_recipes.sql")
	if e != nil {
		t.Fatal(e)
	}
	conn, e := s.db.Conn(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	if _, e = conn.ExecContext(ctx, string(migration)); e == nil {
		t.Fatal("duplicate migration succeeded")
	}
	if _, e = conn.ExecContext(ctx, "ROLLBACK"); e != nil {
		t.Fatal(e)
	}
	got, e = s.Get(ctx, o, saved.ID)
	if e != nil || got.Revision != 2 {
		t.Fatalf("migration damaged existing data: %v", e)
	}
}

func TestRecipePostgresReservedDraftsAndCancellation(t *testing.T) {
	s, _ := recipeTestDatabase(t)
	ctx := context.Background()
	o := RecipeOwner{"issuer", "subject"}
	c := syntheticRecipeContent()
	saved, _, err := s.Create(ctx, o, "one", c)
	if err != nil {
		t.Fatal(err)
	}
	other := RecipeOwner{"issuer", "other-subject"}
	if _, err = s.Get(ctx, other, saved.ID); !errors.Is(err, ErrRecipeNotFound) {
		t.Fatalf("subject isolation: %v", err)
	}
	if rows, _, err := s.List(ctx, other, "", 10); err != nil || len(rows) != 0 {
		t.Fatalf("subject list: %v", err)
	}
	for _, state := range []struct{ status, origin string }{{"draft", "manual"}, {"draft", "agent"}, {"approved", "agent"}} {
		if _, err = s.db.Exec(`UPDATE meal_planner.recipes SET status=$1,origin=$2 WHERE owner_issuer=$3 AND owner_subject=$4 AND id=$5`, state.status, state.origin, o.Issuer, o.Subject, saved.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Get(ctx, o, saved.ID); !errors.Is(err, ErrRecipeNotFound) {
			t.Fatalf("reserved get %+v: %v", state, err)
		}
		if _, err = s.Update(ctx, o, saved.ID, 1, c); !errors.Is(err, ErrRecipeNotFound) {
			t.Fatalf("reserved update %+v: %v", state, err)
		}
		if rows, _, err := s.List(ctx, o, "", 10); err != nil || len(rows) != 0 {
			t.Fatalf("reserved list %+v: %v", state, err)
		}
	}
	if _, _, err = s.Create(ctx, o, "invalid", RecipeContent{}); err == nil {
		t.Fatal("accepted invalid content")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err = s.Create(canceled, o, "canceled", c); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if _, replay, err := s.Create(ctx, o, "canceled", c); err != nil || replay {
		t.Fatalf("canceled transaction left a receipt: %v", err)
	}
}

func TestRecipePostgresLeastPrivilegeRuntime(t *testing.T) {
	admin, dsn := recipeTestDatabase(t)
	ctx := context.Background()
	id, err := newRecipeUUID()
	if err != nil {
		t.Fatal(err)
	}
	role := "recipe_runtime_test_" + strings.ReplaceAll(id, "-", "")
	quotedRole := pq.QuoteIdentifier(role)
	// A unique NOLOGIN role exists only in the disposable local test cluster.
	// SET ROLE tests exactly its privileges without creating a password or login.
	if _, err = admin.db.Exec(`CREATE ROLE ` + quotedRole + ` NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.db.Exec(`DROP OWNED BY ` + quotedRole); err != nil {
			t.Error(err)
		}
		if _, err := admin.db.Exec(`DROP ROLE ` + quotedRole); err != nil {
			t.Error(err)
		}
	})
	if _, err = admin.db.Exec(`
 CREATE SCHEMA auth;
 CREATE TABLE auth.synthetic_accounts (id integer PRIMARY KEY);
 CREATE TABLE public.synthetic_private_data (id integer PRIMARY KEY);
 GRANT USAGE ON SCHEMA meal_planner TO ` + quotedRole + `;
 GRANT SELECT, INSERT, UPDATE ON meal_planner.recipes TO ` + quotedRole + `;
 GRANT SELECT, INSERT ON meal_planner.recipe_creates, meal_planner.recipe_updates TO ` + quotedRole); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	// Keep SET ROLE on the same single pooled session for all repository operations.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(`SET ROLE ` + quotedRole); err != nil {
		t.Fatal(err)
	}
	var effective string
	if err = db.QueryRow(`SELECT current_user`).Scan(&effective); err != nil || effective != role {
		t.Fatalf("runtime role not active: %v", err)
	}
	s := NewPostgresRecipeStore(db)
	owner := RecipeOwner{"synthetic-issuer", "synthetic-owner"}
	content := syntheticRecipeContent()
	created, replay, err := s.Create(ctx, owner, "least-privilege", content)
	if err != nil || replay {
		t.Fatalf("runtime create: %v", err)
	}
	retry, replay, err := s.Create(ctx, owner, "least-privilege", content)
	if err != nil || !replay || !reflect.DeepEqual(retry, created) {
		t.Fatalf("runtime create receipt: %v", err)
	}
	fetched, err := s.Get(ctx, owner, created.ID)
	if err != nil || !reflect.DeepEqual(fetched, created) {
		t.Fatalf("runtime get: %v", err)
	}
	rows, _, err := s.List(ctx, owner, "", 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("runtime list: %v", err)
	}
	content.Title = "Synthetic least privilege edit"
	updated, err := s.Update(ctx, owner, created.ID, 1, content)
	if err != nil || updated.Revision != 2 {
		t.Fatalf("runtime update: %v", err)
	}
	retry, err = s.Update(ctx, owner, created.ID, 1, content)
	if err != nil || !reflect.DeepEqual(retry, updated) {
		t.Fatalf("runtime update receipt: %v", err)
	}
	denied := map[string]string{
		"create in recipe schema": `CREATE TABLE meal_planner.forbidden (id integer)`,
		"create in public schema": `CREATE TABLE public.forbidden (id integer)`,
		"create schema":           `CREATE SCHEMA forbidden`,
		"drop recipe table":       `DROP TABLE meal_planner.recipes CASCADE`,
		"drop recipe schema":      `DROP SCHEMA meal_planner CASCADE`,
		"delete recipes":          `DELETE FROM meal_planner.recipes`,
		"delete create receipts":  `DELETE FROM meal_planner.recipe_creates`,
		"delete update receipts":  `DELETE FROM meal_planner.recipe_updates`,
		"modify create receipts":  `UPDATE meal_planner.recipe_creates SET operation_key='changed'`,
		"modify update receipts":  `UPDATE meal_planner.recipe_updates SET expected_revision=99`,
		"migration history":       `SELECT * FROM meal_planner.schema_migrations`,
		"auth data":               `SELECT * FROM auth.synthetic_accounts`,
		"public data":             `SELECT * FROM public.synthetic_private_data`,
	}
	for name, statement := range denied {
		t.Run(name, func(t *testing.T) {
			_, err := db.Exec(statement)
			var pgError *pq.Error
			if !errors.As(err, &pgError) || pgError.Code != "42501" {
				t.Fatalf("want insufficient_privilege, got %v", err)
			}
		})
	}
	final, err := s.Get(ctx, owner, created.ID)
	if err != nil || !reflect.DeepEqual(final, updated) {
		t.Fatalf("denied actions damaged saved data: %v", err)
	}
}

func TestRecipePostgresTransactionLocalTimeouts(t *testing.T) {
	s, _ := recipeTestDatabase(t)
	ctx := context.Background()
	// Reuse one backend connection to expose any settings leaked by commit/rollback.
	s.db.SetMaxOpenConns(1)
	s.db.SetMaxIdleConns(1)
	const settings = `SELECT current_setting('statement_timeout'), current_setting('lock_timeout'), current_setting('transaction_read_only')`
	var baselineStatement, baselineLock, baselineReadOnly string
	if err := s.db.QueryRowContext(ctx, settings).Scan(&baselineStatement, &baselineLock, &baselineReadOnly); err != nil {
		t.Fatal(err)
	}
	for _, readOnly := range []bool{false, true} {
		for _, commit := range []bool{false, true} {
			t.Run(fmt.Sprintf("readOnly=%v/commit=%v", readOnly, commit), func(t *testing.T) {
				tx, err := s.beginRecipeTx(ctx, readOnly)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				var statement, lock, read string
				if err = tx.QueryRowContext(ctx, settings).Scan(&statement, &lock, &read); err != nil {
					t.Fatal(err)
				}
				expectedRead := "off"
				if readOnly {
					expectedRead = "on"
				}
				if statement != "10s" || lock != "5s" || read != expectedRead {
					t.Fatalf("transaction bounds: statement=%s lock=%s readOnly=%s", statement, lock, read)
				}
				if commit {
					err = tx.Commit()
				} else {
					err = tx.Rollback()
				}
				if err != nil {
					t.Fatal(err)
				}
				if err = s.db.QueryRowContext(ctx, settings).Scan(&statement, &lock, &read); err != nil {
					t.Fatal(err)
				}
				if statement != baselineStatement || lock != baselineLock || read != baselineReadOnly {
					t.Fatalf("transaction settings leaked: statement=%s lock=%s readOnly=%s", statement, lock, read)
				}
			})
		}
	}
}

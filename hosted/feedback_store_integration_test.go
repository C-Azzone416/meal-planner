//go:build integration

package hosted

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type feedbackObservedGrant struct {
	feedbackUpstream
	observe func()
}

func (u feedbackObservedGrant) Grant(ctx context.Context, action string, payload map[string]any) (feedbackGrant, error) {
	u.observe()
	return u.feedbackUpstream.Grant(ctx, action, payload)
}

// This suite only runs with -tags=integration and explicit opt-in. It creates a
// unique schema in a disposable local database, never uses FRAMEBRIAR_DATABASE_URL,
// and removes only that schema. Do not point it at shared or hosted storage.
func TestFeedbackPostgres(t *testing.T) {
	raw := os.Getenv("FEEDBACK_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set FEEDBACK_TEST_DATABASE_URL to a disposable local PostgreSQL database")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "postgres" || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "") {
		t.Fatal("integration database must be a local postgres URL")
	}
	if u.Hostname() == "" && !strings.HasPrefix(u.Query().Get("host"), "/") {
		t.Fatal("local Unix socket directory required")
	}
	admin, err := sql.Open("postgres", raw)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	schema := "feedback_test_" + strings.ReplaceAll(feedbackRandom(), "-", "")
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("schema cleanup: %v", err)
		}
	}()
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	dsn := u.String()
	open := func() *feedbackSQLStore {
		t.Helper()
		store, err := newFeedbackSQLStore(dsn)
		if err != nil {
			t.Fatal(err)
		}
		s := store.(*feedbackSQLStore)
		t.Cleanup(func() { _ = s.db.Close() })
		return s
	}
	first, second := open(), open()
	migration, err := os.ReadFile("../migrations/001_framebriar.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = first.db.ExecContext(ctx, string(migration)); err != nil {
		t.Fatalf("migration: %v", err)
	}
	read := func(store *feedbackSQLStore, owner string) feedbackRecord {
		t.Helper()
		var out feedbackRecord
		if err := store.WithRecord(ctx, owner, func(r *feedbackRecord) error { out = *r; out.Sealed = bytes.Clone(r.Sealed); return nil }); err != nil {
			t.Fatal(err)
		}
		return out
	}
	t.Run("pending committed before upstream and survives new pool", func(t *testing.T) {
		service, _, upstream := feedbackFixture()
		service.store = first
		upstream.failRedeem = true
		owner := service.owner("synthetic-pending")
		service.upstream = feedbackObservedGrant{upstream, func() {
			// Plain SELECT from another pool must see the committed attempt even
			// while the caller holds its second transaction's row lock.
			var attempt string
			if err := second.db.QueryRowContext(ctx, "SELECT attempt FROM framebriar_enrollments WHERE owner=$1", owner).Scan(&attempt); err != nil || attempt == "" {
				t.Fatalf("upstream contacted before durable attempt: %v", err)
			}
		}}
		if err := service.connect(ctx, owner, "synthetic-invitation"); err == nil {
			t.Fatal("expected uncertain answer")
		}
		pending := read(second, owner)
		if pending.Attempt == "" || pending.AttemptDigest == "" || pending.AttemptAt.IsZero() {
			t.Fatal("pending redemption not durable")
		}
		_ = first.db.Close()
		first = open()
		service.store = first
		if err := service.connect(ctx, owner, "synthetic-invitation"); err != nil {
			t.Fatal(err)
		}
		connected := read(second, owner)
		if connected.Attempt != "" || connected.AttemptDigest != "" || connected.State != "connected" || len(connected.Sealed) == 0 || connected.Generation != pending.Generation {
			t.Fatal("redemption commit or generation failed")
		}
		if len(upstream.attempts) != 2 || upstream.attempts[0] != upstream.attempts[1] {
			t.Fatal("retry did not reuse persisted attempt")
		}
		g, err := service.open(&connected)
		if err != nil || g.Refresh == "" {
			t.Fatal("stored encrypted grant unreadable")
		}
		if bytes.Contains(connected.Sealed, []byte(g.Refresh)) {
			t.Fatal("plaintext secret in bytea")
		}
		if err := service.RevokeAndErase(ctx, owner, true); err != nil {
			t.Fatal(err)
		}
		erased := read(second, owner)
		if len(erased.Sealed) != 0 || erased.KeyID != "" || !erased.Expires.IsZero() || erased.Generation == connected.Generation {
			t.Fatal("revoke/erase generation rotation failed")
		}
	})
	t.Run("independent instances serialize mutation and rate limit", func(t *testing.T) {
		var accepted atomic.Int32
		var wg sync.WaitGroup
		results := make(chan error, 32)
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				store := first
				if i%2 == 0 {
					store = second
				}
				results <- store.WithRecord(ctx, "synthetic-rate", func(r *feedbackRecord) error {
					if r.RateCount >= 12 {
						return feedbackError("RATE_LIMITED", 429)
					}
					r.RateWindow = time.Now().UTC()
					r.RateCount++
					accepted.Add(1)
					return nil
				})
			}(i)
		}
		wg.Wait()
		close(results)
		for err := range results {
			var f feedbackFailure
			if err != nil && !(errors.As(err, &f) && f.Code == "RATE_LIMITED") {
				t.Fatal(err)
			}
		}
		r := read(second, "synthetic-rate")
		if accepted.Load() != 12 || r.RateCount != 12 {
			t.Fatalf("accepted=%d persisted=%d", accepted.Load(), r.RateCount)
		}
		other := read(first, "synthetic-other")
		if other.RateCount != 0 || other.Generation == r.Generation {
			t.Fatal("owner isolation failed")
		}
	})
	t.Run("business errors commit cooldown but SQL failure rolls back", func(t *testing.T) {
		until := time.Now().UTC().Truncate(time.Microsecond).Add(time.Minute)
		err := first.WithRecord(ctx, "synthetic-errors", func(r *feedbackRecord) error {
			r.Cooldown = until
			r.RateCount = 3
			return feedbackError("RATE_LIMITED", 429)
		})
		var f feedbackFailure
		if !errors.As(err, &f) || f.Code != "RATE_LIMITED" {
			t.Fatal(err)
		}
		before := read(second, "synthetic-errors")
		if !before.Cooldown.Equal(until) || before.RateCount != 3 {
			t.Fatal("business result lost durable updates")
		}
		err = first.WithRecord(ctx, "synthetic-errors", func(r *feedbackRecord) error {
			r.RateCount = -1
			r.Generation = feedbackRandom()
			r.Cooldown = time.Time{}
			return nil
		})
		if !errors.Is(err, errFeedbackStorage) || err.Error() != "feedback storage unavailable" {
			t.Fatal("SQL failure must be sanitized")
		}
		after := read(second, "synthetic-errors")
		if after.RateCount != before.RateCount || after.Generation != before.Generation || !after.Cooldown.Equal(before.Cooldown) {
			t.Fatal("failed transaction leaked changes")
		}
	})
	t.Run("canceled transaction and unavailable database fail closed", func(t *testing.T) {
		before := read(first, "synthetic-cancel")
		opCtx, stop := context.WithCancel(ctx)
		err := first.WithRecord(opCtx, "synthetic-cancel", func(r *feedbackRecord) error { r.RateCount = 99; stop(); return nil })
		if !errors.Is(err, errFeedbackStorage) {
			t.Fatal("canceled commit accepted")
		}
		if after := read(second, "synthetic-cancel"); after.RateCount != before.RateCount {
			t.Fatal("canceled update persisted")
		}
		closed := open()
		_ = closed.db.Close()
		called := false
		err = closed.WithRecord(ctx, "synthetic-closed", func(*feedbackRecord) error { called = true; return nil })
		if !errors.Is(err, errFeedbackStorage) || called {
			t.Fatal("unavailable store did not fail closed")
		}
	})
}

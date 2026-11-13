//go:build integration

package hosted

import (
	"bytes"
	"context"
	"errors"
	"os"
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

// This suite owns a fresh Unix-socket-only PostgreSQL cluster. No supplied DSN,
// shared database or schema is modified; only its temporary cluster is removed.
func TestFeedbackPostgres(t *testing.T) {
	if os.Getenv("FEEDBACK_POSTGRES_FIXTURE") != "1" {
		t.Skip("set FEEDBACK_POSTGRES_FIXTURE=1 for a disposable PostgreSQL cluster")
	}
	dsn := feedbackDisposablePostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	open := func() *feedbackSQLStore {
		t.Helper()
		store, err := feedbackTestSQLStore(dsn)
		if err != nil {
			t.Fatal(err)
		}
		s := store.(*feedbackSQLStore)
		t.Cleanup(func() { _ = s.db.Close() })
		return s
	}
	first, second := open(), open()
	first.db.SetMaxOpenConns(1)
	if _, err := first.db.ExecContext(ctx, "CREATE SCHEMA shadow; SET search_path = shadow"); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../migrations/002_framebriar.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = first.db.ExecContext(ctx, string(migration)); err != nil {
		t.Fatalf("migration: %v", err)
	}
	// Deliberately shadow the table on each connection. Qualified application SQL
	// must still use public, including when a temporary relation would win lookup.
	for _, store := range []*feedbackSQLStore{first, second} {
		store.db.SetMaxOpenConns(1)
		if _, err := store.db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS shadow;
CREATE TABLE IF NOT EXISTS shadow.framebriar_enrollments (trap text);
SET search_path = shadow;
CREATE TEMP TABLE framebriar_enrollments (trap text);`); err != nil {
			t.Fatal(err)
		}
	}
	read := func(store *feedbackSQLStore, owner string) feedbackRecord {
		t.Helper()
		var out feedbackRecord
		if err := store.WithRecord(ctx, owner, func(r *feedbackRecord) error { out = *r; out.Sealed = bytes.Clone(r.Sealed); return nil }); err != nil {
			t.Fatal(err)
		}
		return out
	}
	t.Run("qualified storage ignores schema and temporary shadows", func(t *testing.T) {
		const owner = "synthetic-qualified-storage"
		if err := first.WithRecord(ctx, owner, func(r *feedbackRecord) error { r.State = "qualified"; return nil }); err != nil {
			t.Fatal(err)
		}
		if got := read(second, owner); got.State != "qualified" {
			t.Fatalf("wrong persisted state: %q", got.State)
		}
		for _, store := range []*feedbackSQLStore{first, second} {
			var count int
			if err := store.db.QueryRowContext(ctx, "SELECT count(*) FROM pg_temp.framebriar_enrollments").Scan(&count); err != nil || count != 0 {
				t.Fatalf("temporary shadow touched: count=%d err=%v", count, err)
			}
		}
	})
	t.Run("pending committed before upstream and survives new pool", func(t *testing.T) {
		service, _, upstream := feedbackFixture()
		service.store = first
		upstream.failRedeem = true
		owner := service.owner("synthetic-pending")
		service.upstream = feedbackObservedGrant{upstream, func() {
			// Plain SELECT from another pool must see the committed attempt even
			// while the caller holds its second transaction's row lock.
			var attempt string
			if err := second.db.QueryRowContext(ctx, "SELECT attempt FROM public.framebriar_enrollments WHERE owner=$1", owner).Scan(&attempt); err != nil || attempt == "" {
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

package hosted

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// feedbackRecord holds only an opaque scoped owner, encrypted credentials, and
// coordination metadata. Invitations and report contents must never be stored.
// Generation survives reconnects and rotates only during account erasure.
type feedbackRecord struct {
	Owner, Generation, State, KeyID string
	Sealed                          []byte
	Expires                         time.Time
	Attempt, AttemptDigest          string
	AttemptAt, Cooldown             time.Time
	RateWindow                      time.Time
	RateCount                       int
}

// WithRecord serializes all operations for an owner across processes. A callback
// error is a business result: mutations still commit (for example, cooldowns).
// Storage failures take precedence and are returned without database details.
// Commit a pending attempt in a separate call BEFORE contacting Framebriar.
type feedbackStore interface {
	WithRecord(context.Context, string, func(*feedbackRecord) error) error
}

type feedbackSQLStore struct{ db *sql.DB }

var errFeedbackStorage = errors.New("feedback storage unavailable")

func newFeedbackSQLStore(databaseURL string) (feedbackStore, error) {
	if databaseURL == "" {
		return nil, errFeedbackStorage
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, errFeedbackStorage
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(time.Minute)
	// No automatic migration or eager network access. The reviewed migration must
	// be applied by the owner before enabling the feature.
	return &feedbackSQLStore{db: db}, nil
}

func feedbackGeneration() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errFeedbackStorage
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func feedbackNullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

func (s *feedbackSQLStore) WithRecord(ctx context.Context, owner string, fn func(*feedbackRecord) error) error {
	if owner == "" || len(owner) > 256 || fn == nil {
		return errFeedbackStorage
	}
	// Bound lock contention and the entire operation, including a callback's
	// upstream request. The service additionally uses shorter upstream deadlines.
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errFeedbackStorage
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SET LOCAL lock_timeout = '5s'"); err != nil {
		return errFeedbackStorage
	}
	if _, err = tx.ExecContext(ctx, "SET LOCAL statement_timeout = '10s'"); err != nil {
		return errFeedbackStorage
	}
	generation, err := feedbackGeneration()
	if err != nil {
		return errFeedbackStorage
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO framebriar_enrollments (owner, generation)
 VALUES ($1, $2) ON CONFLICT (owner) DO NOTHING`, owner, generation)
	if err != nil {
		return errFeedbackStorage
	}
	r := feedbackRecord{Owner: owner}
	var expires, attemptAt, cooldown, rateWindow sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT generation::text, state, key_id, sealed,
 expires_at, attempt, attempt_at, cooldown_until, rate_window, rate_count, attempt_digest
 FROM framebriar_enrollments WHERE owner = $1 FOR UPDATE`, owner).Scan(
		&r.Generation, &r.State, &r.KeyID, &r.Sealed, &expires, &r.Attempt,
		&attemptAt, &cooldown, &rateWindow, &r.RateCount, &r.AttemptDigest)
	if err != nil {
		return errFeedbackStorage
	}
	r.Expires, r.AttemptAt, r.Cooldown, r.RateWindow = expires.Time, attemptAt.Time, cooldown.Time, rateWindow.Time
	callbackErr := fn(&r)
	// Owner cannot be changed by a callback: the verified caller selected the row.
	_, err = tx.ExecContext(ctx, `UPDATE framebriar_enrollments SET generation=$2,
 state=$3, key_id=$4, sealed=$5, expires_at=$6, attempt=$7, attempt_at=$8,
 cooldown_until=$9, rate_window=$10, rate_count=$11, attempt_digest=$12, updated_at=now() WHERE owner=$1`,
		owner, r.Generation, r.State, r.KeyID, r.Sealed, feedbackNullableTime(r.Expires),
		r.Attempt, feedbackNullableTime(r.AttemptAt), feedbackNullableTime(r.Cooldown),
		feedbackNullableTime(r.RateWindow), r.RateCount, r.AttemptDigest)
	if err != nil {
		return errFeedbackStorage
	}
	if err = tx.Commit(); err != nil {
		return errFeedbackStorage
	}
	return callbackErr
}

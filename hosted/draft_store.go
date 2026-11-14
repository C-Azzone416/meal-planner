package hosted

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

// PostgresDraftStore is an insert-only, scoped-grant submission store. No method
// issues credentials, applies migrations, approves recipes, or deletes content.
type PostgresDraftStore struct{ db *sql.DB }

func OpenDraftStore(_ context.Context, raw string) (*PostgresDraftStore, error) {
	dsn, err := recipeDatabaseURL(raw)
	if err != nil {
		return nil, ErrDraftUnavailable
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, ErrDraftUnavailable
	}
	q := u.Query()
	q.Set("application_name", "meal-planner-draft-ingest")
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		return nil, ErrDraftUnavailable
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(time.Minute)
	return NewPostgresDraftStore(db), nil
}
func NewPostgresDraftStore(db *sql.DB) *PostgresDraftStore { return &PostgresDraftStore{db: db} }
func (s *PostgresDraftStore) Close() error                 { return s.db.Close() }

type draftGrant struct {
	Owner                     RecipeOwner
	Email, Scope, Environment string
	CreatedAt, ExpiresAt      time.Time
	RevokedAt                 sql.NullTime
}

const draftGrantColumns = `owner_issuer,owner_subject,email_at_approval,scope,environment,created_at,expires_at,revoked_at`

func validDraftAccess(a DraftAccess) bool {
	if a.Environment != "preview" || a.Issuer == "" || len(a.Issuer) > 2048 || len(a.AllowedEmails) == 0 || len(a.AllowedEmails) > 100 || a.TokenHash == [32]byte{} {
		return false
	}
	for _, email := range a.AllowedEmails {
		if len(email) > 254 || email == "" {
			return false
		}
	}
	return true
}
func readDraftGrant(row recipeScanner) (draftGrant, error) {
	var g draftGrant
	err := row.Scan(&g.Owner.Issuer, &g.Owner.Subject, &g.Email, &g.Scope, &g.Environment, &g.CreatedAt, &g.ExpiresAt, &g.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrDraftUnauthorized
	}
	if err != nil {
		return g, ErrDraftUnavailable
	}
	return g, nil
}
func draftGrantAllowed(g draftGrant, a DraftAccess, now time.Time) bool {
	if g.Environment != a.Environment || g.Owner.Issuer != a.Issuer || !validRecipeOwner(g.Owner) || g.Scope != "draft:create" || g.RevokedAt.Valid || g.CreatedAt.After(now) || !g.ExpiresAt.After(g.CreatedAt) || g.ExpiresAt.Sub(g.CreatedAt) > 7*24*time.Hour || !g.ExpiresAt.After(now) {
		return false
	}
	for _, email := range a.AllowedEmails {
		if g.Email == email {
			return true
		}
	}
	return false
}
func (s *PostgresDraftStore) Authenticate(ctx context.Context, a DraftAccess) error {
	if !validDraftAccess(a) {
		return ErrDraftUnauthorized
	}
	g, err := readDraftGrant(s.db.QueryRowContext(ctx, `SELECT `+draftGrantColumns+` FROM public.draft_ingest_grants WHERE token_hash=$1`, a.TokenHash[:]))
	if err != nil {
		return err
	}
	var now time.Time
	if err = s.db.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return ErrDraftUnavailable
	}
	if !draftGrantAllowed(g, a, now) {
		return ErrDraftUnauthorized
	}
	return nil
}

const draftMaxRetainedCount = 100
const draftMaxRetainedBytes int64 = 16 * 1024 * 1024
const draftMaxHourlyCount = 20

// Every retained variable field is counted, including the complete immutable
// response (which contains both draft and receipt), canonical request, consumed
// idempotency key, owner/environment namespace and request hash. The per-row
// reserve conservatively bounds fixed metadata and index entries at these input
// limits. This is a logical retained-data quota, not a disk-space guarantee.
const draftRowReserveBytes int64 = 4096

func draftRetainedCharge(a DraftAccess, owner RecipeOwner, key string, request, response []byte) int64 {
	return int64(len(request)+len(response)+len(key)+len(a.Environment)+len(owner.Issuer)+len(owner.Subject)+2*sha256.Size) + draftRowReserveBytes
}
func (s *PostgresDraftStore) Submit(ctx context.Context, a DraftAccess, key string, in DraftSubmission) (DraftResponse, bool, error) {
	var zero DraftResponse
	if !validDraftAccess(a) {
		return zero, false, ErrDraftUnauthorized
	}
	if len(key) == 0 || len(key) > 200 || in.ContentSchemaVersion != 1 || !draft.Validate(in.Content).StructurallyValid {
		return zero, false, ErrDraftUnavailable
	}
	body, err := json.Marshal(in)
	if err != nil {
		return zero, false, ErrDraftUnavailable
	}
	hash := sha256.Sum256(body)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, false, ErrDraftUnavailable
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SET LOCAL statement_timeout='8s'; SET LOCAL lock_timeout='3s'`); err != nil {
		return zero, false, ErrDraftUnavailable
	}
	// SHARE conflicts with UPDATE/DELETE by the administrative revoker. Keep this
	// lock through commit; a revocation committed first cannot be bypassed. Runtime
	// needs UPDATE(last_used_at) solely for this row-lock privilege, not grant edits.
	g, err := readDraftGrant(tx.QueryRowContext(ctx, `SELECT `+draftGrantColumns+` FROM public.draft_ingest_grants WHERE token_hash=$1 FOR SHARE`, a.TokenHash[:]))
	if err != nil {
		return zero, false, err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return zero, false, ErrDraftUnavailable
	}
	if !draftGrantAllowed(g, a, now) {
		return zero, false, ErrDraftUnauthorized
	}
	namespace, _ := json.Marshal([]string{"draft-ingest", a.Environment, g.Owner.Issuer, g.Owner.Subject})
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, string(namespace)); err != nil {
		return zero, false, ErrDraftUnavailable
	}
	// Recheck expiry after owner-lock wait, including replay. Never cache auth.
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return zero, false, ErrDraftUnavailable
	}
	if !draftGrantAllowed(g, a, now) {
		return zero, false, ErrDraftUnauthorized
	}
	var oldHash, originGrant, receipt []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,originating_grant_hash,response FROM public.draft_ingest_submissions WHERE environment=$1 AND owner_issuer=$2 AND owner_subject=$3 AND operation_key=$4`, a.Environment, g.Owner.Issuer, g.Owner.Subject, key).Scan(&oldHash, &originGrant, &receipt)
	if err == nil {
		// A new grant cannot turn a send-only capability into access to an older
		// grant's content, even under the same owner and guessed operation key.
		if !bytes.Equal(a.TokenHash[:], originGrant) {
			return zero, false, ErrDraftUnauthorized
		}
		if !bytes.Equal(hash[:], oldHash) {
			return zero, false, ErrDraftConflict
		}
		if json.Unmarshal(receipt, &zero) != nil {
			return DraftResponse{}, false, ErrDraftUnavailable
		}
		if tx.Commit() != nil {
			return DraftResponse{}, false, ErrDraftUnavailable
		}
		return zero, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return zero, false, ErrDraftUnavailable
	}
	response, err := newDraftResponse(g.Owner, in, now.UTC())
	if err != nil {
		return zero, false, ErrDraftUnavailable
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return zero, false, ErrDraftUnavailable
	}
	charge := draftRetainedCharge(a, g.Owner, key, body, encoded)
	var count, hourCount int
	var retained int64
	err = tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(retained_bytes),0),count(*) FILTER (WHERE created_at > $4::timestamptz - interval '1 hour') FROM public.draft_ingest_submissions WHERE environment=$1 AND owner_issuer=$2 AND owner_subject=$3`, a.Environment, g.Owner.Issuer, g.Owner.Subject, now).Scan(&count, &retained, &hourCount)
	if err != nil {
		return zero, false, ErrDraftUnavailable
	}
	// Include retained grant metadata (also revoked/expired records). Issuance is
	// not implemented here and must independently enforce its own grant cap.
	var grantBytes int64
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(sum(octet_length(token_hash)+octet_length(environment)+octet_length(owner_issuer)+octet_length(owner_subject)+octet_length(email_at_approval)+octet_length(scope)+1024),0) FROM public.draft_ingest_grants WHERE environment=$1 AND owner_issuer=$2 AND owner_subject=$3`, a.Environment, g.Owner.Issuer, g.Owner.Subject).Scan(&grantBytes)
	if err != nil {
		return zero, false, ErrDraftUnavailable
	}
	if grantBytes > draftMaxRetainedBytes-retained {
		return zero, false, ErrDraftQuota
	}
	retained += grantBytes
	if count >= draftMaxRetainedCount || hourCount >= draftMaxHourlyCount || charge > draftMaxRetainedBytes-retained {
		return zero, false, ErrDraftQuota
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.draft_ingest_submissions (environment,owner_issuer,owner_subject,operation_key,request_hash,originating_grant_hash,request,response,retained_bytes,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, a.Environment, g.Owner.Issuer, g.Owner.Subject, key, hash[:], a.TokenHash[:], string(body), string(encoded), charge, now)
	if err != nil {
		return zero, false, ErrDraftUnavailable
	}
	if tx.Commit() != nil {
		return zero, false, ErrDraftUnavailable
	}
	return response, false, nil
}

package hosted

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

type PostgresDraftWorkflowStore struct{ drafts *PostgresDraftStore }

func NewPostgresDraftWorkflowStore(d *PostgresDraftStore) *PostgresDraftWorkflowStore {
	return &PostgresDraftWorkflowStore{drafts: d}
}

func OpenDraftWorkflowStore(ctx context.Context, raw string) (*PostgresDraftWorkflowStore, error) {
	d, err := OpenDraftStore(ctx, raw)
	if err != nil {
		return nil, err
	}
	return NewPostgresDraftWorkflowStore(d), nil
}
func (s *PostgresDraftWorkflowStore) Close() error { return s.drafts.Close() }

func (s *PostgresDraftWorkflowStore) grant(ctx context.Context, a DraftAccess) (draftGrant, error) {
	if err := s.drafts.Authenticate(ctx, a); err != nil {
		return draftGrant{}, err
	}
	query, ok := draftGrantSelect(a, false)
	if !ok {
		return draftGrant{}, ErrDraftUnauthorized
	}
	return readDraftGrantForAccess(s.drafts.db.QueryRowContext(ctx, query, a.TokenHash[:]), a)
}

func (s *PostgresDraftWorkflowStore) CreateIntent(ctx context.Context, a DraftAccess, key string, in DraftSubmission) (DraftIntent, bool, error) {
	var zero DraftIntent
	if !validDraftAccess(a) || !draftKeyPattern.MatchString(key) || in.ContentSchemaVersion != 1 || !draft.Validate(in.Content).StructurallyValid {
		return zero, false, ErrDraftUnavailable
	}
	body, err := json.Marshal(in)
	if err != nil {
		return zero, false, ErrDraftUnavailable
	}
	h := sha256.Sum256(body)
	tx, err := s.drafts.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, false, ErrDraftUnavailable
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SET LOCAL statement_timeout='8s'; SET LOCAL lock_timeout='3s'`); err != nil {
		return zero, false, ErrDraftUnavailable
	}
	grantQuery, ok := draftGrantSelect(a, true)
	if !ok {
		return zero, false, ErrDraftUnauthorized
	}
	g, err := readDraftGrantForAccess(tx.QueryRowContext(ctx, grantQuery, a.TokenHash[:]), a)
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
	namespace, _ := json.Marshal([]string{"draft-intent", a.Environment, g.Owner.Issuer, g.Owner.Subject})
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, string(namespace)); err != nil {
		return zero, false, ErrDraftUnavailable
	}
	// Expiry can pass while waiting for the owner lock. The SHARE row lock also
	// serializes this transaction against administrative revocation.
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return zero, false, ErrDraftUnavailable
	}
	if !draftGrantAllowed(g, a, now) {
		return zero, false, ErrDraftUnauthorized
	}
	var oldHash, oldGrant []byte
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,originating_grant_hash,row_to_json(i)::text FROM public.draft_send_intents i WHERE environment=$1 AND owner_issuer=$2 AND owner_subject=$3 AND operation_key=$4`, a.Environment, g.Owner.Issuer, g.Owner.Subject, key).Scan(&oldHash, &oldGrant, &raw)
	if err == nil {
		if !bytes.Equal(oldGrant, a.TokenHash[:]) {
			return zero, false, ErrDraftUnauthorized
		}
		if !bytes.Equal(oldHash, h[:]) {
			return zero, false, ErrDraftConflict
		}
		intent, err := decodeIntentRowJSON(raw, true)
		if err != nil {
			return zero, false, ErrDraftUnavailable
		}
		if err = tx.Commit(); err != nil {
			return zero, false, ErrDraftUnavailable
		}
		return intent, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return zero, false, ErrDraftUnavailable
	}
	id, err := newRecipeUUID()
	if err != nil {
		return zero, false, ErrDraftUnavailable
	}
	now = now.UTC().Truncate(time.Second)
	expires := now.Add(30 * time.Minute)
	charge := int64(len(body) + len(key) + len(a.Environment) + len(g.Owner.Issuer) + len(g.Owner.Subject) + 64 + 4096)
	var retained int64
	var count, pending, hourly int
	err = tx.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER (WHERE state='pending' AND expires_at>$4),count(*) FILTER (WHERE created_at>$4-interval '1 hour'),COALESCE(sum(retained_bytes),0) FROM public.draft_send_intents WHERE environment=$1 AND owner_issuer=$2 AND owner_subject=$3`, a.Environment, g.Owner.Issuer, g.Owner.Subject, now).Scan(&count, &pending, &hourly, &retained)
	if err != nil {
		return zero, false, ErrDraftUnavailable
	}
	if count >= 100 || pending >= 20 || hourly >= 20 || charge > 16*1024*1024-retained {
		return zero, false, ErrDraftQuota
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.draft_send_intents(environment,owner_issuer,owner_subject,id,operation_key,originating_grant_hash,request_hash,submission,retained_bytes,state,revision,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'pending',1,$10,$11)`, a.Environment, g.Owner.Issuer, g.Owner.Subject, id, key, a.TokenHash[:], h[:], string(body), charge, now, expires)
	if err != nil {
		return zero, false, ErrDraftUnavailable
	}
	if err = tx.Commit(); err != nil {
		return zero, false, ErrDraftUnavailable
	}
	return DraftIntent{ID: id, Revision: 1, State: "pending", ContentHash: hex.EncodeToString(h[:]), CreatedAt: now, ExpiresAt: expires, Submission: in}, false, nil
}

func decodeIntentRowJSON(raw []byte, include bool) (DraftIntent, error) {
	var row struct {
		ID               string          `json:"id"`
		Revision         int64           `json:"revision"`
		State            string          `json:"state"`
		RequestHash      string          `json:"request_hash"`
		Submission       json.RawMessage `json:"submission"`
		CreatedAt        time.Time       `json:"created_at"`
		ExpiresAt        time.Time       `json:"expires_at"`
		DraftID          *string         `json:"draft_id"`
		ReceiptID        *string         `json:"receipt_id"`
		AcceptedRevision *int64          `json:"accepted_revision"`
		SubmittedAt      *time.Time      `json:"submitted_at"`
	}
	if json.Unmarshal(raw, &row) != nil {
		return DraftIntent{}, ErrDraftUnavailable
	}
	// bytea is emitted as a PostgreSQL hex string ("\\x...") by row_to_json.
	h := row.RequestHash
	if len(h) > 2 && h[:2] == `\x` {
		h = h[2:]
	}
	i := DraftIntent{ID: row.ID, Revision: row.Revision, State: row.State, ContentHash: h, CreatedAt: row.CreatedAt.UTC(), ExpiresAt: row.ExpiresAt.UTC(), DraftID: row.DraftID, ReceiptID: row.ReceiptID}
	if row.State == "accepted" && row.AcceptedRevision != nil && row.SubmittedAt != nil {
		i.AcceptedRevision = row.AcceptedRevision
		at := row.SubmittedAt.UTC()
		i.SubmittedAt = &at
	}
	if include && json.Unmarshal(row.Submission, &i.Submission) != nil {
		return DraftIntent{}, ErrDraftUnavailable
	}
	return i, nil
}

const intentSelect = `SELECT id::text,revision,state,request_hash,submission,created_at,expires_at,draft_id::text,receipt_id::text,accepted_revision,submitted_at FROM public.draft_send_intents`

func scanIntent(row recipeScanner, include bool) (DraftIntent, error) {
	var i DraftIntent
	var hash, sub []byte
	var accepted sql.NullInt64
	var submitted sql.NullTime
	err := row.Scan(&i.ID, &i.Revision, &i.State, &hash, &sub, &i.CreatedAt, &i.ExpiresAt, &i.DraftID, &i.ReceiptID, &accepted, &submitted)
	if errors.Is(err, sql.ErrNoRows) {
		return i, ErrDraftIntentNotFound
	}
	if err != nil {
		return i, ErrDraftUnavailable
	}
	i.ContentHash = hex.EncodeToString(hash)
	i.CreatedAt = i.CreatedAt.UTC()
	i.ExpiresAt = i.ExpiresAt.UTC()
	if i.State == "accepted" && accepted.Valid && submitted.Valid {
		value := accepted.Int64
		at := submitted.Time.UTC()
		i.AcceptedRevision = &value
		i.SubmittedAt = &at
	}
	if include && json.Unmarshal(sub, &i.Submission) != nil {
		return DraftIntent{}, ErrDraftUnavailable
	}
	return i, nil
}
func (s *PostgresDraftWorkflowStore) PollIntent(ctx context.Context, a DraftAccess, id string) (DraftIntent, error) {
	if !validRecipeUUID(id) {
		return DraftIntent{}, ErrDraftIntentNotFound
	}
	if _, err := s.grant(ctx, a); err != nil {
		return DraftIntent{}, err
	}
	i, err := scanIntent(s.drafts.db.QueryRowContext(ctx, intentSelect+` WHERE id=$1 AND environment=$2 AND originating_grant_hash=$3`, id, a.Environment, a.TokenHash[:]), false)
	if err != nil {
		return i, err
	}
	if i.State == "pending" && !i.ExpiresAt.After(time.Now()) {
		return DraftIntent{}, ErrDraftIntentExpired
	}
	return i, nil
}
func (s *PostgresDraftWorkflowStore) ListIntents(ctx context.Context, o RecipeOwner) ([]DraftIntent, error) {
	if !validRecipeOwner(o) {
		return nil, ErrDraftIntentNotFound
	}
	rows, err := s.drafts.db.QueryContext(ctx, intentSelect+` WHERE owner_issuer=$1 AND owner_subject=$2 AND state='pending' AND expires_at>clock_timestamp() ORDER BY id LIMIT 101`, o.Issuer, o.Subject)
	if err != nil {
		return nil, ErrDraftUnavailable
	}
	defer rows.Close()
	items := []DraftIntent{}
	for rows.Next() {
		i, err := scanIntent(rows, true)
		if err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if rows.Err() != nil || len(items) > 100 {
		return nil, ErrDraftUnavailable
	}
	return items, nil
}
func (s *PostgresDraftWorkflowStore) GetIntent(ctx context.Context, o RecipeOwner, id string) (DraftIntent, error) {
	if !validRecipeOwner(o) || !validRecipeUUID(id) {
		return DraftIntent{}, ErrDraftIntentNotFound
	}
	return scanIntent(s.drafts.db.QueryRowContext(ctx, intentSelect+` WHERE id=$1 AND owner_issuer=$2 AND owner_subject=$3`, id, o.Issuer, o.Subject), true)
}
func (s *PostgresDraftWorkflowStore) DecideIntent(ctx context.Context, o RecipeOwner, id string, expected int64, hash string, approve bool, a DraftAccess) (DraftIntent, error) {
	if !validRecipeOwner(o) || !validRecipeUUID(id) {
		return DraftIntent{}, ErrDraftIntentNotFound
	}
	tx, err := s.drafts.db.BeginTx(ctx, nil)
	if err != nil {
		return DraftIntent{}, ErrDraftUnavailable
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SET LOCAL statement_timeout='12s'; SET LOCAL lock_timeout='3s'`); err != nil {
		return DraftIntent{}, ErrDraftUnavailable
	}
	// Lock the intent through the complete decision. The hosted submission uses
	// its own retry-safe transaction, but a concurrent deny cannot become final
	// between that submission and this intent transition.
	i, err := scanIntent(tx.QueryRowContext(ctx, intentSelect+` WHERE id=$1 AND owner_issuer=$2 AND owner_subject=$3 FOR UPDATE`, id, o.Issuer, o.Subject), true)
	if err != nil {
		return i, err
	}
	if i.ContentHash != hash {
		return DraftIntent{}, ErrDraftRevision
	}
	if i.State == "denied" && i.Revision == expected+1 && !approve {
		return i, nil
	}
	if i.State == "accepted" && i.Revision == expected+1 && approve {
		return i, nil
	}
	if i.Revision != expected {
		return DraftIntent{}, ErrDraftRevision
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return DraftIntent{}, ErrDraftUnavailable
	}
	if !i.ExpiresAt.After(now) {
		return DraftIntent{}, ErrDraftIntentExpired
	}
	if !approve {
		res, err := tx.ExecContext(ctx, `UPDATE public.draft_send_intents SET state='denied',revision=revision+1,decided_at=clock_timestamp() WHERE id=$1 AND owner_issuer=$2 AND owner_subject=$3 AND state='pending' AND revision=$4`, id, o.Issuer, o.Subject, expected)
		if err != nil {
			return DraftIntent{}, ErrDraftUnavailable
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return DraftIntent{}, ErrDraftRevision
		}
		if err = tx.Commit(); err != nil {
			return DraftIntent{}, ErrDraftUnavailable
		}
		return s.GetIntent(ctx, o, id)
	}
	// The stored grant hash, not browser input, selects the exact proposal grant.
	var grantHash []byte
	err = tx.QueryRowContext(ctx, `SELECT originating_grant_hash FROM public.draft_send_intents WHERE id=$1 AND owner_issuer=$2 AND owner_subject=$3`, id, o.Issuer, o.Subject).Scan(&grantHash)
	if err != nil || len(grantHash) != 32 {
		return DraftIntent{}, ErrDraftUnavailable
	}
	copy(a.TokenHash[:], grantHash)
	a.Environment = "preview"
	result, _, err := s.drafts.submitTx(ctx, tx, a, id, i.Submission)
	if err != nil {
		return DraftIntent{}, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE public.draft_send_intents SET state='accepted',revision=revision+1,decided_at=clock_timestamp(),draft_id=$5,receipt_id=$6,accepted_revision=$7,submitted_at=$8 WHERE id=$1 AND owner_issuer=$2 AND owner_subject=$3 AND state='pending' AND revision=$4`, id, o.Issuer, o.Subject, expected, result.Draft.ID, result.Receipt.ID, result.Receipt.AcceptedRevision, result.Receipt.SubmittedAt)
	if err != nil {
		return DraftIntent{}, ErrDraftUnavailable
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return DraftIntent{}, ErrDraftRevision
	}
	if err = tx.Commit(); err != nil {
		return DraftIntent{}, ErrDraftUnavailable
	}
	return s.GetIntent(ctx, o, id)
}

func overlayReview(r DraftResponse, revision int64, state string, recipeID *string) DraftResponse {
	if revision > 0 {
		r.Draft.Review.Revision = revision
		r.Draft.Review.State = state
		r.Draft.Review.DraftRevision = r.Draft.Revision
		r.Draft.ApprovedRecipeID = recipeID
		if state == "approved" {
			r.Draft.Status = "approved"
		}
	}
	return r
}
func scanHosted(row recipeScanner) (DraftResponse, error) {
	var raw []byte
	var review sql.NullInt64
	var state sql.NullString
	var recipeID sql.NullString
	err := row.Scan(&raw, &review, &state, &recipeID)
	if errors.Is(err, sql.ErrNoRows) {
		return DraftResponse{}, ErrRecipeNotFound
	}
	if err != nil {
		return DraftResponse{}, ErrDraftUnavailable
	}
	r, err := decodeHostedResponse(raw)
	if err != nil {
		return r, err
	}
	var p *string
	if recipeID.Valid {
		p = &recipeID.String
	}
	if review.Valid {
		r = overlayReview(r, review.Int64, state.String, p)
	}
	return r, nil
}

func decodeHostedResponse(raw []byte) (DraftResponse, error) {
	var versions struct {
		Draft struct {
			SchemaVersion        json.RawMessage `json:"schema_version"`
			ContentSchemaVersion json.RawMessage `json:"content_schema_version"`
		} `json:"draft"`
	}
	if json.Unmarshal(raw, &versions) != nil {
		return DraftResponse{}, ErrDraftUnavailable
	}
	if !bytes.Equal(bytes.TrimSpace(versions.Draft.SchemaVersion), []byte("1")) || !bytes.Equal(bytes.TrimSpace(versions.Draft.ContentSchemaVersion), []byte("1")) {
		return DraftResponse{}, ErrDraftUnsupported
	}
	var response DraftResponse
	if json.Unmarshal(raw, &response) != nil {
		return DraftResponse{}, ErrDraftUnavailable
	}
	return response, nil
}
func (s *PostgresDraftWorkflowStore) hostedQuery(ctx context.Context, o RecipeOwner, id string) (DraftResponse, error) {
	return scanHosted(s.drafts.db.QueryRowContext(ctx, `SELECT s.response,rv.revision,rv.state,rv.approved_recipe_id::text FROM public.draft_ingest_submissions s LEFT JOIN public.draft_reviews rv ON rv.environment=s.environment AND rv.owner_issuer=s.owner_issuer AND rv.owner_subject=s.owner_subject AND rv.draft_id=(s.response->'draft'->>'id')::uuid WHERE s.environment='preview' AND s.owner_issuer=$1 AND s.owner_subject=$2 AND s.response->'draft'->>'id'=$3`, o.Issuer, o.Subject, id))
}
func (s *PostgresDraftWorkflowStore) GetHostedDraft(ctx context.Context, o RecipeOwner, id string) (DraftResponse, error) {
	if !validRecipeOwner(o) || !validRecipeUUID(id) {
		return DraftResponse{}, ErrRecipeNotFound
	}
	return s.hostedQuery(ctx, o, id)
}
func (s *PostgresDraftWorkflowStore) ListHostedDrafts(ctx context.Context, o RecipeOwner) ([]HostedDraftSummary, error) {
	if !validRecipeOwner(o) {
		return nil, ErrRecipeNotFound
	}
	rows, err := s.drafts.db.QueryContext(ctx, `SELECT s.response,rv.revision,rv.state,rv.approved_recipe_id::text FROM public.draft_ingest_submissions s LEFT JOIN public.draft_reviews rv ON rv.environment=s.environment AND rv.owner_issuer=s.owner_issuer AND rv.owner_subject=s.owner_subject AND rv.draft_id=(s.response->'draft'->>'id')::uuid WHERE s.environment='preview' AND s.owner_issuer=$1 AND s.owner_subject=$2 AND COALESCE(rv.state,'not_started')<>'approved' ORDER BY (s.response->'draft'->>'id')::uuid LIMIT 101`, o.Issuer, o.Subject)
	if err != nil {
		return nil, ErrDraftUnavailable
	}
	defer rows.Close()
	items := []HostedDraftSummary{}
	for rows.Next() {
		r, err := scanHosted(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, HostedDraftSummary{ID: r.Draft.ID, Title: r.Draft.Content.Title, Status: r.Draft.Status, Revision: r.Draft.Revision, CreatedAt: r.Draft.CreatedAt, UpdatedAt: r.Draft.UpdatedAt, ReviewState: r.Draft.Review.State, ReviewRevision: r.Draft.Review.Revision})
	}
	if rows.Err() != nil || len(items) > 100 {
		return nil, ErrDraftUnavailable
	}
	return items, nil
}
func (s *PostgresDraftWorkflowStore) ReviewHostedDraft(ctx context.Context, o RecipeOwner, id string, draftRev, reviewRev int64) (DraftResponse, error) {
	r, err := s.GetHostedDraft(ctx, o, id)
	if err != nil {
		return r, err
	}
	if r.Draft.Revision != draftRev || r.Draft.Review.Revision != reviewRev || r.Draft.Review.State != "not_started" {
		return DraftResponse{}, ErrDraftRevision
	}
	res, err := s.drafts.db.ExecContext(ctx, `INSERT INTO public.draft_reviews(environment,owner_issuer,owner_subject,draft_id,revision,state,draft_revision,updated_at) VALUES('preview',$1,$2,$3,2,'ready',$4,clock_timestamp()) ON CONFLICT(environment,owner_issuer,owner_subject,draft_id) DO UPDATE SET revision=public.draft_reviews.revision+1,state='ready',draft_revision=$4,updated_at=clock_timestamp() WHERE public.draft_reviews.revision=$5 AND public.draft_reviews.state<>'approved'`, o.Issuer, o.Subject, id, draftRev, reviewRev)
	if err != nil {
		return DraftResponse{}, ErrDraftUnavailable
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return DraftResponse{}, ErrDraftRevision
	}
	return s.GetHostedDraft(ctx, o, id)
}
func (s *PostgresDraftWorkflowStore) MarkHostedDraftApproved(ctx context.Context, o RecipeOwner, id string, draftRev, reviewRev int64, recipeID string) (DraftResponse, error) {
	if !validRecipeUUID(recipeID) {
		return DraftResponse{}, ErrDraftUnavailable
	}
	r, err := s.GetHostedDraft(ctx, o, id)
	if err != nil {
		return r, err
	}
	if r.Draft.Revision != draftRev {
		return DraftResponse{}, ErrDraftRevision
	}
	if r.Draft.Review.State == "approved" {
		if r.Draft.ApprovedRecipeID != nil && *r.Draft.ApprovedRecipeID == recipeID {
			return r, nil
		}
		return DraftResponse{}, ErrDraftRevision
	}
	if r.Draft.Review.State != "ready" || r.Draft.Review.Revision != reviewRev {
		return DraftResponse{}, ErrDraftRevision
	}
	res, err := s.drafts.db.ExecContext(ctx, `UPDATE public.draft_reviews SET revision=revision+1,state='approved',approved_recipe_id=$5,updated_at=clock_timestamp() WHERE environment='preview' AND owner_issuer=$1 AND owner_subject=$2 AND draft_id=$3 AND revision=$4 AND state='ready'`, o.Issuer, o.Subject, id, reviewRev, recipeID)
	if err != nil {
		return DraftResponse{}, ErrDraftUnavailable
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return DraftResponse{}, ErrDraftRevision
	}
	return s.GetHostedDraft(ctx, o, id)
}

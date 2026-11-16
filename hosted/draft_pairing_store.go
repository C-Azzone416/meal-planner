package hosted

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"time"
)

const (
	pairingLifetime        = 10 * time.Minute
	grantLifetime          = 7 * 24 * time.Hour
	maxActiveDraftGrants   = 5
	maxRetainedDraftGrants = 50
)

type PostgresDraftPairingStore struct{ db *sql.DB }

func draftGrantOwnerLockKey(environment, issuer, subject string) (string, error) {
	namespace, err := json.Marshal([]string{"draft-grant-owner", environment, issuer, subject})
	if err != nil {
		return "", err
	}
	return string(namespace), nil
}

func OpenDraftPairingStore(_ context.Context, raw string) (*PostgresDraftPairingStore, error) {
	dsn, err := recipeDatabaseURL(raw)
	if err != nil {
		return nil, ErrDraftUnavailable
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, ErrDraftUnavailable
	}
	q := u.Query()
	q.Set("application_name", "meal-planner-draft-pairing")
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		return nil, ErrDraftUnavailable
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(time.Minute)
	return &PostgresDraftPairingStore{db: db}, nil
}

func (s *PostgresDraftPairingStore) Close() error { return s.db.Close() }

func randomBase64URL(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

const pairingAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

func randomUserCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = pairingAlphabet[int(b[i])&(len(pairingAlphabet)-1)]
	}
	return string(b), nil
}

func (s *PostgresDraftPairingStore) CreatePairing(ctx context.Context, c DraftPairingConfig, in DraftPairingCreateRequest) (DraftPairingCreated, error) {
	if !validPairingConfig(c) || in.CodeChallengeMethod != "S256" || !pairingChallengePattern.MatchString(in.CodeChallenge) || !validClientLabel(in.ClientLabel) {
		return DraftPairingCreated{}, ErrDraftPairingInvalid
	}
	for attempt := 0; attempt < 3; attempt++ {
		id, err := newRecipeUUID()
		if err != nil {
			return DraftPairingCreated{}, ErrDraftUnavailable
		}
		poll, err := randomBase64URL(32)
		if err != nil {
			return DraftPairingCreated{}, ErrDraftUnavailable
		}
		code, err := randomUserCode()
		if err != nil {
			return DraftPairingCreated{}, ErrDraftUnavailable
		}
		h := sha256.Sum256([]byte(poll))
		var now time.Time
		err = s.db.QueryRowContext(ctx, `WITH instant AS (SELECT clock_timestamp() AS now)
 INSERT INTO public.draft_pairings
 (id,environment,issuer,user_code,poll_token_hash,code_challenge,challenge_fingerprint,client_label,status,created_at,expires_at)
 SELECT $1,$2,$3,$4,$5,$6,$7,$8,'pending',now,now+interval '10 minutes' FROM instant
 RETURNING created_at`, id, c.Environment, c.Issuer, code, h[:], in.CodeChallenge, challengeFingerprint(in.CodeChallenge), in.ClientLabel).Scan(&now)
		if err != nil {
			continue
		}
		return DraftPairingCreated{PairingID: id, PollToken: poll, UserCode: code, VerificationURI: c.WebOrigin + "/connect", ExpiresIn: int(pairingLifetime / time.Second), Interval: 5, ChallengeFingerprint: challengeFingerprint(in.CodeChallenge)}, nil
	}
	return DraftPairingCreated{}, ErrDraftUnavailable
}

type pairingRow struct {
	PollHash                                             []byte
	Challenge, Fingerprint, Label, Status                string
	CreatedAt, ExpiresAt                                 time.Time
	LastPolled                                           sql.NullTime
	OwnerIssuer, OwnerSubject, OwnerEmail, ApprovalEpoch sql.NullString
}

func scanPairing(row recipeScanner) (pairingRow, error) {
	var p pairingRow
	err := row.Scan(&p.PollHash, &p.Challenge, &p.Fingerprint, &p.Label, &p.Status, &p.CreatedAt, &p.ExpiresAt, &p.LastPolled, &p.OwnerIssuer, &p.OwnerSubject, &p.OwnerEmail, &p.ApprovalEpoch)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrDraftPairingMissing
	}
	if err != nil {
		return p, ErrDraftUnavailable
	}
	return p, nil
}

func (s *PostgresDraftPairingStore) PollPairing(ctx context.Context, c DraftPairingConfig, in DraftPairingPollRequest) (DraftPairingPollResult, error) {
	var out DraftPairingPollResult
	if !validPairingConfig(c) || !recipeUUID.MatchString(in.PairingID) || !pairingSecretPattern.MatchString(in.PollToken) || !pairingVerifierPattern.MatchString(in.CodeVerifier) {
		return out, ErrDraftPairingInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, ErrDraftUnavailable
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SET LOCAL statement_timeout='8s'; SET LOCAL lock_timeout='3s'`); err != nil {
		return out, ErrDraftUnavailable
	}
	p, err := scanPairing(tx.QueryRowContext(ctx, `SELECT poll_token_hash,code_challenge,challenge_fingerprint,client_label,status,created_at,expires_at,last_polled_at,owner_issuer,owner_subject,owner_email,approval_epoch FROM public.draft_pairings WHERE id=$1 AND environment=$2 AND issuer=$3 FOR UPDATE`, in.PairingID, c.Environment, c.Issuer))
	if err != nil {
		return out, err
	}
	pollHash := sha256.Sum256([]byte(in.PollToken))
	verifierHash := sha256.Sum256([]byte(in.CodeVerifier))
	expectedChallenge := base64.RawURLEncoding.EncodeToString(verifierHash[:])
	if subtle.ConstantTimeCompare(pollHash[:], p.PollHash) != 1 || subtle.ConstantTimeCompare([]byte(expectedChallenge), []byte(p.Challenge)) != 1 {
		return out, ErrDraftPairingInvalid
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return out, ErrDraftUnavailable
	}
	if !p.ExpiresAt.After(now) || p.Status == "consumed" || p.Status == "denied" {
		return out, ErrDraftPairingGone
	}
	if p.LastPolled.Valid && now.Sub(p.LastPolled.Time) < 5*time.Second {
		_, _ = tx.ExecContext(ctx, `UPDATE public.draft_pairings SET last_polled_at=$2 WHERE id=$1`, in.PairingID, now)
		if tx.Commit() != nil {
			return out, ErrDraftUnavailable
		}
		return out, ErrDraftPairingSlowDown
	}
	if _, err = tx.ExecContext(ctx, `UPDATE public.draft_pairings SET last_polled_at=$2 WHERE id=$1`, in.PairingID, now); err != nil {
		return out, ErrDraftUnavailable
	}
	if p.Status == "pending" {
		if tx.Commit() != nil {
			return out, ErrDraftUnavailable
		}
		return out, ErrDraftPairingPending
	}
	if p.Status != "approved" || !p.OwnerIssuer.Valid || !p.OwnerSubject.Valid || !p.OwnerEmail.Valid || !p.ApprovalEpoch.Valid || p.ApprovalEpoch.String != c.MembershipEpoch {
		return out, ErrDraftPairingGone
	}
	ownerKey, err := draftGrantOwnerLockKey(c.Environment, p.OwnerIssuer.String, p.OwnerSubject.String)
	if err != nil {
		return out, ErrDraftUnavailable
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, ownerKey); err != nil {
		return out, ErrDraftUnavailable
	}
	var active, retained int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE revoked_at IS NULL AND expires_at>$4 AND membership_epoch=$5),count(*) FROM public.draft_send_grants WHERE environment=$1 AND owner_issuer=$2 AND owner_subject=$3`, c.Environment, p.OwnerIssuer.String, p.OwnerSubject.String, now, c.MembershipEpoch).Scan(&active, &retained); err != nil {
		return out, ErrDraftUnavailable
	}
	if active >= maxActiveDraftGrants || retained >= maxRetainedDraftGrants {
		return out, ErrDraftGrantLimit
	}
	grantID, err := newRecipeUUID()
	if err != nil {
		return out, ErrDraftUnavailable
	}
	grant, err := randomBase64URL(32)
	if err != nil {
		return out, ErrDraftUnavailable
	}
	grant = "mp_lc1_pv_" + grant
	grantHash := sha256.Sum256([]byte(grant))
	expires := now.Add(grantLifetime)
	_, err = tx.ExecContext(ctx, `INSERT INTO public.draft_send_grants
 (id,token_hash,environment,owner_issuer,owner_subject,email_at_approval,membership_epoch,client_label,scope,created_at,expires_at)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'draft:propose',$9,$10)`, grantID, grantHash[:], c.Environment, p.OwnerIssuer.String, p.OwnerSubject.String, p.OwnerEmail.String, c.MembershipEpoch, p.Label, now, expires)
	if err != nil {
		return out, ErrDraftUnavailable
	}
	result, err := tx.ExecContext(ctx, `UPDATE public.draft_pairings SET status='consumed',consumed_at=$2,user_code=NULL WHERE id=$1 AND status='approved'`, in.PairingID, now)
	if err != nil {
		return out, ErrDraftUnavailable
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return out, ErrDraftPairingGone
	}
	if err = tx.Commit(); err != nil {
		return out, ErrDraftUnavailable
	}
	return DraftPairingPollResult{State: "approved", Environment: c.Environment, APIOrigin: c.APIOrigin, WebOrigin: c.WebOrigin, BindingID: grantID, Grant: grant, ExpiresAt: expires.UTC().Format(time.RFC3339), OwnerLabel: p.OwnerEmail.String, Scope: "draft:propose"}, nil
}

func (s *PostgresDraftPairingStore) ApprovePairing(ctx context.Context, c DraftPairingConfig, identity UserIdentity, in DraftPairingApproveRequest) error {
	if !validPairingConfig(c) || identity.Issuer != c.Issuer || identity.Subject == "" || identity.Email == "" || !pairingCodePattern.MatchString(in.UserCode) || !pairingFingerprint.MatchString(in.ChallengeFingerprint) {
		return ErrDraftPairingInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrDraftUnavailable
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SET LOCAL statement_timeout='8s'; SET LOCAL lock_timeout='3s'`); err != nil {
		return ErrDraftUnavailable
	}
	var id, fingerprint, status string
	var expires time.Time
	err = tx.QueryRowContext(ctx, `SELECT id,challenge_fingerprint,status,expires_at FROM public.draft_pairings WHERE environment=$1 AND issuer=$2 AND user_code=$3 FOR UPDATE`, c.Environment, c.Issuer, in.UserCode).Scan(&id, &fingerprint, &status, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDraftPairingMissing
	}
	if err != nil {
		return ErrDraftUnavailable
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return ErrDraftUnavailable
	}
	if status != "pending" || !expires.After(now) {
		return ErrDraftPairingGone
	}
	if subtle.ConstantTimeCompare([]byte(fingerprint), []byte(in.ChallengeFingerprint)) != 1 {
		return ErrDraftPairingMissing
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.draft_membership_epochs (environment,owner_issuer,owner_subject,membership_epoch,email,updated_at)
 VALUES ($1,$2,$3,$4,$5,$6)
 ON CONFLICT (environment,owner_issuer,owner_subject) DO UPDATE SET membership_epoch=EXCLUDED.membership_epoch,email=EXCLUDED.email,updated_at=EXCLUDED.updated_at`, c.Environment, identity.Issuer, identity.Subject, c.MembershipEpoch, identity.Email, now)
	if err != nil {
		return ErrDraftUnavailable
	}
	result, err := tx.ExecContext(ctx, `UPDATE public.draft_pairings SET status='approved',approved_at=$2,owner_issuer=$3,owner_subject=$4,owner_email=$5,approval_epoch=$6 WHERE id=$1 AND status='pending'`, id, now, identity.Issuer, identity.Subject, identity.Email, c.MembershipEpoch)
	if err != nil {
		return ErrDraftUnavailable
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ErrDraftPairingGone
	}
	if err = tx.Commit(); err != nil {
		return ErrDraftUnavailable
	}
	return nil
}

func (s *PostgresDraftPairingStore) ListDraftGrants(ctx context.Context, c DraftPairingConfig, owner RecipeOwner) ([]DraftGrantSummary, error) {
	if !validPairingConfig(c) || !validRecipeOwner(owner) || owner.Issuer != c.Issuer {
		return nil, ErrDraftPairingInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,client_label,scope,created_at,expires_at,revoked_at,membership_epoch,clock_timestamp() FROM public.draft_send_grants WHERE environment=$1 AND owner_issuer=$2 AND owner_subject=$3 ORDER BY created_at DESC,id DESC LIMIT 100`, c.Environment, owner.Issuer, owner.Subject)
	if err != nil {
		return nil, ErrDraftUnavailable
	}
	defer rows.Close()
	items := []DraftGrantSummary{}
	for rows.Next() {
		var item DraftGrantSummary
		var created, expires, now time.Time
		var revoked sql.NullTime
		var epoch string
		if rows.Scan(&item.ID, &item.ClientLabel, &item.Scope, &created, &expires, &revoked, &epoch, &now) != nil {
			return nil, ErrDraftUnavailable
		}
		item.CreatedAt, item.ExpiresAt = created.UTC().Format(time.RFC3339), expires.UTC().Format(time.RFC3339)
		if revoked.Valid {
			v := revoked.Time.UTC().Format(time.RFC3339)
			item.RevokedAt = &v
		}
		item.Active = !revoked.Valid && expires.After(now) && epoch == c.MembershipEpoch
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, ErrDraftUnavailable
	}
	return items, nil
}

func (s *PostgresDraftPairingStore) RevokeDraftGrant(ctx context.Context, c DraftPairingConfig, owner RecipeOwner, id string) error {
	if !validPairingConfig(c) || !validRecipeOwner(owner) || owner.Issuer != c.Issuer || !recipeUUID.MatchString(id) {
		return ErrDraftPairingInvalid
	}
	var got string
	err := s.db.QueryRowContext(ctx, `UPDATE public.draft_send_grants SET revoked_at=COALESCE(revoked_at,clock_timestamp()) WHERE id=$1 AND environment=$2 AND owner_issuer=$3 AND owner_subject=$4 RETURNING id`, id, c.Environment, owner.Issuer, owner.Subject).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDraftPairingMissing
	}
	if err != nil {
		return ErrDraftUnavailable
	}
	return nil
}

type DraftProposalGrant struct {
	ID          string
	Owner       RecipeOwner
	ClientLabel string
	ExpiresAt   time.Time
}

// AuthenticateDraftProposal is the narrow server-side seam for the later send
// intent endpoint. It never returns the token and cannot approve or read drafts.
func (s *PostgresDraftPairingStore) AuthenticateDraftProposal(ctx context.Context, c DraftPairingConfig, tokenHash [32]byte) (DraftProposalGrant, error) {
	var g DraftProposalGrant
	var environment, epoch, scope string
	var revoked sql.NullTime
	var now time.Time
	err := s.db.QueryRowContext(ctx, `SELECT id,owner_issuer,owner_subject,client_label,expires_at,environment,membership_epoch,scope,revoked_at,clock_timestamp() FROM public.draft_send_grants WHERE token_hash=$1`, tokenHash[:]).Scan(&g.ID, &g.Owner.Issuer, &g.Owner.Subject, &g.ClientLabel, &g.ExpiresAt, &environment, &epoch, &scope, &revoked, &now)
	if errors.Is(err, sql.ErrNoRows) {
		return DraftProposalGrant{}, ErrDraftUnauthorized
	}
	if err != nil {
		return DraftProposalGrant{}, ErrDraftUnavailable
	}
	if !validPairingConfig(c) || environment != c.Environment || g.Owner.Issuer != c.Issuer || epoch != c.MembershipEpoch || scope != "draft:propose" || revoked.Valid || !g.ExpiresAt.After(now) {
		return DraftProposalGrant{}, ErrDraftUnauthorized
	}
	return g, nil
}

package hosted

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// PostgresRecipeStore never migrates the database. Use a least-privilege runtime role.
type PostgresRecipeStore struct{ db *sql.DB }

// OpenRecipeStore validates server-only TLS settings and initializes a bounded pool.
// Connection errors deliberately omit credentials and connection strings.
func OpenRecipeStore(_ context.Context, databaseURL string) (*PostgresRecipeStore, error) {
	dsn, err := recipeDatabaseURL(databaseURL)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, errors.New("invalid recipe database configuration")
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(time.Minute)
	return NewPostgresRecipeStore(db), nil
}
func recipeDatabaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.User == nil || u.User.Username() == "" || u.Path == "" || u.Path == "/" || u.Fragment != "" {
		return "", errors.New("invalid recipe database URL")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || q.Get("sslmode") != "verify-full" {
		return "", errors.New("recipe database requires verified TLS")
	}
	// Prevent query parameters from overriding host, credentials, or TLS policy.
	for k, v := range q {
		if len(v) != 1 || (k != "sslmode" && k != "sslrootcert") {
			return "", errors.New("unsupported recipe database settings")
		}
	}
	q.Set("connect_timeout", "5")
	q.Set("application_name", "meal-planner-recipes")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// NewPostgresRecipeStore accepts an already configured pool, primarily for tests.
func NewPostgresRecipeStore(db *sql.DB) *PostgresRecipeStore { return &PostgresRecipeStore{db: db} }
func (s *PostgresRecipeStore) Close() error                  { return s.db.Close() }

// beginRecipeTx bounds every operation without sending unsupported startup
// parameters to Neon/PgBouncer. SET LOCAL is transaction-scoped, so settings
// cannot leak to another request when the backend connection returns to a pool.
func (s *PostgresRecipeStore) beginRecipeTx(ctx context.Context, readOnly bool) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SET LOCAL statement_timeout = '10s'; SET LOCAL lock_timeout = '5s'`); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func recipePayload(c RecipeContent) ([]byte, []byte, error) {
	b, e := json.Marshal(c)
	if e != nil {
		return nil, nil, e
	}
	h := sha256.Sum256(b)
	return b, h[:], nil
}
func newRecipeUUID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}
func validRecipeUUID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	_, e := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	return e == nil
}
func validRecipeOwner(o RecipeOwner) bool {
	return o.Issuer != "" && len(o.Issuer) <= 2048 && o.Subject != "" && len(o.Subject) <= 512
}

const recipeColumns = `id::text, revision, status, origin, created_at, updated_at, content`

type recipeScanner interface{ Scan(...any) error }

func scanRecipe(row recipeScanner) (RecipeEnvelope, error) {
	var r RecipeEnvelope
	var b []byte
	e := row.Scan(&r.ID, &r.Revision, &r.Status, &r.Origin, &r.CreatedAt, &r.UpdatedAt, &b)
	if errors.Is(e, sql.ErrNoRows) {
		return r, ErrRecipeNotFound
	}
	if e != nil {
		return r, e
	}
	// Canonical UTC keeps fresh SQL results and JSON retry receipts identical.
	r.CreatedAt = r.CreatedAt.UTC()
	r.UpdatedAt = r.UpdatedAt.UTC()
	e = json.Unmarshal(b, &r.Content)
	return r, e
}
func (s *PostgresRecipeStore) Create(ctx context.Context, o RecipeOwner, key string, c RecipeContent) (RecipeEnvelope, bool, error) {
	var z RecipeEnvelope
	if !validRecipeOwner(o) || key == "" || len(key) > 200 {
		return z, false, errors.New("invalid recipe create")
	}
	if err := ValidateRecipeContent(c); err != nil {
		return z, false, err
	}
	body, hash, e := recipePayload(c)
	if e != nil {
		return z, false, e
	}
	tx, e := s.beginRecipeTx(ctx, false)
	if e != nil {
		return z, false, e
	}
	defer tx.Rollback()
	// Serialize account creates including absent receipt keys. Hash collisions can
	// only serialize unrelated accounts, never bypass ownership. JSON frames the
	// issuer/subject pair without an ambiguous delimiter or Postgres NUL byte.
	identity, _ := json.Marshal([]string{o.Issuer, o.Subject})
	_, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, string(identity))
	if e != nil {
		return z, false, e
	}
	var oldHash, receipt []byte
	e = tx.QueryRowContext(ctx, `SELECT request_hash,receipt FROM meal_planner.recipe_creates WHERE owner_issuer=$1 AND owner_subject=$2 AND operation_key=$3`, o.Issuer, o.Subject, key).Scan(&oldHash, &receipt)
	if e == nil {
		if !bytes.Equal(hash, oldHash) {
			return z, false, ErrRecipeIdempotencyConflict
		}
		if e = json.Unmarshal(receipt, &z); e != nil {
			return z, false, e
		}
		return z, true, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return z, false, e
	}
	id, e := newRecipeUUID()
	if e != nil {
		return z, false, e
	}
	saved, e := scanRecipe(tx.QueryRowContext(ctx, `INSERT INTO meal_planner.recipes (id,owner_issuer,owner_subject,status,origin,content) VALUES ($1,$2,$3,'approved','manual',$4) RETURNING `+recipeColumns, id, o.Issuer, o.Subject, string(body)))
	if e != nil {
		return z, false, e
	}
	receipt, e = json.Marshal(saved)
	if e != nil {
		return z, false, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO meal_planner.recipe_creates (owner_issuer,owner_subject,operation_key,request_hash,recipe_id,receipt) VALUES ($1,$2,$3,$4,$5,$6)`, o.Issuer, o.Subject, key, hash, id, string(receipt))
	if e != nil {
		return z, false, e
	}
	if e = tx.Commit(); e != nil {
		return z, false, e
	}
	return saved, false, nil
}
func (s *PostgresRecipeStore) Get(ctx context.Context, o RecipeOwner, id string) (RecipeEnvelope, error) {
	if !validRecipeOwner(o) || !validRecipeUUID(id) {
		return RecipeEnvelope{}, ErrRecipeNotFound
	}
	tx, err := s.beginRecipeTx(ctx, true)
	if err != nil {
		return RecipeEnvelope{}, err
	}
	defer tx.Rollback()
	result, err := scanRecipe(tx.QueryRowContext(ctx, `SELECT `+recipeColumns+` FROM meal_planner.recipes WHERE owner_issuer=$1 AND owner_subject=$2 AND id=$3 AND status='approved' AND origin='manual'`, o.Issuer, o.Subject, id))
	if err != nil {
		return RecipeEnvelope{}, err
	}
	if err = tx.Commit(); err != nil {
		return RecipeEnvelope{}, err
	}
	return result, nil
}
func (s *PostgresRecipeStore) List(ctx context.Context, o RecipeOwner, after string, limit int) ([]RecipeEnvelope, string, error) {
	if !validRecipeOwner(o) || limit < 1 || limit > 100 || (after != "" && !validRecipeUUID(after)) {
		return nil, "", errors.New("invalid recipe page")
	}
	// Stable ID ordering across edits; pagination is not a snapshot of concurrent inserts.
	q := `SELECT ` + recipeColumns + ` FROM meal_planner.recipes WHERE owner_issuer=$1 AND owner_subject=$2 AND status='approved' AND origin='manual'`
	args := []any{o.Issuer, o.Subject, limit + 1}
	if after != "" {
		q += ` AND id>$4::uuid`
		args = append(args, after)
	}
	q += ` ORDER BY id LIMIT $3`
	tx, e := s.beginRecipeTx(ctx, true)
	if e != nil {
		return nil, "", e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, q, args...)
	if e != nil {
		return nil, "", e
	}
	defer rows.Close()
	out := make([]RecipeEnvelope, 0, limit+1)
	for rows.Next() {
		r, e := scanRecipe(rows)
		if e != nil {
			return nil, "", e
		}
		out = append(out, r)
	}
	if e = rows.Err(); e != nil {
		return nil, "", e
	}
	if e = rows.Close(); e != nil {
		return nil, "", e
	}
	if e = tx.Commit(); e != nil {
		return nil, "", e
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = out[len(out)-1].ID
	}
	return out, next, nil
}
func (s *PostgresRecipeStore) Update(ctx context.Context, o RecipeOwner, id string, expected int64, c RecipeContent) (RecipeEnvelope, error) {
	var z RecipeEnvelope
	if !validRecipeOwner(o) || !validRecipeUUID(id) {
		return z, ErrRecipeNotFound
	}
	if expected < 1 {
		return z, ErrRecipeConflict
	}
	if err := ValidateRecipeContent(c); err != nil {
		return z, err
	}
	body, hash, e := recipePayload(c)
	if e != nil {
		return z, e
	}
	tx, e := s.beginRecipeTx(ctx, false)
	if e != nil {
		return z, e
	}
	defer tx.Rollback()
	current, e := scanRecipe(tx.QueryRowContext(ctx, `SELECT `+recipeColumns+` FROM meal_planner.recipes WHERE owner_issuer=$1 AND owner_subject=$2 AND id=$3 AND status='approved' AND origin='manual' FOR UPDATE`, o.Issuer, o.Subject, id))
	if e != nil {
		return z, e
	}
	var oldHash, receipt []byte
	e = tx.QueryRowContext(ctx, `SELECT request_hash,receipt FROM meal_planner.recipe_updates WHERE owner_issuer=$1 AND owner_subject=$2 AND recipe_id=$3 AND expected_revision=$4`, o.Issuer, o.Subject, id, expected).Scan(&oldHash, &receipt)
	if e == nil {
		if !bytes.Equal(oldHash, hash) {
			return z, ErrRecipeConflict
		}
		if e = json.Unmarshal(receipt, &z); e != nil {
			return z, e
		}
		if z.Revision != current.Revision {
			return RecipeEnvelope{}, ErrRecipeConflict
		}
		return z, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return z, e
	}
	if current.Revision != expected || current.Status != "approved" || current.Origin != "manual" {
		return z, ErrRecipeConflict
	}
	saved, e := scanRecipe(tx.QueryRowContext(ctx, `UPDATE meal_planner.recipes SET content=$4,revision=revision+1,updated_at=clock_timestamp() WHERE owner_issuer=$1 AND owner_subject=$2 AND id=$3 AND revision=$5 RETURNING `+recipeColumns, o.Issuer, o.Subject, id, string(body), expected))
	if e != nil {
		return z, e
	}
	receipt, e = json.Marshal(saved)
	if e != nil {
		return z, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO meal_planner.recipe_updates (owner_issuer,owner_subject,recipe_id,expected_revision,request_hash,receipt) VALUES ($1,$2,$3,$4,$5,$6)`, o.Issuer, o.Subject, id, expected, hash, string(receipt))
	if e != nil {
		return z, e
	}
	if e = tx.Commit(); e != nil {
		return z, e
	}
	return saved, nil
}

var _ RecipeRepository = (*PostgresRecipeStore)(nil)

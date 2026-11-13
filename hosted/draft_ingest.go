package hosted

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

// This first slice accepts immutable, unreviewed snapshots only. It has no
// pairing, credential-issuance, read, edit, approval, or recipe/planner capability.
// Browser session JWTs and cookies are deliberately NOT accepted as send grants.
type DraftSubmission struct {
	ContentSchemaVersion int           `json:"content_schema_version"`
	Content              draft.Content `json:"content"`
}
type DraftReview struct {
	Revision      int64   `json:"revision"`
	State         string  `json:"state"`
	DraftRevision int64   `json:"draft_revision"`
	PolicyVersion *string `json:"policy_version"`
	Candidate     any     `json:"candidate"`
	Confirmations []any   `json:"confirmations"`
}
type HostedDraft struct {
	SchemaVersion        int    `json:"schema_version"`
	ContentSchemaVersion int    `json:"content_schema_version"`
	ID                   string `json:"id"`
	Revision             int64  `json:"revision"`
	Status               string `json:"status"`
	Origin               string `json:"origin"`
	Owner                struct {
		Issuer  string `json:"issuer"`
		Subject string `json:"subject"`
	} `json:"owner"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
	Content          draft.Content `json:"content"`
	Review           DraftReview   `json:"review"`
	ApprovedRecipeID *string       `json:"approved_recipe_id"`
}
type DraftReceipt struct {
	ID                   string        `json:"id"`
	DraftID              string        `json:"draft_id"`
	SubmittedAt          time.Time     `json:"submitted_at"`
	AcceptedRevision     int64         `json:"accepted_revision"`
	ContentSchemaVersion int           `json:"content_schema_version"`
	SubmissionContent    draft.Content `json:"submission_content"`
}
type DraftResponse struct {
	APIVersion string                 `json:"api_version"`
	Draft      HostedDraft            `json:"draft"`
	Receipt    DraftReceipt           `json:"receipt"`
	Validation draft.ValidationResult `json:"validation"`
}

func newDraftResponse(owner RecipeOwner, s DraftSubmission, now time.Time) (DraftResponse, error) {
	id, err := newRecipeUUID()
	if err != nil {
		return DraftResponse{}, err
	}
	receiptID, err := newRecipeUUID()
	if err != nil {
		return DraftResponse{}, err
	}
	d := HostedDraft{SchemaVersion: 1, ContentSchemaVersion: 1, ID: id, Revision: 1, Status: "draft", Origin: "agent", CreatedAt: now.UTC(), UpdatedAt: now.UTC(), Content: s.Content, Review: DraftReview{Revision: 1, State: "not_started", DraftRevision: 1, Confirmations: []any{}}}
	d.Owner.Issuer, d.Owner.Subject = owner.Issuer, owner.Subject
	return DraftResponse{APIVersion: "draft-v1", Draft: d, Receipt: DraftReceipt{ID: receiptID, DraftID: id, SubmittedAt: now.UTC(), AcceptedRevision: 1, ContentSchemaVersion: 1, SubmissionContent: s.Content}, Validation: draft.Validate(s.Content)}, nil
}

// DraftAccess is internal server policy, never decoded from request JSON. The
// raw grant is hashed immediately and is never retained, echoed or logged.
type DraftAccess struct {
	TokenHash     [32]byte
	Environment   string
	Issuer        string
	AllowedEmails []string
}
type DraftRepository interface {
	Authenticate(context.Context, DraftAccess) error
	Submit(context.Context, DraftAccess, string, DraftSubmission) (DraftResponse, bool, error)
}

var (
	ErrDraftUnauthorized = errors.New("draft grant unauthorized")
	ErrDraftQuota        = errors.New("draft quota exceeded")
	ErrDraftConflict     = errors.New("draft idempotency conflict")
	ErrDraftUnavailable  = errors.New("draft storage unavailable")
)

type DraftIngestConfig struct {
	Enabled                     bool
	Environment, Issuer, Origin string
	AllowedEmails               []string
}

// DraftIngestConfigFromEnvironment is evaluated per request so membership
// removal and disabling do not wait for a warm serverless process to restart.
func DraftIngestConfigFromEnvironment() DraftIngestConfig {
	if os.Getenv("VERCEL_ENV") != "preview" || os.Getenv("MEAL_DRAFT_INGEST_ENABLED") != "true" {
		return DraftIngestConfig{}
	}
	base, members, err := normalizeAuthMembership(os.Getenv("NEON_AUTH_BASE_URL"), os.Getenv("NEON_AUTH_ENDPOINT_ID"), os.Getenv("AUTH_ALLOWED_EMAIL"), authAllowedEmailsFromEnvironment())
	if err != nil {
		return DraftIngestConfig{}
	}
	origin := os.Getenv("MEAL_DRAFT_INGEST_ORIGIN")
	// Initial endpoint is pinned to the existing stable Preview, never Host/XFH.
	if origin != "https://athome-meals-dev.vercel.app" {
		return DraftIngestConfig{}
	}
	emails := make([]string, 0, len(members))
	for email := range members {
		emails = append(emails, email)
	}
	sort.Strings(emails)
	return DraftIngestConfig{Enabled: true, Environment: "preview", Issuer: base, Origin: origin, AllowedEmails: emails}
}

var draftGrantPattern = regexp.MustCompile(`^Bearer mp_lc1_pv_[A-Za-z0-9_-]{43}$`)
var draftKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{15,127}$`)
var draftVersionPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

func DraftIngestHandler(repository DraftRepository, config DraftIngestConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Deliberate disabled default and Production lock. No CORS/preflight route.
		if !config.Enabled || config.Environment != "preview" || config.Issuer == "" || config.Origin != "https://athome-meals-dev.vercel.app" || len(config.AllowedEmails) == 0 {
			recipeError(w, 404, "not_found", "Not found.")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			recipeError(w, 405, "method_not_allowed", "Method not allowed.")
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			recipeError(w, 400, "invalid_request", "Query parameters are not supported.")
			return
		}
		// Native clients have no Origin. A browser caller must be the pinned Preview.
		if origins, ok := r.Header["Origin"]; ok && (len(origins) != 1 || origins[0] != config.Origin) {
			recipeError(w, 403, "forbidden", "Origin not allowed.")
			return
		}
		auth := r.Header.Values("Authorization")
		if len(auth) != 1 || !draftGrantPattern.MatchString(auth[0]) {
			recipeError(w, 401, "unauthorized", "A valid draft submission grant is required.")
			return
		}
		rawToken := strings.TrimPrefix(auth[0], "Bearer ")
		decodedToken, decodeErr := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(rawToken, "mp_lc1_pv_"))
		if decodeErr != nil || len(decodedToken) != 32 {
			recipeError(w, 401, "unauthorized", "A valid draft submission grant is required.")
			return
		}
		access := DraftAccess{TokenHash: sha256.Sum256([]byte(rawToken)), Environment: config.Environment, Issuer: config.Issuer, AllowedEmails: append([]string(nil), config.AllowedEmails...)}
		if repository == nil {
			recipeError(w, 503, "unavailable", "Draft storage is unavailable. Saving has not been confirmed.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		if err := repository.Authenticate(ctx, access); err != nil {
			draftRepositoryError(w, err)
			return
		}
		mediaHeaders := r.Header.Values("Content-Type")
		if len(mediaHeaders) != 1 {
			recipeError(w, 415, "unsupported_media_type", "Use application/json.")
			return
		}
		media, _, err := mime.ParseMediaType(mediaHeaders[0])
		if err != nil || media != "application/json" {
			recipeError(w, 415, "unsupported_media_type", "Use application/json.")
			return
		}
		if len(r.Header.Values("Content-Encoding")) != 0 {
			recipeError(w, 415, "unsupported_media_type", "Content encoding is not supported.")
			return
		}
		keys := r.Header.Values("Idempotency-Key")
		if len(keys) == 0 || (len(keys) == 1 && keys[0] == "") {
			recipeError(w, 400, "missing_idempotency_key", "An idempotency key is required.")
			return
		}
		if len(keys) != 1 || !draftKeyPattern.MatchString(keys[0]) {
			recipeError(w, 400, "invalid_idempotency_key", "Invalid idempotency key.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, draft.MaxInputBytes)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			var large *http.MaxBytesError
			if errors.As(err, &large) {
				recipeError(w, 413, "input_too_large", "Draft input exceeds the size limit.")
			} else {
				recipeError(w, 400, "invalid_request", "Could not read draft input.")
			}
			return
		}
		submission, status, code := decodeDraftSubmission(raw)
		if status != 0 {
			recipeError(w, status, code, "Draft input was not accepted.")
			return
		}
		result, replay, err := repository.Submit(ctx, access, keys[0], submission)
		if err != nil {
			draftRepositoryError(w, err)
			return
		}
		status = 201
		if replay {
			status = 200
		}
		recipeJSON(w, status, result)
	})
}
func draftRepositoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrDraftUnauthorized):
		recipeError(w, 401, "unauthorized", "A valid draft submission grant is required.")
	case errors.Is(err, ErrDraftConflict):
		recipeError(w, 409, "idempotency_conflict", "This operation key was already used with different content.")
	case errors.Is(err, ErrDraftQuota):
		w.Header().Set("Retry-After", "3600")
		recipeError(w, 429, "quota_exceeded", "Draft capacity or submission limit reached. No work was deleted.")
	default:
		recipeError(w, 503, "unavailable", "Saving has not been confirmed. Retry the same content with the same operation key.")
	}
}
func decodeDraftSubmission(raw []byte) (DraftSubmission, int, string) {
	fail := func(status int, code string) (DraftSubmission, int, string) { return DraftSubmission{}, status, code }
	if len(raw) > draft.MaxInputBytes {
		return fail(413, "input_too_large")
	}
	if !draft.ValidUniqueJSONEnvelope(raw, "content_schema_version", "content") {
		return fail(400, "invalid_request")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 2 {
		return fail(400, "invalid_request")
	}
	version, ok := fields["content_schema_version"]
	content, hasContent := fields["content"]
	if !ok || !hasContent {
		return fail(400, "invalid_request")
	}
	version = bytes.TrimSpace(version)
	if !draftVersionPattern.Match(version) {
		return fail(400, "invalid_type")
	}
	if !bytes.Equal(version, []byte("1")) {
		return fail(422, "unsupported_content_schema")
	}
	decoded, issues := draft.DecodeContent(content)
	if len(issues) > 0 {
		return fail(400, "invalid_content")
	}
	if !draft.Validate(decoded).StructurallyValid {
		return fail(400, "invalid_content")
	}
	return DraftSubmission{ContentSchemaVersion: 1, Content: decoded}, 0, ""
}

// Synthetic tests and operators use only a hash, never persist the raw token.
func draftTokenHashHex(a DraftAccess) string { return hex.EncodeToString(a.TokenHash[:]) }

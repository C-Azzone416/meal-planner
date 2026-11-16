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
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

// This source gate is deliberately closed. A reviewed change must open it in
// addition to configuring Preview. Environment configuration alone cannot make
// the workflow reachable.
const draftWorkflowSourceEnabled = false

type DraftWorkflowConfig struct {
	Enabled                     bool
	Environment, Issuer, Origin string
	AllowedEmails               []string
}

func DraftWorkflowConfigFromEnvironment() DraftWorkflowConfig {
	if !draftWorkflowSourceEnabled || os.Getenv("VERCEL_ENV") != "preview" || os.Getenv("MEAL_DRAFT_WORKFLOW_ENABLED") != "true" {
		return DraftWorkflowConfig{}
	}
	base, members, err := normalizeAuthMembership(os.Getenv("NEON_AUTH_BASE_URL"), os.Getenv("NEON_AUTH_ENDPOINT_ID"), os.Getenv("AUTH_ALLOWED_EMAIL"), authAllowedEmailsFromEnvironment())
	if err != nil || os.Getenv("MEAL_DRAFT_INGEST_ORIGIN") != "https://athome-meals-dev.vercel.app" {
		return DraftWorkflowConfig{}
	}
	emails := make([]string, 0, len(members))
	for email := range members {
		emails = append(emails, email)
	}
	return DraftWorkflowConfig{Enabled: true, Environment: "preview", Issuer: base, Origin: "https://athome-meals-dev.vercel.app", AllowedEmails: emails}
}

type DraftIntent struct {
	ID               string          `json:"id"`
	Revision         int64           `json:"revision"`
	State            string          `json:"state"`
	ContentHash      string          `json:"content_hash"`
	CreatedAt        time.Time       `json:"created_at"`
	ExpiresAt        time.Time       `json:"expires_at"`
	Submission       DraftSubmission `json:"submission,omitempty"`
	DraftID          *string         `json:"draft_id"`
	ReceiptID        *string         `json:"receipt_id"`
	AcceptedRevision *int64          `json:"accepted_revision,omitempty"`
	SubmittedAt      *time.Time      `json:"submitted_at,omitempty"`
}

type HostedDraftSummary struct {
	ID, Title, Status, ReviewState string
	Revision, ReviewRevision       int64
	CreatedAt, UpdatedAt           time.Time
}

func (s HostedDraftSummary) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID             string    `json:"id"`
		Title          string    `json:"title"`
		Status         string    `json:"status"`
		Revision       int64     `json:"revision"`
		CreatedAt      time.Time `json:"created_at"`
		UpdatedAt      time.Time `json:"updated_at"`
		ReviewState    string    `json:"review_state"`
		ReviewRevision int64     `json:"review_revision"`
	}{s.ID, s.Title, s.Status, s.Revision, s.CreatedAt, s.UpdatedAt, s.ReviewState, s.ReviewRevision})
}

var (
	ErrDraftIntentNotFound = errors.New("draft intent not found")
	ErrDraftRevision       = errors.New("draft workflow revision mismatch")
	ErrDraftIntentExpired  = errors.New("draft intent expired")
)

type DraftWorkflowRepository interface {
	CreateIntent(context.Context, DraftAccess, string, DraftSubmission) (DraftIntent, bool, error)
	PollIntent(context.Context, DraftAccess, string) (DraftIntent, error)
	ListIntents(context.Context, RecipeOwner) ([]DraftIntent, error)
	GetIntent(context.Context, RecipeOwner, string) (DraftIntent, error)
	DecideIntent(context.Context, RecipeOwner, string, int64, string, bool, DraftAccess) (DraftIntent, error)
	ListHostedDrafts(context.Context, RecipeOwner) ([]HostedDraftSummary, error)
	GetHostedDraft(context.Context, RecipeOwner, string) (DraftResponse, error)
	ReviewHostedDraft(context.Context, RecipeOwner, string, int64, int64) (DraftResponse, error)
	MarkHostedDraftApproved(context.Context, RecipeOwner, string, int64, int64, string) (DraftResponse, error)
}

func workflowEnabled(c DraftWorkflowConfig) bool {
	return c.Enabled && c.Environment == "preview" && c.Issuer != "" && c.Origin == "https://athome-meals-dev.vercel.app" && len(c.AllowedEmails) > 0
}

func workflowAccess(r *http.Request, c DraftWorkflowConfig) (DraftAccess, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !draftGrantPattern.MatchString(values[0]) {
		return DraftAccess{}, false
	}
	raw := strings.TrimPrefix(values[0], "Bearer ")
	b, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(raw, "mp_lc1_pv_"))
	if err != nil || len(b) != 32 {
		return DraftAccess{}, false
	}
	return DraftAccess{TokenHash: sha256.Sum256([]byte(raw)), Environment: c.Environment, Issuer: c.Issuer, AllowedEmails: append([]string(nil), c.AllowedEmails...), RequiredScope: "draft:propose"}, true
}

func validWorkflowRequest(w http.ResponseWriter, r *http.Request, c DraftWorkflowConfig) bool {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !workflowEnabled(c) {
		recipeError(w, 404, "not_found", "Not found.")
		return false
	}
	if origins := r.Header.Values("Origin"); len(origins) != 0 && (len(origins) != 1 || origins[0] != c.Origin) {
		recipeError(w, 403, "forbidden", "Origin not allowed.")
		return false
	}
	return true
}

func ownerMutationAllowed(w http.ResponseWriter, r *http.Request, c DraftWorkflowConfig) bool {
	if r.Method == http.MethodGet {
		return true
	}
	if origins := r.Header.Values("Origin"); len(origins) != 1 || origins[0] != c.Origin || r.Header.Get("Sec-Fetch-Site") != "same-origin" {
		recipeError(w, http.StatusForbidden, "forbidden", "Cross-site requests are not allowed.")
		return false
	}
	return true
}

func decodeWorkflowBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, bool) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		recipeError(w, 400, "invalid_request", "Query parameters are not supported.")
		return nil, false
	}
	mediaValues := r.Header.Values("Content-Type")
	if len(mediaValues) != 1 {
		recipeError(w, 415, "unsupported_media_type", "Use application/json.")
		return nil, false
	}
	media, _, err := mime.ParseMediaType(mediaValues[0])
	if err != nil || media != "application/json" || len(r.Header.Values("Content-Encoding")) != 0 {
		recipeError(w, 415, "unsupported_media_type", "Use uncompressed application/json.")
		return nil, false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		recipeError(w, 400, "invalid_request", "Invalid request.")
		return nil, false
	}
	return raw, true
}

// DraftIntentCreateHandler is grant-authenticated. It stores a proposal only;
// a signed-in owner must approve the exact hash before any hosted draft exists.
func DraftIntentCreateHandler(repo DraftWorkflowRepository, c DraftWorkflowConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validWorkflowRequest(w, r, c) {
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			recipeError(w, 405, "method_not_allowed", "Method not allowed.")
			return
		}
		a, ok := workflowAccess(r, c)
		if !ok {
			recipeError(w, 401, "unauthorized", "A valid draft proposal grant is required.")
			return
		}
		keys := r.Header.Values("Idempotency-Key")
		if len(keys) != 1 || !draftKeyPattern.MatchString(keys[0]) {
			recipeError(w, 400, "invalid_idempotency_key", "Invalid idempotency key.")
			return
		}
		raw, ok := decodeWorkflowBody(w, r, draft.MaxInputBytes)
		if !ok {
			return
		}
		sub, status, code := decodeDraftSubmission(raw)
		if status != 0 {
			recipeError(w, status, code, "Draft input was not accepted.")
			return
		}
		if repo == nil {
			recipeError(w, 503, "unavailable", "Draft workflow is unavailable.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		intent, replay, err := repo.CreateIntent(ctx, a, keys[0], sub)
		if err != nil {
			workflowError(w, err)
			return
		}
		if replay {
			status = 200
		} else {
			status = 201
		}
		recipeJSON(w, status, map[string]any{"api_version": "draft-intent-v1", "intent": safeIntent(intent), "approval": map[string]string{"path": "/drafts/incoming/" + intent.ID}})
	})
}

type intentPollRequest struct {
	IntentID string `json:"intent_id"`
}

func DraftIntentPollHandler(repo DraftWorkflowRepository, c DraftWorkflowConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validWorkflowRequest(w, r, c) {
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			recipeError(w, 405, "method_not_allowed", "Method not allowed.")
			return
		}
		a, ok := workflowAccess(r, c)
		if !ok {
			recipeError(w, 401, "unauthorized", "A valid draft proposal grant is required.")
			return
		}
		raw, ok := decodeWorkflowBody(w, r, 4096)
		if !ok {
			return
		}
		var in intentPollRequest
		if !draft.ValidUniqueJSONEnvelope(raw, "intent_id") || json.Unmarshal(raw, &in) != nil || !validRecipeUUID(in.IntentID) {
			recipeError(w, 400, "invalid_request", "Invalid poll request.")
			return
		}
		if repo == nil {
			recipeError(w, 503, "unavailable", "Draft workflow is unavailable.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		intent, err := repo.PollIntent(ctx, a, in.IntentID)
		if err != nil {
			workflowError(w, err)
			return
		}
		status := http.StatusOK
		if intent.State == "pending" {
			status = http.StatusAccepted
		}
		recipeJSON(w, status, map[string]any{"api_version": "draft-intent-v1", "intent": safeIntent(intent)})
	})
}

func safeIntent(i DraftIntent) map[string]any {
	state := i.State
	if state == "accepted" {
		state = "approved"
	}
	out := map[string]any{"id": i.ID, "state": state, "revision": i.Revision, "content_hash": i.ContentHash, "expires_at": i.ExpiresAt}
	if state == "approved" {
		out["draft_id"] = i.DraftID
		out["receipt_id"] = i.ReceiptID
		out["accepted_revision"] = i.AcceptedRevision
		out["submitted_at"] = i.SubmittedAt
	}
	return out
}

type intentDecisionRequest struct {
	IntentID         string `json:"intent_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	ContentHash      string `json:"content_hash"`
	Decision         string `json:"decision"`
}

// OwnerIntentHandler must be wrapped in RequireUser. It deliberately checks the
// verified owner before lookup, so foreign and absent identifiers are identical.
func OwnerIntentHandler(repo DraftWorkflowRepository, c DraftWorkflowConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validWorkflowRequest(w, r, c) {
			return
		}
		identity, ok := UserFromContext(r.Context())
		if !ok {
			recipeError(w, 401, "unauthorized", "Sign in to review drafts.")
			return
		}
		owner := RecipeOwner{identity.Issuer, identity.Subject}
		if !ownerMutationAllowed(w, r, c) {
			return
		}
		if repo == nil {
			recipeError(w, 503, "unavailable", "Draft workflow is unavailable.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		switch r.Method {
		case http.MethodGet:
			q, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil {
				recipeError(w, 400, "invalid_request", "Invalid query.")
				return
			}
			if len(q) == 0 {
				items, err := repo.ListIntents(ctx, owner)
				if err != nil {
					workflowError(w, err)
					return
				}
				recipeJSON(w, 200, map[string]any{"intents": items})
				return
			}
			if len(q) != 1 || len(q["id"]) != 1 || !validRecipeUUID(q.Get("id")) {
				recipeError(w, 400, "invalid_request", "Invalid query.")
				return
			}
			item, err := repo.GetIntent(ctx, owner, q.Get("id"))
			if err != nil {
				workflowError(w, err)
				return
			}
			recipeJSON(w, 200, item)
		case http.MethodPost:
			raw, ok := decodeWorkflowBody(w, r, 4096)
			if !ok {
				return
			}
			var in intentDecisionRequest
			if !draft.ValidUniqueJSONEnvelope(raw, "intent_id", "expected_revision", "content_hash", "decision") || json.Unmarshal(raw, &in) != nil || !validRecipeUUID(in.IntentID) || in.ExpectedRevision < 1 || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(in.ContentHash) || (in.Decision != "approve" && in.Decision != "deny") {
				recipeError(w, 400, "invalid_request", "Invalid decision.")
				return
			}
			access := DraftAccess{Environment: c.Environment, Issuer: c.Issuer, AllowedEmails: append([]string(nil), c.AllowedEmails...), RequiredScope: "draft:propose"}
			item, err := repo.DecideIntent(ctx, owner, in.IntentID, in.ExpectedRevision, in.ContentHash, in.Decision == "approve", access)
			if err != nil {
				workflowError(w, err)
				return
			}
			recipeJSON(w, 200, item)
		default:
			w.Header().Set("Allow", "GET, POST")
			recipeError(w, 405, "method_not_allowed", "Method not allowed.")
		}
	})
}

type reviewRequest struct {
	DraftID                string `json:"draft_id"`
	ExpectedDraftRevision  int64  `json:"expected_draft_revision"`
	ExpectedReviewRevision int64  `json:"expected_review_revision"`
}
type saveRequest struct {
	DraftID                string          `json:"draft_id"`
	ExpectedDraftRevision  int64           `json:"expected_draft_revision"`
	ExpectedReviewRevision int64           `json:"expected_review_revision"`
	Projection             json.RawMessage `json:"projection"`
}

// OwnerDraftHandler exposes lossless canonical drafts and a narrow review
// transition. Saving requires a separately supplied complete manual projection;
// no unknown value is coerced into zero or false.
func OwnerDraftHandler(repo DraftWorkflowRepository, recipes RecipeRepository, c DraftWorkflowConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validWorkflowRequest(w, r, c) {
			return
		}
		identity, ok := UserFromContext(r.Context())
		if !ok {
			recipeError(w, 401, "unauthorized", "Sign in to review drafts.")
			return
		}
		owner := RecipeOwner{identity.Issuer, identity.Subject}
		if !ownerMutationAllowed(w, r, c) {
			return
		}
		if repo == nil {
			recipeError(w, 503, "unavailable", "Draft workflow is unavailable.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		if r.Method == http.MethodGet {
			q, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil {
				recipeError(w, 400, "invalid_request", "Invalid query.")
				return
			}
			if len(q) == 0 {
				items, err := repo.ListHostedDrafts(ctx, owner)
				if err != nil {
					workflowError(w, err)
					return
				}
				recipeJSON(w, 200, map[string]any{"drafts": items})
				return
			}
			if len(q) != 1 || len(q["id"]) != 1 || !validRecipeUUID(q.Get("id")) {
				recipeError(w, 400, "invalid_request", "Invalid query.")
				return
			}
			item, err := repo.GetHostedDraft(ctx, owner, q.Get("id"))
			if err != nil {
				workflowError(w, err)
				return
			}
			recipeJSON(w, 200, item)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			recipeError(w, 405, "method_not_allowed", "Method not allowed.")
			return
		}
		raw, ok := decodeWorkflowBody(w, r, 3_500_000)
		if !ok {
			return
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			recipeError(w, 400, "invalid_request", "Invalid review request.")
			return
		}
		if _, has := fields["projection"]; !has {
			var in reviewRequest
			if !draft.ValidUniqueJSONEnvelope(raw, "draft_id", "expected_draft_revision", "expected_review_revision") || json.Unmarshal(raw, &in) != nil || !validRecipeUUID(in.DraftID) || in.ExpectedDraftRevision < 1 || in.ExpectedReviewRevision < 1 {
				recipeError(w, 400, "invalid_request", "Invalid review request.")
				return
			}
			item, err := repo.ReviewHostedDraft(ctx, owner, in.DraftID, in.ExpectedDraftRevision, in.ExpectedReviewRevision)
			if err != nil {
				workflowError(w, err)
				return
			}
			recipeJSON(w, 200, item)
			return
		}
		var in saveRequest
		if !draft.ValidUniqueJSONEnvelope(raw, "draft_id", "expected_draft_revision", "expected_review_revision", "projection") || json.Unmarshal(raw, &in) != nil || !validRecipeUUID(in.DraftID) || in.ExpectedDraftRevision < 1 || in.ExpectedReviewRevision < 1 {
			recipeError(w, 400, "invalid_request", "Invalid save request.")
			return
		}
		candidate, err := decodeRecipeContent(in.Projection)
		if err != nil {
			recipeError(w, 400, "invalid_request", "The recipe projection is incomplete or invalid.")
			return
		}
		if recipes == nil {
			recipeError(w, 503, "unavailable", "Recipe storage is unavailable. Nothing was confirmed saved.")
			return
		}
		// The hosted draft UUID is a stable owner-scoped recipe create key. If recipe
		// creation commits but marking the draft does not, retry recovers the same
		// receipt rather than creating a second recipe.
		current, err := repo.GetHostedDraft(ctx, owner, in.DraftID)
		if err != nil {
			workflowError(w, err)
			return
		}
		if current.Draft.Review.State == "approved" && current.Draft.ApprovedRecipeID != nil && current.Draft.Revision == in.ExpectedDraftRevision && current.Draft.Review.Revision == in.ExpectedReviewRevision+1 {
			recipe, err := recipes.Get(ctx, owner, *current.Draft.ApprovedRecipeID)
			if err != nil {
				recipeRepositoryError(w, err)
				return
			}
			if !reflect.DeepEqual(recipe.Content, candidate) {
				workflowError(w, ErrDraftRevision)
				return
			}
			recipeJSON(w, 200, map[string]any{"draft": current.Draft, "recipe": recipe})
			return
		}
		if current.Draft.Revision != in.ExpectedDraftRevision || current.Draft.Review.Revision != in.ExpectedReviewRevision || current.Draft.Review.State != "ready" {
			workflowError(w, ErrDraftRevision)
			return
		}
		recipe, replayed, err := recipes.Create(ctx, owner, in.DraftID, candidate)
		_ = replayed
		if err != nil {
			if errors.Is(err, ErrRecipeIdempotencyConflict) {
				workflowError(w, ErrDraftRevision)
			} else {
				recipeRepositoryError(w, err)
			}
			return
		}
		item, err := repo.MarkHostedDraftApproved(ctx, owner, in.DraftID, in.ExpectedDraftRevision, in.ExpectedReviewRevision, recipe.ID)
		if err != nil {
			workflowError(w, err)
			return
		}
		recipeJSON(w, 200, map[string]any{"draft": item.Draft, "recipe": recipe})
	})
}

func workflowError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrDraftUnauthorized):
		recipeError(w, 401, "unauthorized", "A valid draft proposal grant is required.")
	case errors.Is(err, ErrDraftIntentNotFound), errors.Is(err, ErrRecipeNotFound):
		recipeError(w, 404, "not_found", "Draft not found.")
	case errors.Is(err, ErrDraftConflict):
		recipeError(w, 409, "idempotency_conflict", "This operation key was already used with different content.")
	case errors.Is(err, ErrDraftQuota):
		w.Header().Set("Retry-After", "3600")
		recipeError(w, 429, "quota_exceeded", "Draft proposal capacity is full. No work was deleted.")
	case errors.Is(err, ErrDraftRevision):
		recipeError(w, 409, "revision_mismatch", "The draft changed. Reload it before continuing.")
	case errors.Is(err, ErrDraftIntentExpired):
		recipeError(w, 410, "intent_expired", "This proposal expired without being saved.")
	default:
		recipeError(w, 503, "unavailable", "Draft workflow is unavailable. No change was confirmed.")
	}
}

func contentDigest(s DraftSubmission) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func sameHash(a string, b []byte) bool {
	decoded, err := hex.DecodeString(a)
	return err == nil && bytes.Equal(decoded, b)
}
func parseLimit(raw string, fallback int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 100 {
		return 0, errors.New("invalid limit")
	}
	return n, nil
}

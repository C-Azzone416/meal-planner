package hosted

import (
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
	"strings"
	"time"
	"unicode/utf8"
)

// Pairing is deliberately a device-style, outbound-only protocol. The local
// client polls over HTTPS; there is no callback URI or local listener.
const (
	draftPairingAPIVersion = "local-client-v1"
	draftPreviewOrigin     = "https://athome-meals-dev.vercel.app"
	draftPreviewHost       = "athome-meals-dev.vercel.app"
	draftPairingBodyLimit  = 8 * 1024
	draftPairingStoreLimit = 10 * time.Second
	// Source authorization is intentionally separate from runtime configuration.
	// Tomorrow's reviewed hookup must change this constant as its own approval.
	draftPairingSourceEnabled = false
)

var (
	pairingChallengePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	pairingVerifierPattern  = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
	pairingSecretPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	pairingCodePattern      = regexp.MustCompile(`^[23456789ABCDEFGHJKLMNPQRSTUVWXYZ]{8}$`)
	pairingFingerprint      = regexp.MustCompile(`^[0-9a-f]{8}$`)
	membershipEpochPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

	ErrDraftPairingInvalid  = errors.New("draft pairing request is invalid")
	ErrDraftPairingMissing  = errors.New("draft pairing was not found")
	ErrDraftPairingPending  = errors.New("draft pairing is pending")
	ErrDraftPairingSlowDown = errors.New("draft pairing polling is too frequent")
	ErrDraftPairingGone     = errors.New("draft pairing is no longer redeemable")
	ErrDraftPairingLimit    = errors.New("draft pairing capacity reached")
	ErrDraftGrantLimit      = errors.New("draft grant limit reached")
)

type DraftPairingConfig struct {
	Enabled         bool
	Environment     string
	Issuer          string
	APIOrigin       string
	WebOrigin       string
	MembershipEpoch string
}

// DraftPairingConfigFromEnvironment has two independent gates: the source only
// permits the stable Preview origin, and runtime must opt in with an explicit
// epoch. Operators must rotate the epoch whenever membership is removed or
// re-added; otherwise old grants intentionally fail closed after that change.
func DraftPairingConfigFromEnvironment() DraftPairingConfig {
	if !draftPairingSourceEnabled || os.Getenv("VERCEL_ENV") != "preview" || os.Getenv("MEAL_DRAFT_PAIRING_ENABLED") != "true" {
		return DraftPairingConfig{}
	}
	base, _, err := normalizeAuthMembership(os.Getenv("NEON_AUTH_BASE_URL"), os.Getenv("NEON_AUTH_ENDPOINT_ID"), os.Getenv("AUTH_ALLOWED_EMAIL"), authAllowedEmailsFromEnvironment())
	if err != nil {
		return DraftPairingConfig{}
	}
	origin := os.Getenv("MEAL_DRAFT_PAIRING_ORIGIN")
	epoch := os.Getenv("MEAL_DRAFT_MEMBERSHIP_EPOCH")
	if origin != draftPreviewOrigin || !membershipEpochPattern.MatchString(epoch) {
		return DraftPairingConfig{}
	}
	return DraftPairingConfig{Enabled: true, Environment: "preview", Issuer: base, APIOrigin: origin, WebOrigin: origin, MembershipEpoch: epoch}
}

func validPairingConfig(c DraftPairingConfig) bool {
	return c.Enabled && c.Environment == "preview" && c.Issuer != "" && c.APIOrigin == draftPreviewOrigin && c.WebOrigin == draftPreviewOrigin && membershipEpochPattern.MatchString(c.MembershipEpoch)
}

type DraftPairingCreateRequest struct {
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	ClientLabel         string `json:"client_label"`
}

type DraftPairingCreated struct {
	APIVersion           string `json:"api_version"`
	PairingID            string `json:"pairing_id"`
	PollToken            string `json:"poll_token"`
	UserCode             string `json:"user_code"`
	VerificationURI      string `json:"verification_uri"`
	ExpiresIn            int    `json:"expires_in"`
	Interval             int    `json:"interval"`
	ChallengeFingerprint string `json:"challenge_fingerprint"`
}

type DraftPairingPollRequest struct {
	PairingID    string `json:"pairing_id"`
	PollToken    string `json:"poll_token"`
	CodeVerifier string `json:"code_verifier"`
}

type DraftPairingPollResult struct {
	APIVersion  string `json:"api_version"`
	State       string `json:"state"`
	Interval    int    `json:"interval,omitempty"`
	Environment string `json:"environment,omitempty"`
	APIOrigin   string `json:"api_origin,omitempty"`
	WebOrigin   string `json:"web_origin,omitempty"`
	BindingID   string `json:"binding_id,omitempty"`
	Grant       string `json:"grant,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	OwnerLabel  string `json:"owner_label,omitempty"`
	Scope       string `json:"scope,omitempty"`
}

type DraftPairingApproveRequest struct {
	UserCode             string `json:"user_code"`
	ChallengeFingerprint string `json:"challenge_fingerprint"`
}

type DraftGrantSummary struct {
	ID          string  `json:"id"`
	ClientLabel string  `json:"client_label"`
	Scope       string  `json:"scope"`
	CreatedAt   string  `json:"created_at"`
	ExpiresAt   string  `json:"expires_at"`
	RevokedAt   *string `json:"revoked_at"`
	Active      bool    `json:"active"`
}

type DraftPairingRepository interface {
	CreatePairing(context.Context, DraftPairingConfig, DraftPairingCreateRequest) (DraftPairingCreated, error)
	PollPairing(context.Context, DraftPairingConfig, DraftPairingPollRequest) (DraftPairingPollResult, error)
	ApprovePairing(context.Context, DraftPairingConfig, UserIdentity, DraftPairingApproveRequest) error
	ListDraftGrants(context.Context, DraftPairingConfig, RecipeOwner) ([]DraftGrantSummary, error)
	RevokeDraftGrant(context.Context, DraftPairingConfig, RecipeOwner, string) error
}

func pairingHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
}

func pairingDisabled(w http.ResponseWriter, c DraftPairingConfig) bool {
	pairingHeaders(w)
	if !validPairingConfig(c) {
		recipeError(w, http.StatusNotFound, "not_found", "Not found.")
		return true
	}
	return false
}

func pairingOriginAllowed(r *http.Request, c DraftPairingConfig, browser bool) bool {
	origins, present := r.Header["Origin"]
	if browser {
		return present && len(origins) == 1 && origins[0] == c.WebOrigin
	}
	return !present || (len(origins) == 1 && origins[0] == c.WebOrigin)
}

func pairingPublicTargetAllowed(r *http.Request, path string) bool {
	return r.Host == draftPreviewHost && r.URL.Path == path && r.URL.RawPath == "" && r.URL.RawQuery == "" && !r.URL.ForceQuery
}

func decodePairingBody(w http.ResponseWriter, r *http.Request, allowed map[string]bool) (map[string]json.RawMessage, bool) {
	values := r.Header.Values("Content-Type")
	if len(values) != 1 {
		recipeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json.")
		return nil, false
	}
	media, _, err := mime.ParseMediaType(values[0])
	if err != nil || media != "application/json" || len(r.Header.Values("Content-Encoding")) != 0 {
		recipeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json without content encoding.")
		return nil, false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, draftPairingBodyLimit))
	if err != nil {
		recipeError(w, http.StatusBadRequest, "invalid_request", "Invalid request.")
		return nil, false
	}
	fields, err := recipeObjectFields(raw, allowed)
	if err != nil || len(fields) != len(allowed) {
		recipeError(w, http.StatusBadRequest, "invalid_request", "Invalid request.")
		return nil, false
	}
	return fields, true
}

func decodeRequiredString(fields map[string]json.RawMessage, name string) (string, bool) {
	var value string
	if json.Unmarshal(fields[name], &value) != nil {
		return "", false
	}
	return value, true
}

func validClientLabel(s string) bool {
	if !utf8.ValidString(s) || len(s) == 0 || len(s) > 80 || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func pairingStoreContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), draftPairingStoreLimit)
}

func DraftPairingCreateHandler(repo DraftPairingRepository, c DraftPairingConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pairingDisabled(w, c) {
			return
		}
		if !pairingPublicTargetAllowed(r, "/api/draft-pairings") {
			recipeError(w, http.StatusNotFound, "not_found", "Not found.")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			recipeError(w, 405, "method_not_allowed", "Method not allowed.")
			return
		}
		if !pairingOriginAllowed(r, c, false) {
			recipeError(w, 403, "forbidden", "Request origin not allowed.")
			return
		}
		fields, ok := decodePairingBody(w, r, map[string]bool{"code_challenge": true, "code_challenge_method": true, "client_label": true})
		if !ok {
			return
		}
		challenge, a := decodeRequiredString(fields, "code_challenge")
		method, b := decodeRequiredString(fields, "code_challenge_method")
		label, d := decodeRequiredString(fields, "client_label")
		if !a || !b || !d || method != "S256" || !pairingChallengePattern.MatchString(challenge) || !validClientLabel(label) {
			recipeError(w, 400, "invalid_request", "Invalid pairing request.")
			return
		}
		decoded, err := base64.RawURLEncoding.Strict().DecodeString(challenge)
		if err != nil || len(decoded) != sha256.Size {
			recipeError(w, 400, "invalid_request", "Invalid pairing request.")
			return
		}
		if repo == nil {
			recipeError(w, 503, "unavailable", "Pairing is unavailable.")
			return
		}
		ctx, cancel := pairingStoreContext(r)
		defer cancel()
		result, err := repo.CreatePairing(ctx, c, DraftPairingCreateRequest{CodeChallenge: challenge, CodeChallengeMethod: method, ClientLabel: label})
		if errors.Is(err, ErrDraftPairingLimit) {
			recipeError(w, 429, "pairing_limit", "Pairing capacity is temporarily full. Try again later.")
			return
		}
		if err != nil {
			recipeError(w, 503, "unavailable", "Pairing is unavailable.")
			return
		}
		result.APIVersion = draftPairingAPIVersion
		recipeJSON(w, http.StatusCreated, result)
	})
}

func DraftPairingPollHandler(repo DraftPairingRepository, c DraftPairingConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pairingDisabled(w, c) {
			return
		}
		if !pairingPublicTargetAllowed(r, "/api/draft-pairings/poll") {
			recipeError(w, http.StatusNotFound, "not_found", "Not found.")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			recipeError(w, 405, "method_not_allowed", "Method not allowed.")
			return
		}
		if !pairingOriginAllowed(r, c, false) {
			recipeError(w, 403, "forbidden", "Request origin not allowed.")
			return
		}
		fields, ok := decodePairingBody(w, r, map[string]bool{"pairing_id": true, "poll_token": true, "code_verifier": true})
		if !ok {
			return
		}
		id, a := decodeRequiredString(fields, "pairing_id")
		token, b := decodeRequiredString(fields, "poll_token")
		verifier, d := decodeRequiredString(fields, "code_verifier")
		if !a || !b || !d || !recipeUUID.MatchString(id) || !pairingSecretPattern.MatchString(token) || !pairingVerifierPattern.MatchString(verifier) {
			recipeError(w, 400, "invalid_request", "Invalid pairing poll.")
			return
		}
		if repo == nil {
			recipeError(w, 503, "unavailable", "Pairing is unavailable.")
			return
		}
		ctx, cancel := pairingStoreContext(r)
		defer cancel()
		result, err := repo.PollPairing(ctx, c, DraftPairingPollRequest{PairingID: id, PollToken: token, CodeVerifier: verifier})
		result.APIVersion = draftPairingAPIVersion
		switch {
		case err == nil:
			recipeJSON(w, 200, result)
		case errors.Is(err, ErrDraftPairingPending):
			result.State, result.Interval = "pending", 5
			recipeJSON(w, 202, result)
		case errors.Is(err, ErrDraftPairingSlowDown):
			result.State, result.Interval = "slow_down", 10
			recipeJSON(w, 202, result)
		case errors.Is(err, ErrDraftPairingGone):
			recipeJSON(w, 410, DraftPairingPollResult{APIVersion: draftPairingAPIVersion, State: "expired"})
		case errors.Is(err, ErrDraftGrantLimit):
			recipeError(w, 429, "grant_limit", "Revoke an existing connection before pairing another client.")
		case errors.Is(err, ErrDraftPairingInvalid), errors.Is(err, ErrDraftPairingMissing):
			recipeError(w, 401, "unauthorized", "Pairing credentials were not accepted.")
		default:
			recipeError(w, 503, "unavailable", "Pairing outcome is unknown. Do not start another pairing yet.")
		}
	})
}

func DraftPairingApproveHandler(repo DraftPairingRepository, c DraftPairingConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pairingDisabled(w, c) {
			return
		}
		identity, ok := UserFromContext(r.Context())
		if !ok || identity.Issuer != c.Issuer || identity.Subject == "" {
			recipeError(w, 401, "unauthorized", "Sign in to approve pairing.")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			recipeError(w, 405, "method_not_allowed", "Method not allowed.")
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery || !pairingOriginAllowed(r, c, true) {
			recipeError(w, 403, "forbidden", "Request origin not allowed.")
			return
		}
		fields, good := decodePairingBody(w, r, map[string]bool{"user_code": true, "challenge_fingerprint": true})
		if !good {
			return
		}
		code, a := decodeRequiredString(fields, "user_code")
		fingerprint, b := decodeRequiredString(fields, "challenge_fingerprint")
		if !a || !b || !pairingCodePattern.MatchString(code) || !pairingFingerprint.MatchString(fingerprint) {
			recipeError(w, 400, "invalid_request", "Invalid pairing approval.")
			return
		}
		if repo == nil {
			recipeError(w, 503, "unavailable", "Pairing is unavailable.")
			return
		}
		ctx, cancel := pairingStoreContext(r)
		defer cancel()
		err := repo.ApprovePairing(ctx, c, identity, DraftPairingApproveRequest{UserCode: code, ChallengeFingerprint: fingerprint})
		switch {
		case err == nil:
			recipeJSON(w, 200, map[string]any{"api_version": draftPairingAPIVersion, "state": "approved"})
		case errors.Is(err, ErrDraftPairingMissing):
			recipeError(w, 404, "not_found", "Pairing code was not found.")
		case errors.Is(err, ErrDraftPairingGone):
			recipeError(w, 410, "pairing_expired", "Pairing is no longer available.")
		default:
			recipeError(w, 503, "unavailable", "Pairing approval was not confirmed.")
		}
	})
}

func DraftGrantsHandler(repo DraftPairingRepository, c DraftPairingConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pairingDisabled(w, c) {
			return
		}
		identity, ok := UserFromContext(r.Context())
		if !ok || identity.Issuer != c.Issuer || identity.Subject == "" {
			recipeError(w, 401, "unauthorized", "Sign in to manage connections.")
			return
		}
		if !pairingOriginAllowed(r, c, true) {
			recipeError(w, 403, "forbidden", "Request origin not allowed.")
			return
		}
		if repo == nil {
			recipeError(w, 503, "unavailable", "Connections are unavailable.")
			return
		}
		owner := RecipeOwner{Issuer: identity.Issuer, Subject: identity.Subject}
		switch r.Method {
		case http.MethodGet:
			if r.URL.RawQuery != "" || r.URL.ForceQuery {
				recipeError(w, 400, "invalid_request", "Query parameters are not supported.")
				return
			}
			ctx, cancel := pairingStoreContext(r)
			defer cancel()
			items, err := repo.ListDraftGrants(ctx, c, owner)
			if err != nil {
				recipeError(w, 503, "unavailable", "Connections are unavailable.")
				return
			}
			if items == nil {
				items = []DraftGrantSummary{}
			}
			recipeJSON(w, 200, map[string]any{"api_version": draftPairingAPIVersion, "grants": items})
		case http.MethodPost:
			if r.URL.RawQuery != "" || r.URL.ForceQuery {
				recipeError(w, 400, "invalid_request", "Query parameters are not supported.")
				return
			}
			fields, good := decodePairingBody(w, r, map[string]bool{"grant_id": true})
			if !good {
				return
			}
			id, valid := decodeRequiredString(fields, "grant_id")
			if !valid || !recipeUUID.MatchString(id) {
				recipeError(w, 400, "invalid_request", "Invalid connection.")
				return
			}
			ctx, cancel := pairingStoreContext(r)
			defer cancel()
			err := repo.RevokeDraftGrant(ctx, c, owner, id)
			if errors.Is(err, ErrDraftPairingMissing) {
				recipeError(w, 404, "not_found", "Connection not found.")
				return
			}
			if err != nil {
				recipeError(w, 503, "unavailable", "Revocation was not confirmed.")
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.Header().Set("Allow", "GET, POST")
			recipeError(w, 405, "method_not_allowed", "Method not allowed.")
		}
	})
}

func challengeFingerprint(challenge string) string {
	h := sha256.Sum256([]byte(challenge))
	return hex.EncodeToString(h[:4])
}

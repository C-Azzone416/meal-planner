package hosted

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	beforeCreateEventType = "user.before_create"
	defaultWebhookBodyMax = int64(64 * 1024)
	defaultJWKSBodyMax    = int64(64 * 1024)
	defaultWebhookMaxAge  = 5 * time.Minute
)

// HTTPDoer is the subset of http.Client used to fetch Neon signing keys.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// AuthWebhookConfig configures the blocking Neon user.before_create webhook.
// JWKSURL exists for deterministic tests; production derives it from BaseURL.
type AuthWebhookConfig struct {
	BaseURL           string
	AllowedEmail      string
	ExpectedEndpoint  string
	JWKSURL           string
	Client            HTTPDoer
	Now               func() time.Time
	MaxBodyBytes      int64
	MaxTimestampDrift time.Duration
}

// AuthBeforeCreateFromEnvironment serves the webhook using server-only
// environment configuration.
func AuthBeforeCreateFromEnvironment(w http.ResponseWriter, r *http.Request) {
	AuthBeforeCreate(AuthWebhookConfig{
		BaseURL:          os.Getenv("NEON_AUTH_BASE_URL"),
		AllowedEmail:     os.Getenv("AUTH_ALLOWED_EMAIL"),
		ExpectedEndpoint: os.Getenv("NEON_AUTH_ENDPOINT_ID"),
	}).ServeHTTP(w, r)
}

// AuthBeforeCreate returns the blocking signup allowlist handler.
func AuthBeforeCreate(config AuthWebhookConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveAuthBeforeCreate(w, r, config)
	})
}

type beforeCreatePayload struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	Context   struct {
		EndpointID string `json:"endpoint_id"`
	} `json:"context"`
	User struct {
		Email string `json:"email"`
	} `json:"user"`
}

type detachedJWSHeader = bearerHeader

type jwksDocument struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	KeyType       string   `json:"kty"`
	Curve         string   `json:"crv"`
	KeyID         string   `json:"kid"`
	X             string   `json:"x"`
	Algorithm     *string  `json:"alg,omitempty"`
	Use           *string  `json:"use,omitempty"`
	KeyOperations []string `json:"key_ops,omitempty"`
}

type beforeCreateResponse struct {
	Allowed      bool   `json:"allowed"`
	ErrorMessage string `json:"error_message,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
}

func serveAuthBeforeCreate(w http.ResponseWriter, r *http.Request, config AuthWebhookConfig) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	baseURL, err := normalizeAuthPolicy(config.BaseURL, config.ExpectedEndpoint, config.AllowedEmail)
	if err != nil {
		http.Error(w, "authentication webhook is not configured", http.StatusServiceUnavailable)
		return
	}
	if contentType := r.Header.Get("Content-Type"); contentType != "" && !strings.HasPrefix(strings.ToLower(contentType), "application/json") {
		http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
		return
	}

	eventType := strings.TrimSpace(r.Header.Get("X-Neon-Event-Type"))
	eventID := strings.TrimSpace(r.Header.Get("X-Neon-Event-Id"))
	timestampText := strings.TrimSpace(r.Header.Get("X-Neon-Timestamp"))
	signature := strings.TrimSpace(r.Header.Get("X-Neon-Signature"))
	signatureKeyID := strings.TrimSpace(r.Header.Get("X-Neon-Signature-Kid"))
	if eventType != beforeCreateEventType || eventID == "" || timestampText == "" || signature == "" || signatureKeyID == "" {
		http.Error(w, "invalid webhook headers", http.StatusBadRequest)
		return
	}

	timestampMillis, err := strconv.ParseInt(timestampText, 10, 64)
	if err != nil {
		http.Error(w, "invalid webhook timestamp", http.StatusBadRequest)
		return
	}
	now := time.Now
	if config.Now != nil {
		now = config.Now
	}
	maxDrift := config.MaxTimestampDrift
	if maxDrift <= 0 {
		maxDrift = defaultWebhookMaxAge
	}
	drift := now().Sub(time.UnixMilli(timestampMillis))
	if drift > maxDrift || drift < -maxDrift {
		http.Error(w, "webhook timestamp is outside the accepted window", http.StatusUnauthorized)
		return
	}

	maxBodyBytes := config.MaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = defaultWebhookBodyMax
	}
	rawBody, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		http.Error(w, "could not read webhook body", http.StatusBadRequest)
		return
	}
	if int64(len(rawBody)) > maxBodyBytes {
		http.Error(w, "webhook body is too large", http.StatusRequestEntityTooLarge)
		return
	}

	jwksURL := strings.TrimSpace(config.JWKSURL)
	if jwksURL == "" {
		jwksURL = baseURL + "/.well-known/jwks.json"
	}
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	publicKey, err := fetchEd25519PublicKey(r.Context(), client, jwksURL, signatureKeyID)
	if err != nil {
		http.Error(w, "could not verify webhook signing key", http.StatusBadGateway)
		return
	}
	if err := verifyDetachedWebhookSignature(rawBody, timestampText, signature, signatureKeyID, publicKey); err != nil {
		http.Error(w, "invalid webhook signature", http.StatusUnauthorized)
		return
	}

	var payload beforeCreatePayload
	if err := decodeAuthJSON(rawBody, authWebhookFields, &payload); err != nil {
		http.Error(w, "invalid webhook payload", http.StatusBadRequest)
		return
	}
	if payload.EventType != eventType || payload.EventID != eventID {
		http.Error(w, "webhook envelope does not match its headers", http.StatusBadRequest)
		return
	}
	if payload.Context.EndpointID != strings.TrimSpace(config.ExpectedEndpoint) {
		http.Error(w, "webhook endpoint does not match this environment", http.StatusBadRequest)
		return
	}

	email := payload.User.Email
	if strings.Trim(email, " \t\r\n") == "" {
		http.Error(w, "webhook payload is missing an email", http.StatusBadRequest)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if !authEmailMatches(email, config.AllowedEmail) {
		_ = json.NewEncoder(w).Encode(beforeCreateResponse{
			Allowed:      false,
			ErrorMessage: "This app is private and is not accepting signups for this email.",
			ErrorCode:    "SIGNUP_NOT_ALLOWED",
		})
		return
	}
	_ = json.NewEncoder(w).Encode(beforeCreateResponse{Allowed: true})
}

func verifyDetachedWebhookSignature(rawBody []byte, timestamp, compactSignature, expectedKeyID string, publicKey ed25519.PublicKey) error {
	parts := strings.Split(compactSignature, ".")
	if len(parts) != 3 || parts[1] != "" || len(parts[0]) > base64.RawURLEncoding.EncodedLen(maxBearerTokenBytes) {
		return errors.New("expected detached JWS")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("decode JWS header: %w", err)
	}
	var header detachedJWSHeader
	if len(headerBytes) > maxBearerTokenBytes {
		return errors.New("JWS header is too large")
	}
	if err := decodeAuthJSON(headerBytes, authHeaderFields, &header); err != nil {
		return fmt.Errorf("parse JWS header: %w", err)
	}
	if !header.supported() || header.KeyID != expectedKeyID {
		return errors.New("unexpected JWS header")
	}
	signatureBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("decode JWS signature: %w", err)
	}
	payloadBase64 := base64.RawURLEncoding.EncodeToString(rawBody)
	timestampedPayload := timestamp + "." + payloadBase64
	timestampedPayloadBase64 := base64.RawURLEncoding.EncodeToString([]byte(timestampedPayload))
	signingInput := parts[0] + "." + timestampedPayloadBase64
	if !ed25519.Verify(publicKey, []byte(signingInput), signatureBytes) {
		return errors.New("signature verification failed")
	}
	return nil
}

func fetchEd25519PublicKey(ctx context.Context, client HTTPDoer, jwksURL, keyID string) (ed25519.PublicKey, error) {
	parsed, err := url.Parse(jwksURL)
	if err != nil || parsed.Host == "" || parsed.Scheme != "https" {
		return nil, errors.New("invalid JWKS URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("JWKS returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, defaultJWKSBodyMax+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > defaultJWKSBodyMax {
		return nil, errors.New("JWKS response is too large")
	}
	var document jwksDocument
	if err := decodeAuthJSON(body, authJWKSFields, &document); err != nil {
		return nil, err
	}
	var publicKey ed25519.PublicKey
	for _, candidate := range document.Keys {
		if candidate.KeyID != keyID {
			continue
		}
		if publicKey != nil {
			return nil, errors.New("ambiguous signing key ID")
		}
		if candidate.KeyType != "OKP" || candidate.Curve != "Ed25519" {
			return nil, errors.New("signing key has an unexpected type")
		}
		if (candidate.Algorithm != nil && *candidate.Algorithm != "EdDSA") ||
			(candidate.Use != nil && *candidate.Use != "sig") ||
			(candidate.KeyOperations != nil && (len(candidate.KeyOperations) != 1 || candidate.KeyOperations[0] != "verify")) {
			return nil, errors.New("signing key does not permit EdDSA verification")
		}
		decoded, err := base64.RawURLEncoding.DecodeString(candidate.X)
		if err != nil || len(decoded) != ed25519.PublicKeySize {
			return nil, errors.New("signing key is invalid")
		}
		publicKey = ed25519.PublicKey(decoded)
	}
	if publicKey != nil {
		return publicKey, nil
	}
	return nil, errSigningKeyNotFound
}

var errSigningKeyNotFound = errors.New("signing key not found")

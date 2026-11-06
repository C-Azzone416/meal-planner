package hosted

import (
	"bytes"
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

const maxBearerTokenBytes = 16 * 1024

var (
	// ErrAuthConfiguration means server-side identity policy is incomplete.
	ErrAuthConfiguration = errors.New("authentication policy is not configured")
	// ErrInvalidBearer means the caller did not present an acceptable identity.
	ErrInvalidBearer = errors.New("bearer token is not authorized")
)

// BearerAuthConfig configures Neon JWT verification for protected APIs.
type BearerAuthConfig struct {
	BaseURL      string
	JWKSURL      string
	AllowedEmail string
	Client       HTTPDoer
	Now          func() time.Time
}

// UserIdentity is the verified caller identity placed on protected requests.
type UserIdentity struct {
	Subject string
	Email   string
}

type bearerContextKey struct{}

type bearerHeader struct {
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
}

type bearerClaims struct {
	Issuer             string          `json:"iss"`
	Audience           json.RawMessage `json:"aud"`
	Expiration         json.Number     `json:"exp"`
	NotBefore          json.Number     `json:"nbf"`
	Subject            string          `json:"sub"`
	Email              string          `json:"email"`
	EmailVerifiedCamel *bool           `json:"emailVerified"`
	EmailVerifiedSnake *bool           `json:"email_verified"`
}

// BearerAuthConfigFromEnvironment returns the server-only policy used by
// protected API handlers.
func BearerAuthConfigFromEnvironment() BearerAuthConfig {
	return BearerAuthConfig{
		BaseURL:      os.Getenv("NEON_AUTH_BASE_URL"),
		AllowedEmail: os.Getenv("AUTH_ALLOWED_EMAIL"),
	}
}

// RequireUser protects an API handler with the complete environment-specific
// Neon identity policy. Product APIs should use this wrapper rather than
// performing ad-hoc token checks.
func RequireUser(next http.Handler) http.Handler {
	return RequireUserWithConfig(BearerAuthConfigFromEnvironment(), next)
}

// RequireUserWithConfig is the injectable form of RequireUser.
func RequireUserWithConfig(config BearerAuthConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, err := VerifyBearer(r.Context(), r.Header.Get("Authorization"), config)
		if err != nil {
			w.Header().Set("Cache-Control", "no-store")
			if errors.Is(err, ErrAuthConfiguration) {
				http.Error(w, "authentication is unavailable", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("WWW-Authenticate", `Bearer realm="meal-planner"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), bearerContextKey{}, identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserFromContext returns the identity established by RequireUser.
func UserFromContext(ctx context.Context) (UserIdentity, bool) {
	identity, ok := ctx.Value(bearerContextKey{}).(UserIdentity)
	return identity, ok
}

// VerifyBearer validates a Neon Ed25519 JWT and the app's environment-specific
// identity restrictions.
func VerifyBearer(ctx context.Context, authorization string, config BearerAuthConfig) (UserIdentity, error) {
	baseURL, err := normalizeAuthBaseURL(config.BaseURL)
	if err != nil || strings.TrimSpace(config.AllowedEmail) == "" {
		return UserIdentity{}, ErrAuthConfiguration
	}
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil {
		return UserIdentity{}, ErrAuthConfiguration
	}
	expectedOrigin := parsedBaseURL.Scheme + "://" + parsedBaseURL.Host

	fields := strings.Fields(authorization)
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") || len(fields[1]) > maxBearerTokenBytes {
		return UserIdentity{}, ErrInvalidBearer
	}
	parts := strings.Split(fields[1], ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return UserIdentity{}, ErrInvalidBearer
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return UserIdentity{}, ErrInvalidBearer
	}
	var header bearerHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil || header.Algorithm != "EdDSA" || header.KeyID == "" {
		return UserIdentity{}, ErrInvalidBearer
	}

	jwksURL := strings.TrimSpace(config.JWKSURL)
	if jwksURL == "" {
		jwksURL = baseURL + "/.well-known/jwks.json"
	}
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	publicKey, err := fetchEd25519PublicKey(ctx, client, jwksURL, header.KeyID)
	if err != nil {
		if errors.Is(err, errSigningKeyNotFound) {
			return UserIdentity{}, ErrInvalidBearer
		}
		return UserIdentity{}, fmt.Errorf("%w: signing keys unavailable", ErrAuthConfiguration)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(publicKey, []byte(parts[0]+"."+parts[1]), signature) {
		return UserIdentity{}, ErrInvalidBearer
	}

	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return UserIdentity{}, ErrInvalidBearer
	}
	decoder := json.NewDecoder(bytes.NewReader(claimsBytes))
	decoder.UseNumber()
	var claims bearerClaims
	if err := decoder.Decode(&claims); err != nil {
		return UserIdentity{}, ErrInvalidBearer
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return UserIdentity{}, ErrInvalidBearer
	}
	if claims.Issuer != expectedOrigin || !audienceContains(claims.Audience, expectedOrigin) {
		return UserIdentity{}, ErrInvalidBearer
	}

	now := time.Now
	if config.Now != nil {
		now = config.Now
	}
	nowUnix := now().Unix()
	expiration, err := numericDate(claims.Expiration)
	if err != nil || expiration <= nowUnix {
		return UserIdentity{}, ErrInvalidBearer
	}
	if string(claims.NotBefore) != "" {
		notBefore, err := numericDate(claims.NotBefore)
		if err != nil || notBefore > nowUnix {
			return UserIdentity{}, ErrInvalidBearer
		}
	}

	email := strings.TrimSpace(claims.Email)
	emailVerified := claims.EmailVerifiedCamel != nil && *claims.EmailVerifiedCamel
	if claims.EmailVerifiedSnake != nil {
		if claims.EmailVerifiedCamel != nil {
			emailVerified = emailVerified && *claims.EmailVerifiedSnake
		} else {
			emailVerified = *claims.EmailVerifiedSnake
		}
	}
	if claims.Subject == "" || !emailVerified || email == "" || !strings.EqualFold(email, strings.TrimSpace(config.AllowedEmail)) {
		return UserIdentity{}, ErrInvalidBearer
	}
	return UserIdentity{Subject: claims.Subject, Email: email}, nil
}

func audienceContains(raw json.RawMessage, expected string) bool {
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return one == expected
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return false
	}
	for _, audience := range many {
		if audience == expected {
			return true
		}
	}
	return false
}

func numericDate(number json.Number) (int64, error) {
	if string(number) == "" {
		return 0, errors.New("numeric date is missing")
	}
	if integer, err := number.Int64(); err == nil {
		return integer, nil
	}
	floating, err := strconv.ParseFloat(string(number), 64)
	if err != nil {
		return 0, err
	}
	return int64(floating), nil
}

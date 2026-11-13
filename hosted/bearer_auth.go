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
	"math/big"
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
// JWKSURL exists for deterministic tests; production derives it from BaseURL.
type BearerAuthConfig struct {
	BaseURL          string
	ExpectedEndpoint string
	JWKSURL          string
	AllowedEmail     string
	// AllowedEmailsJSON is a private JSON array; nil selects legacy AllowedEmail.
	AllowedEmailsJSON *string
	Client            HTTPDoer
	Now               func() time.Time
}

// UserIdentity is the verified caller identity placed on protected requests.
type UserIdentity struct {
	Issuer  string
	Subject string
	Email   string
}

type bearerContextKey struct{}

type bearerHeader struct {
	Algorithm   string   `json:"alg"`
	KeyID       string   `json:"kid"`
	Type        *string  `json:"typ,omitempty"`
	ContentType *string  `json:"cty,omitempty"`
	Critical    []string `json:"crit,omitempty"`
	Base64      *bool    `json:"b64,omitempty"`
}

type bearerClaims struct {
	Issuer             string          `json:"iss"`
	Audience           json.RawMessage `json:"aud"`
	Expiration         json.RawMessage `json:"exp"`
	NotBefore          json.RawMessage `json:"nbf"`
	IssuedAt           json.RawMessage `json:"iat,omitempty"`
	JWTID              string          `json:"jti,omitempty"`
	Subject            string          `json:"sub"`
	Email              string          `json:"email"`
	EmailVerifiedCamel *bool           `json:"emailVerified"`
	EmailVerifiedSnake *bool           `json:"email_verified"`
}

// BearerAuthConfigFromEnvironment returns the server-only policy used by
// protected API handlers.
func BearerAuthConfigFromEnvironment() BearerAuthConfig {
	return BearerAuthConfig{
		BaseURL:           os.Getenv("NEON_AUTH_BASE_URL"),
		ExpectedEndpoint:  os.Getenv("NEON_AUTH_ENDPOINT_ID"),
		AllowedEmail:      os.Getenv("AUTH_ALLOWED_EMAIL"),
		AllowedEmailsJSON: authAllowedEmailsFromEnvironment(),
	}
}

// RequireUser protects an API handler with the complete environment-specific
// Neon identity policy. Product APIs should use this wrapper rather than
// performing ad-hoc token checks.
func RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read membership at request time even when the wrapper is long-lived.
		RequireUserWithConfig(BearerAuthConfigFromEnvironment(), next).ServeHTTP(w, r)
	})
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
	baseURL, members, err := normalizeAuthMembership(config.BaseURL, config.ExpectedEndpoint, config.AllowedEmail, config.AllowedEmailsJSON)
	if err != nil {
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
	if err := decodeAuthJSON(headerBytes, authHeaderFields, &header); err != nil || !header.supported() {
		return UserIdentity{}, ErrInvalidBearer
	}

	// Reject malformed signatures before any provider I/O. A structurally
	// invalid token cannot authenticate, even when the signing service is down.
	// This avoids amplifying trivial invalid requests into remote JWKS fetches;
	// it is not a substitute for deployment-level rate limiting.
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != ed25519.SignatureSize {
		return UserIdentity{}, ErrInvalidBearer
	}

	jwksURL := strings.TrimSpace(config.JWKSURL)
	if jwksURL == "" {
		jwksURL = baseURL + "/.well-known/jwks.json"
	}
	publicKey, err := fetchEd25519PublicKey(ctx, config.Client, jwksURL, header.KeyID)
	if err != nil {
		if errors.Is(err, errSigningKeyNotFound) {
			return UserIdentity{}, ErrInvalidBearer
		}
		return UserIdentity{}, fmt.Errorf("%w: signing keys unavailable", ErrAuthConfiguration)
	}
	if !ed25519.Verify(publicKey, []byte(parts[0]+"."+parts[1]), signature) {
		return UserIdentity{}, ErrInvalidBearer
	}

	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return UserIdentity{}, ErrInvalidBearer
	}
	var claims bearerClaims
	if err := decodeAuthJSON(claimsBytes, authClaimFields, &claims); err != nil {
		return UserIdentity{}, ErrInvalidBearer
	}
	if claims.Issuer != expectedOrigin || !audienceContains(claims.Audience, expectedOrigin) {
		return UserIdentity{}, ErrInvalidBearer
	}

	now := time.Now
	if config.Now != nil {
		now = config.Now
	}
	instant := now()
	nowUnix := new(big.Rat).SetInt64(instant.Unix())
	nowUnix.Add(nowUnix, new(big.Rat).SetFrac64(int64(instant.Nanosecond()), 1_000_000_000))
	expiration, err := numericDate(claims.Expiration)
	if err != nil || expiration.Cmp(nowUnix) <= 0 {
		return UserIdentity{}, ErrInvalidBearer
	}
	if len(claims.NotBefore) != 0 {
		notBefore, err := numericDate(claims.NotBefore)
		if err != nil || notBefore.Cmp(nowUnix) > 0 {
			return UserIdentity{}, ErrInvalidBearer
		}
	}
	// iat is optional and is not an activation time, but when present it must
	// retain the registered NumericDate type rather than being ignored.
	if len(claims.IssuedAt) != 0 {
		if _, err := numericDate(claims.IssuedAt); err != nil {
			return UserIdentity{}, ErrInvalidBearer
		}
	}

	email := claims.Email
	emailVerified := claims.EmailVerifiedCamel != nil && *claims.EmailVerifiedCamel
	if claims.EmailVerifiedSnake != nil {
		if claims.EmailVerifiedCamel != nil {
			emailVerified = emailVerified && *claims.EmailVerifiedSnake
		} else {
			emailVerified = *claims.EmailVerifiedSnake
		}
	}
	_, allowed := members.email(email)
	if claims.Subject == "" || !emailVerified || !allowed {
		return UserIdentity{}, ErrInvalidBearer
	}
	return UserIdentity{Issuer: claims.Issuer, Subject: claims.Subject, Email: strings.Trim(email, " \t\r\n")}, nil
}

func (header bearerHeader) supported() bool {
	// Access-token metadata is distinct from the detached webhook JWS profile.
	return header.supportedSignature() && (header.Type == nil || *header.Type == "JWT")
}

func (header bearerHeader) supportedSignature() bool {
	// Neither auth path implements nested JWTs or another declared payload
	// format. Pointers distinguish omission from explicitly empty metadata.
	// No critical extensions or unencoded payloads are implemented. Unknown
	// noncritical metadata never selects the signing key's source.
	return header.Algorithm == "EdDSA" && header.KeyID != "" &&
		header.ContentType == nil &&
		header.Critical == nil && (header.Base64 == nil || *header.Base64)
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

// numericDate accepts JSON numbers only and compares exact decimal values.
// Bound representation size/exponents and Unix-second range before arithmetic
// to avoid overflow and disproportionate allocations from attacker input.
func numericDate(raw json.RawMessage) (*big.Rat, error) {
	invalid := errors.New("invalid numeric date")
	if len(raw) == 0 || len(raw) > 128 {
		return nil, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, invalid
	}
	number, ok := value.(json.Number)
	if !ok {
		return nil, invalid
	}
	if err := decoder.Decode(&value); err != io.EOF {
		return nil, invalid
	}
	text := string(number)
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		exponent, err := strconv.Atoi(text[i+1:])
		if err != nil || exponent < -1000 || exponent > 1000 {
			return nil, invalid
		}
	}
	date, ok := new(big.Rat).SetString(text)
	if !ok || date.Cmp(new(big.Rat).SetInt64(-1<<63)) < 0 || date.Cmp(new(big.Rat).SetInt64(1<<63-1)) > 0 {
		return nil, invalid
	}
	return date, nil
}

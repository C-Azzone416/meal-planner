package hosted

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultAuthProxyBodyMax     = int64(64 * 1024)
	defaultAuthProxyResponseMax = int64(1024 * 1024)
	defaultAuthProxyTimeout     = 5 * time.Second
	neonAuthCookiePrefix        = "__Secure-neon-auth"
)

var authProxyRoutes = map[string]map[string]bool{
	"email-otp/request-password-reset": {http.MethodPost: true},
	"email-otp/reset-password":         {http.MethodPost: true},
	"get-session":                      {http.MethodGet: true, http.MethodPost: true},
	"revoke-other-sessions":            {http.MethodPost: true},
	"sign-in/email":                    {http.MethodPost: true},
	"sign-out":                         {http.MethodPost: true},
}

var authProxyEmailRoutes = map[string]bool{
	"email-otp/request-password-reset": true,
	"email-otp/reset-password":         true,
	"sign-in/email":                    true,
}

var authProxySilentEmailRoutes = map[string]bool{
	"email-otp/request-password-reset": true,
}

type authProxyBlockedResponse struct {
	Status int
	Body   string
}

var authProxyBlockedEmailResponses = map[string]authProxyBlockedResponse{
	"email-otp/reset-password": {
		Status: http.StatusBadRequest,
		Body:   `{"code":"INVALID_OTP","message":"Invalid OTP"}`,
	},
	"sign-in/email": {
		Status: http.StatusUnauthorized,
		Body:   `{"code":"INVALID_EMAIL_OR_PASSWORD","message":"Invalid email or password"}`,
	},
}

// AuthProxyConfig configures the narrow same-origin proxy used by the browser
// auth client. The provider URL remains server-only.
type AuthProxyConfig struct {
	BaseURL          string
	ExpectedEndpoint string
	AllowedEmail     string
	// AllowedEmailsJSON is a private JSON array; nil selects legacy AllowedEmail.
	AllowedEmailsJSON *string
	Client            HTTPDoer
	MaxBodyBytes      int64
	MaxResponseBytes  int64
}

// AuthProxyFromEnvironment serves the browser auth proxy with private config.
func AuthProxyFromEnvironment(w http.ResponseWriter, r *http.Request) {
	AuthProxy(AuthProxyConfig{
		BaseURL:           os.Getenv("NEON_AUTH_BASE_URL"),
		ExpectedEndpoint:  os.Getenv("NEON_AUTH_ENDPOINT_ID"),
		AllowedEmail:      os.Getenv("AUTH_ALLOWED_EMAIL"),
		AllowedEmailsJSON: authAllowedEmailsFromEnvironment(),
	}).ServeHTTP(w, r)
}

// AuthProxy forwards only the Better Auth methods the Meal Planner UI uses.
// Provider cookies are rewritten as host-only cookies for the application so
// browsers never need to accept a third-party session cookie.
func AuthProxy(config AuthProxyConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveAuthProxy(w, r, config)
	})
}

func serveAuthProxy(w http.ResponseWriter, r *http.Request, config AuthProxyConfig) {
	w.Header().Set("Cache-Control", "no-store")

	baseURL, members, err := normalizeAuthMembership(config.BaseURL, config.ExpectedEndpoint, config.AllowedEmail, config.AllowedEmailsJSON)
	if err != nil {
		http.Error(w, "authentication proxy is not configured", http.StatusServiceUnavailable)
		return
	}

	path := strings.Trim(strings.TrimSpace(r.URL.Query().Get("path")), "/")
	if path == "" && strings.HasPrefix(r.URL.Path, publicAuthProxyBaseURL+"/") {
		path = strings.TrimPrefix(r.URL.Path, publicAuthProxyBaseURL+"/")
	}
	allowedMethods, ok := authProxyRoutes[path]
	if !ok {
		http.Error(w, "authentication route is not available", http.StatusNotFound)
		return
	}
	if !allowedMethods[r.Method] {
		var methods []string
		for method := range allowedMethods {
			methods = append(methods, method)
		}
		w.Header().Set("Allow", strings.Join(methods, ", "))
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	requestOrigin, originAllowed := authProxyRequestOrigin(r)
	if !originAllowed {
		http.Error(w, "origin is not allowed", http.StatusForbidden)
		return
	}

	maxBodyBytes := config.MaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = defaultAuthProxyBodyMax
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		http.Error(w, "could not read authentication request", http.StatusBadRequest)
		return
	}
	if int64(len(body)) > maxBodyBytes {
		http.Error(w, "authentication request is too large", http.StatusRequestEntityTooLarge)
		return
	}
	if authProxyEmailRoutes[path] {
		validatedBody, allowed := authProxyMembershipPayload(body, members)
		if !allowed {
			if authProxySilentEmailRoutes[path] {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"success":true}`))
				return
			}
			if blocked, ok := authProxyBlockedEmailResponses[path]; ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(blocked.Status)
				_, _ = w.Write([]byte(blocked.Body))
				return
			}
			http.Error(w, "authentication request is not allowed", http.StatusForbidden)
			return
		}
		body = validatedBody
	}

	upstream, err := url.Parse(baseURL + "/" + path)
	if err != nil {
		http.Error(w, "authentication proxy is not configured", http.StatusServiceUnavailable)
		return
	}
	upstream.RawQuery = authProxyUpstreamQuery(r.URL.RawQuery)

	request, err := http.NewRequestWithContext(r.Context(), r.Method, upstream.String(), bytes.NewReader(body))
	if err != nil {
		http.Error(w, "could not prepare authentication request", http.StatusBadGateway)
		return
	}
	for _, header := range []string{"Authorization", "Content-Type", "User-Agent", "X-Neon-Client-Info"} {
		if value := r.Header.Get(header); value != "" {
			request.Header.Set(header, value)
		}
	}
	request.Header.Set("Origin", requestOrigin)
	if cookies := extractNeonAuthCookies(r.Header.Get("Cookie")); cookies != "" {
		request.Header.Set("Cookie", cookies)
	}
	request.Header.Set("X-Neon-Auth-Middleware", "true")

	client := config.Client
	if client == nil {
		client = &http.Client{
			Timeout: defaultAuthProxyTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	response, err := client.Do(request)
	if err != nil {
		http.Error(w, "authentication service is unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()

	maxResponseBytes := config.MaxResponseBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = defaultAuthProxyResponseMax
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || int64(len(responseBody)) > maxResponseBytes {
		http.Error(w, "authentication service returned an invalid response", http.StatusBadGateway)
		return
	}

	stagedHeaders := make(http.Header)
	for _, header := range []string{"Content-Type", "Content-Encoding", "Date", "Set-Auth-Jwt", "Set-Auth-Token", "X-Neon-Ret-Request-Id"} {
		for _, value := range response.Header.Values(header) {
			stagedHeaders.Add(header, value)
		}
	}
	var cookies []string
	for _, cookie := range response.Header.Values("Set-Cookie") {
		if !strings.HasPrefix(strings.TrimSpace(cookie), neonAuthCookiePrefix) {
			continue
		}
		rewritten, err := rewriteAuthProxyCookie(cookie)
		if err != nil {
			http.Error(w, "authentication service returned an invalid cookie", http.StatusBadGateway)
			return
		}
		cookies = append(cookies, rewritten)
	}
	for _, cookie := range cookies {
		stagedHeaders.Add("Set-Cookie", cookie)
	}
	for header, values := range stagedHeaders {
		for _, value := range values {
			w.Header().Add(header, value)
		}
	}

	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(responseBody)
}

// authProxyEmailPayload requires one unambiguous lowercase email field and
// forwards the identity actually checked by the allowlist. A struct unmarshal
// would accept case-insensitive keys, unlike the upstream JavaScript parser.
func authProxyEmailPayload(body []byte, allowedEmail string) ([]byte, bool) {
	members, err := parseAuthMembership(allowedEmail, nil)
	if err != nil {
		return nil, false
	}
	return authProxyMembershipPayload(body, members)
}

func authProxyMembershipPayload(body []byte, members authMembership) ([]byte, bool) {
	payload := make(map[string]json.RawMessage)
	if err := decodeAuthJSON(body, authEmailFields, &payload); err != nil {
		return nil, false
	}
	var email string
	if err := json.Unmarshal(payload["email"], &email); err != nil {
		return nil, false
	}
	canonicalEmail, allowed := members.email(email)
	if !allowed {
		return nil, false
	}
	// Preserve other SDK fields without numeric conversion, while giving the
	// provider the configured identity rather than differently normalized input.
	payload["email"], _ = json.Marshal(canonicalEmail)
	canonical, err := json.Marshal(payload)
	return canonical, err == nil
}

func authProxyUpstreamQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	parts := strings.Split(rawQuery, "&")
	kept := parts[:0]
	for _, part := range parts {
		name := part
		if index := strings.IndexByte(name, '='); index >= 0 {
			name = name[:index]
		}
		decoded, err := url.QueryUnescape(name)
		if err == nil && decoded == "path" {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, "&")
}

func extractNeonAuthCookies(header string) string {
	var cookies []string
	for _, raw := range strings.Split(header, ";") {
		pair := strings.TrimSpace(raw)
		name, _, ok := strings.Cut(pair, "=")
		if ok && strings.HasPrefix(name, neonAuthCookiePrefix) {
			cookies = append(cookies, pair)
		}
	}
	return strings.Join(cookies, "; ")
}

func authProxyRequestOrigin(r *http.Request) (string, bool) {
	if r.Host == "" || strings.ContainsAny(r.Host, "/\\@") {
		return "", false
	}
	scheme := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	if scheme != "https" && scheme != "http" {
		return "", false
	}
	requestOrigin := (&url.URL{Scheme: scheme, Host: r.Host}).String()
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return requestOrigin, true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	return requestOrigin, parsed.Scheme == scheme && parsed.Host == r.Host
}

func rewriteAuthProxyCookie(header string) (string, error) {
	parts := strings.Split(header, ";")
	if len(parts) == 0 || !strings.Contains(parts[0], "=") {
		return "", errors.New("invalid set-cookie header")
	}
	rewritten := []string{strings.TrimSpace(parts[0])}
	sawPath := false
	sawSameSite := false
	sawSecure := false
	sawHTTPOnly := false
	for _, raw := range parts[1:] {
		attribute := strings.TrimSpace(raw)
		if attribute == "" {
			continue
		}
		lower := strings.ToLower(attribute)
		switch {
		case strings.HasPrefix(lower, "domain="):
			continue
		case strings.HasPrefix(lower, "path="):
			rewritten = append(rewritten, "Path=/")
			sawPath = true
		case strings.HasPrefix(lower, "samesite="):
			rewritten = append(rewritten, "SameSite=Lax")
			sawSameSite = true
		case lower == "partitioned":
			continue
		case lower == "secure":
			rewritten = append(rewritten, "Secure")
			sawSecure = true
		case lower == "httponly":
			rewritten = append(rewritten, "HttpOnly")
			sawHTTPOnly = true
		default:
			rewritten = append(rewritten, attribute)
		}
	}
	if !sawPath {
		rewritten = append(rewritten, "Path=/")
	}
	if !sawSameSite {
		rewritten = append(rewritten, "SameSite=Lax")
	}
	if !sawSecure {
		rewritten = append(rewritten, "Secure")
	}
	if !sawHTTPOnly {
		rewritten = append(rewritten, "HttpOnly")
	}
	if strings.TrimSpace(rewritten[0]) == "=" {
		return "", fmt.Errorf("invalid cookie pair")
	}
	return strings.Join(rewritten, "; "), nil
}

package hosted

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// AuthConfigResponse contains the public configuration needed to initialize
// the browser auth client. It must never include secrets or allowlist values.
type AuthConfigResponse struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"baseUrl,omitempty"`
}

const publicAuthProxyBaseURL = "/api/auth-proxy"

// AuthConfig serves GET /api/auth-config.
func AuthConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")

	configured := strings.TrimSpace(os.Getenv("NEON_AUTH_BASE_URL"))
	allowedEmail := strings.TrimSpace(os.Getenv("AUTH_ALLOWED_EMAIL"))
	endpointID := strings.TrimSpace(os.Getenv("NEON_AUTH_ENDPOINT_ID"))
	if configured == "" || allowedEmail == "" || endpointID == "" {
		_ = json.NewEncoder(w).Encode(AuthConfigResponse{Enabled: false})
		return
	}

	_, err := normalizeManagedAuthBaseURL(configured, endpointID)
	if err != nil {
		http.Error(w, "authentication is not configured correctly", http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(AuthConfigResponse{
		Enabled: true,
		BaseURL: publicAuthProxyBaseURL,
	})
}

var (
	managedAuthEndpointPattern = regexp.MustCompile(`^ep-[a-z0-9-]+$`)
	managedAuthHostPattern     = regexp.MustCompile(`^([a-z0-9-]+)\.neonauth\.c-[0-9]+\.[a-z0-9-]+\.aws\.neon\.tech$`)
	managedAuthDatabasePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

func normalizeManagedAuthBaseURL(value, expectedEndpoint string) (string, error) {
	baseURL, err := normalizeAuthBaseURL(value)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimSpace(expectedEndpoint)
	hostMatches := managedAuthHostPattern.FindStringSubmatch(strings.ToLower(parsed.Hostname()))
	pathParts := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	if !managedAuthEndpointPattern.MatchString(endpoint) ||
		parsed.Port() != "" ||
		parsed.RawPath != "" ||
		len(hostMatches) != 2 ||
		hostMatches[1] != endpoint ||
		len(pathParts) != 2 ||
		!managedAuthDatabasePattern.MatchString(pathParts[0]) ||
		pathParts[1] != "auth" {
		return "", errInvalidManagedAuthBaseURL
	}
	return baseURL, nil
}

func normalizeAuthBaseURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", &url.Error{Op: "parse", URL: value, Err: errInvalidAuthBaseURL}
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = strings.TrimRight(parsed.RawPath, "/")
	return parsed.String(), nil
}

type authConfigError string

func (e authConfigError) Error() string { return string(e) }

const errInvalidAuthBaseURL = authConfigError("invalid auth base URL")
const errInvalidManagedAuthBaseURL = authConfigError("invalid managed auth base URL")

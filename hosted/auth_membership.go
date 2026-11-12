package hosted

import (
	"encoding/json"
	"os"
	"strings"
)

const (
	maxAuthMembershipBytes = 32 * 1024
	maxAuthMembers         = 100
)

// authMembership is a request-local, immutable set. Nothing caches membership
// or token authorization between requests, including after a successful login.
type authMembership map[string]struct{}

// A pointer distinguishes an absent setting (legacy migration) from an
// explicitly empty/broken setting (configuration failure, never fallback).
func authAllowedEmailsFromEnvironment() *string {
	value, present := os.LookupEnv("AUTH_ALLOWED_EMAILS")
	if !present {
		return nil
	}
	return &value
}

// normalizeAuthMembership is the shared policy for readiness, email proxying,
// account creation and every bearer authorization. Exactly one storage format
// may be configured; retaining a legacy value cannot silently restore a member.
func normalizeAuthMembership(baseURL, endpoint, legacyEmail string, emailsJSON *string) (string, authMembership, error) {
	members, err := parseAuthMembership(legacyEmail, emailsJSON)
	if err != nil {
		return "", nil, ErrAuthConfiguration
	}
	baseURL, err = normalizeManagedAuthBaseURL(baseURL, endpoint)
	if err != nil {
		return "", nil, err
	}
	return baseURL, members, nil
}

func parseAuthMembership(legacyEmail string, emailsJSON *string) (authMembership, error) {
	emails := []string{legacyEmail}
	if emailsJSON != nil {
		if legacyEmail != "" || len(*emailsJSON) > maxAuthMembershipBytes || !validAuthJSONUnicode([]byte(*emailsJSON)) {
			return nil, ErrAuthConfiguration
		}
		if err := json.Unmarshal([]byte(*emailsJSON), &emails); err != nil {
			return nil, ErrAuthConfiguration
		}
	}
	if len(emails) == 0 || len(emails) > maxAuthMembers {
		return nil, ErrAuthConfiguration
	}
	members := make(authMembership, len(emails))
	for _, email := range emails {
		// Keep the existing conservative configuration format: exact lowercase
		// ASCII, without padding, display names, domains or wildcard entries.
		local, _, _ := strings.Cut(email, "@")
		if len(email) > 254 || len(local) > 64 || !allowedAuthEmailPattern.MatchString(email) {
			return nil, ErrAuthConfiguration
		}
		if _, duplicate := members[email]; duplicate {
			return nil, ErrAuthConfiguration
		}
		members[email] = struct{}{}
	}
	return members, nil
}

func (members authMembership) email(email string) (string, bool) {
	canonical, ok := canonicalAuthEmail(email)
	if !ok {
		return "", false
	}
	_, allowed := members[canonical]
	return canonical, allowed
}

// Reject Unicode before trimming/lowercasing: Unicode SimpleFold aliases and
// Unicode whitespace must never become aliases of configured ASCII identities.
func canonicalAuthEmail(email string) (string, bool) {
	for i := range len(email) {
		if email[i] >= 0x80 {
			return "", false
		}
	}
	email = strings.ToLower(strings.Trim(email, " \t\r\n"))
	return email, email != ""
}

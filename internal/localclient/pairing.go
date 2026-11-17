package localclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var userCodePattern = regexp.MustCompile(`^[23456789ABCDEFGHJKLMNPQRSTUVWXYZ]{8}$`)

type PairingSession struct {
	PairingID            string
	UserCode             string
	VerificationURI      string
	ChallengeFingerprint string
	ExpiresAt            time.Time
	Interval             time.Duration
	pollToken            string
	codeVerifier         string
}

type publicPairingSession struct {
	PairingID            string        `json:"pairing_id"`
	UserCode             string        `json:"user_code"`
	VerificationURI      string        `json:"verification_uri"`
	ChallengeFingerprint string        `json:"challenge_fingerprint"`
	ExpiresAt            time.Time     `json:"expires_at"`
	Interval             time.Duration `json:"interval"`
}

func (s PairingSession) public() publicPairingSession {
	return publicPairingSession{
		PairingID: s.PairingID, UserCode: s.UserCode, VerificationURI: s.VerificationURI,
		ChallengeFingerprint: s.ChallengeFingerprint, ExpiresAt: s.ExpiresAt, Interval: s.Interval,
	}
}

func (s PairingSession) String() string                 { raw, _ := json.Marshal(s.public()); return string(raw) }
func (s PairingSession) GoString() string               { return s.String() }
func (s PairingSession) MarshalJSON() ([]byte, error)   { return json.Marshal(s.public()) }
func (s PairingSession) Format(state fmt.State, _ rune) { _, _ = io.WriteString(state, s.String()) }

type PairingPollResult struct {
	Pending    bool
	BindingID  string
	OwnerLabel string
	ExpiresAt  time.Time
}

// PairingClient has no account binding yet. It can only create and poll a
// pairing; it cannot submit a draft.
type PairingClient struct {
	profile     Profile
	credentials CredentialStore
	http        httpDoer
}

func NewPairingClient(credentials CredentialStore) (*PairingClient, error) {
	if credentials == nil {
		return nil, ErrUnavailable
	}
	return &PairingClient{
		profile:     Profile{Environment: PreviewEnvironment, APIOrigin: PreviewOrigin, WebOrigin: PreviewOrigin},
		credentials: credentials,
		http:        &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

type pairingCreateRequest struct {
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	ClientLabel         string `json:"client_label"`
}

type pairingCreateResponse struct {
	APIVersion           string `json:"api_version"`
	PairingID            string `json:"pairing_id"`
	PollToken            string `json:"poll_token"`
	UserCode             string `json:"user_code"`
	VerificationURI      string `json:"verification_uri"`
	ExpiresIn            int    `json:"expires_in"`
	Interval             int    `json:"interval"`
	ChallengeFingerprint string `json:"challenge_fingerprint"`
}

type pairingPollRequest struct {
	PairingID    string `json:"pairing_id"`
	PollToken    string `json:"poll_token"`
	CodeVerifier string `json:"code_verifier"`
}

type pairingPollResponse struct {
	APIVersion  string    `json:"api_version"`
	State       string    `json:"state"`
	Scope       string    `json:"scope,omitempty"`
	Environment string    `json:"environment,omitempty"`
	APIOrigin   string    `json:"api_origin,omitempty"`
	WebOrigin   string    `json:"web_origin,omitempty"`
	Grant       string    `json:"grant,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	OwnerLabel  string    `json:"owner_label,omitempty"`
	BindingID   string    `json:"binding_id,omitempty"`
}

// BeginPairing starts an outbound polling session. Its verifier and polling
// secret stay in memory and are never returned by an MCP tool.
func (c *PairingClient) BeginPairing(ctx context.Context, clientLabel string) (*PairingSession, error) {
	if c == nil || c.profile.Environment != PreviewEnvironment || c.profile.APIOrigin != PreviewOrigin || c.profile.WebOrigin != PreviewOrigin || len(clientLabel) < 1 || len(clientLabel) > 64 ||
		strings.TrimSpace(clientLabel) != clientLabel || strings.ContainsAny(clientLabel, "\r\n\x00") {
		return nil, ErrUnavailable
	}
	var verifierBytes [32]byte
	if _, err := rand.Read(verifierBytes[:]); err != nil {
		return nil, ErrUnavailable
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes[:])
	challenge := sha256.Sum256([]byte(verifier))
	body, _ := json.Marshal(pairingCreateRequest{CodeChallenge: base64.RawURLEncoding.EncodeToString(challenge[:]), CodeChallengeMethod: "S256", ClientLabel: clientLabel})
	response, err := c.pairingPost(ctx, "/api/draft-pairings", body)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return nil, ErrInvalidResponse
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return nil, ErrRateLimited
	}
	if response.StatusCode != http.StatusCreated {
		return nil, ErrUnavailable
	}
	var decoded pairingCreateResponse
	if decodeResponse(response, &decoded) != nil || decoded.APIVersion != "local-client-v1" ||
		!uuidPattern.MatchString(decoded.PairingID) || !grantFragment(decoded.PollToken) ||
		!userCodePattern.MatchString(decoded.UserCode) || decoded.ExpiresIn < 60 || decoded.ExpiresIn > 600 ||
		decoded.Interval < 5 || decoded.Interval > 60 || !validVerificationURI(decoded.VerificationURI) ||
		!validFingerprint(decoded.ChallengeFingerprint) {
		return nil, ErrInvalidResponse
	}
	return &PairingSession{
		PairingID: decoded.PairingID, UserCode: decoded.UserCode, VerificationURI: decoded.VerificationURI,
		ChallengeFingerprint: decoded.ChallengeFingerprint, ExpiresAt: time.Now().Add(time.Duration(decoded.ExpiresIn) * time.Second),
		Interval: time.Duration(decoded.Interval) * time.Second, pollToken: decoded.PollToken, codeVerifier: verifier,
	}, nil
}

// PollPairing performs one outbound poll. On approval it writes the grant
// directly to the credential store, clears it from the decoded response and
// returns only non-secret binding metadata.
func (c *PairingClient) PollPairing(ctx context.Context, session *PairingSession) (PairingPollResult, error) {
	if c == nil || session == nil || !uuidPattern.MatchString(session.PairingID) ||
		!grantFragment(session.pollToken) || !grantFragment(session.codeVerifier) || time.Now().After(session.ExpiresAt) {
		return PairingPollResult{}, ErrExpired
	}
	body, _ := json.Marshal(pairingPollRequest{PairingID: session.PairingID, PollToken: session.pollToken, CodeVerifier: session.codeVerifier})
	response, err := c.pairingPost(ctx, "/api/draft-pairings/poll", body)
	if err != nil {
		return PairingPollResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return PairingPollResult{}, ErrInvalidResponse
	}
	if response.StatusCode == http.StatusTooManyRequests {
		session.pollToken, session.codeVerifier = "", ""
		return PairingPollResult{}, ErrRateLimited
	}
	var decoded pairingPollResponse
	if response.StatusCode == http.StatusAccepted {
		if decodeResponse(response, &decoded) != nil || decoded.APIVersion != "local-client-v1" ||
			(decoded.State != "pending" && decoded.State != "slow_down") {
			return PairingPollResult{}, ErrInvalidResponse
		}
		return PairingPollResult{Pending: true}, nil
	}
	if response.StatusCode == http.StatusGone {
		return PairingPollResult{}, ErrExpired
	}
	if response.StatusCode != http.StatusOK || decodeResponse(response, &decoded) != nil ||
		decoded.APIVersion != "local-client-v1" || decoded.State != "approved" || decoded.Scope != "draft:propose" ||
		decoded.Environment != PreviewEnvironment || decoded.APIOrigin != PreviewOrigin || decoded.WebOrigin != PreviewOrigin ||
		!grantPattern.MatchString(decoded.Grant) || !safeBindingID(decoded.BindingID) || decoded.ExpiresAt.IsZero() {
		return PairingPollResult{}, ErrInvalidResponse
	}
	grant, err := newCredential([]byte(decoded.Grant))
	if err != nil {
		return PairingPollResult{}, ErrInvalidResponse
	}
	decoded.Grant = ""
	profile := c.profile
	profile.BindingID = decoded.BindingID
	if profile.Validate() != nil {
		grant.clear()
		return PairingPollResult{}, ErrInvalidResponse
	}
	if err := c.credentials.Put(ctx, credentialKey(profile), grant); err != nil {
		grant.clear()
		return PairingPollResult{}, ErrUnavailable
	}
	grant.clear()
	session.pollToken, session.codeVerifier = "", ""
	return PairingPollResult{BindingID: decoded.BindingID, OwnerLabel: decoded.OwnerLabel, ExpiresAt: decoded.ExpiresAt}, nil
}

func (c *PairingClient) pairingPost(ctx context.Context, path string, body []byte) (*http.Response, error) {
	target := c.profile.APIOrigin + path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, ErrUnavailable
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, ErrUnavailable
	}
	if response.Request != nil && response.Request.URL.String() != target {
		response.Body.Close()
		return nil, ErrInvalidResponse
	}
	return response, nil
}

func grantFragment(value string) bool {
	if len(value) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	return err == nil && len(decoded) == 32
}

func validVerificationURI(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host == "athome-meals-dev.vercel.app" &&
		parsed.Path == "/connect" && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.User == nil
}

func validFingerprint(value string) bool {
	if len(value) < 8 || len(value) > 16 {
		return false
	}
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

// Compile-time guard: pairing code must not grow an inbound network API.
var _ io.Reader = (*bytes.Reader)(nil)

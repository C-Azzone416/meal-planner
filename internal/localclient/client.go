package localclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

const maxResponseBytes = 256 * 1024

var (
	grantPattern = regexp.MustCompile(`^mp_lc1_pv_[A-Za-z0-9_-]{43}$`)
	uuidPattern  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Client performs one outbound operation per Send call. A pending send is not
// polled in the background; repeating the explicit MCP call polls it once.
type Client struct {
	profile     Profile
	credentials CredentialStore
	ledger      *Ledger
	http        httpDoer
}

func New(profile Profile, credentials CredentialStore, ledger *Ledger) (*Client, error) {
	if profile.Validate() != nil || credentials == nil || ledger == nil {
		return nil, ErrUnavailable
	}
	return &Client{
		profile: profile, credentials: credentials, ledger: ledger,
		http: &http.Client{
			Timeout:       15 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// newWithDoer is a test seam. Production always uses New's no-redirect client.
func newWithDoer(profile Profile, credentials CredentialStore, ledger *Ledger, doer httpDoer) (*Client, error) {
	client, err := New(profile, credentials, ledger)
	if err == nil && doer != nil {
		client.http = doer
	}
	return client, err
}

type submission struct {
	ContentSchemaVersion int           `json:"content_schema_version"`
	Content              draft.Content `json:"content"`
}

type intentCreateResponse struct {
	APIVersion string `json:"api_version"`
	Intent     struct {
		ID          string    `json:"id"`
		State       string    `json:"state"`
		Revision    int64     `json:"revision"`
		ContentHash string    `json:"content_hash"`
		ExpiresAt   time.Time `json:"expires_at"`
	} `json:"intent"`
	Approval struct {
		Path string `json:"path"`
	} `json:"approval"`
}

type pollRequest struct {
	IntentID string `json:"intent_id"`
}

type pollResponse struct {
	APIVersion string `json:"api_version"`
	State      string `json:"state"`
	Intent     *struct {
		ID    string `json:"id"`
		State string `json:"state"`
	} `json:"intent,omitempty"`
	Receipt *struct {
		ReceiptID        string    `json:"receipt_id"`
		DraftID          string    `json:"draft_id"`
		AcceptedRevision int64     `json:"accepted_revision"`
		SubmittedAt      time.Time `json:"submitted_at"`
	} `json:"receipt,omitempty"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error,omitempty"`
}

func (c *Client) Send(ctx context.Context, local draft.Draft) (SendResult, error) {
	if c == nil || c.profile.Validate() != nil || !draft.ValidID(local.ID) || local.Revision < 1 ||
		local.Status != draft.StatusDraft || local.SchemaVersion != draft.SchemaVersion ||
		!draft.Validate(local.Content).StructurallyValid {
		return SendResult{}, ErrUnavailable
	}
	body, err := json.Marshal(submission{ContentSchemaVersion: draft.SchemaVersion, Content: local.Content})
	if err != nil {
		return SendResult{}, ErrUnavailable
	}
	digest := sha256.Sum256(body)
	scope := ledgerScope{
		Environment: c.profile.Environment, APIOrigin: c.profile.APIOrigin, BindingID: c.profile.BindingID,
		LocalID: local.ID, Revision: local.Revision, ContentHash: hex.EncodeToString(digest[:]),
	}
	record, err := c.ledger.Begin(ctx, scope)
	if err != nil {
		return SendResult{}, safeError(err)
	}
	if record.State == "saved" {
		return savedResult(record), nil
	}
	credential, err := c.credentials.Get(ctx, credentialKey(c.profile))
	if err != nil || !credential.valid() {
		credential.clear()
		return SendResult{}, ErrCredentialMissing
	}
	defer credential.clear()
	if record.IntentID == "" {
		return c.createIntent(ctx, credential, body, record)
	}
	return c.pollIntent(ctx, credential, record)
}

func (c *Client) createIntent(ctx context.Context, credential Credential, body []byte, record ledgerRecord) (SendResult, error) {
	response, err := c.post(ctx, "/api/draft-send-intents", credential, record.OperationKey, body)
	if err != nil {
		return SendResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return SendResult{}, ErrInvalidResponse
	}
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		return SendResult{}, responseError(response)
	}
	var decoded intentCreateResponse
	if decodeResponse(response, &decoded) != nil || decoded.APIVersion != "draft-intent-v1" ||
		!uuidPattern.MatchString(decoded.Intent.ID) || decoded.Intent.State != "pending" ||
		decoded.Intent.Revision < 1 || decoded.Intent.ContentHash != record.Scope.ContentHash ||
		!decoded.Intent.ExpiresAt.After(time.Now().Add(-time.Minute)) || !validApprovalPath(decoded.Approval.Path, decoded.Intent.ID) {
		return SendResult{}, ErrInvalidResponse
	}
	record.IntentID = decoded.Intent.ID
	record.ApprovalPath = decoded.Approval.Path
	record.State = "pending"
	if err := c.ledger.Update(ctx, record); err != nil {
		// The server accepted the idempotent operation, but a durable local receipt
		// was not committed. Retrying reuses the operation key from Begin.
		return SendResult{}, ErrUnavailable
	}
	return pendingResult(c.profile, record), nil
}

func (c *Client) pollIntent(ctx context.Context, credential Credential, record ledgerRecord) (SendResult, error) {
	body, _ := json.Marshal(pollRequest{IntentID: record.IntentID})
	response, err := c.post(ctx, "/api/draft-send-intents/poll", credential, "", body)
	if err != nil {
		return SendResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return SendResult{}, ErrInvalidResponse
	}
	var decoded pollResponse
	if response.StatusCode == http.StatusAccepted {
		if decodeResponse(response, &decoded) != nil || decoded.APIVersion != "draft-intent-v1" ||
			(decoded.State != "pending" && (decoded.Intent == nil || decoded.Intent.State != "pending")) {
			return SendResult{}, ErrInvalidResponse
		}
		return pendingResult(c.profile, record), nil
	}
	if response.StatusCode != http.StatusOK {
		if decodeResponse(response, &decoded) == nil && decoded.Error != nil {
			switch decoded.Error.Code {
			case "intent_denied":
				return SendResult{}, ErrDenied
			case "intent_expired":
				return SendResult{}, ErrExpired
			}
		}
		return SendResult{}, ErrUnavailable
	}
	if decodeResponse(response, &decoded) != nil || decoded.APIVersion != "draft-intent-v1" ||
		decoded.Receipt == nil || !uuidPattern.MatchString(decoded.Receipt.ReceiptID) ||
		!uuidPattern.MatchString(decoded.Receipt.DraftID) || decoded.Receipt.AcceptedRevision < 1 ||
		decoded.Receipt.SubmittedAt.IsZero() {
		return SendResult{}, ErrInvalidResponse
	}
	record.State = "saved"
	record.ReceiptID = decoded.Receipt.ReceiptID
	record.HostedDraftID = decoded.Receipt.DraftID
	if err := c.ledger.Update(ctx, record); err != nil {
		return SendResult{}, ErrUnavailable
	}
	return savedResult(record), nil
}

func (c *Client) post(ctx context.Context, path string, credential Credential, operationKey string, body []byte) (*http.Response, error) {
	target := c.profile.APIOrigin + path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, ErrUnavailable
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", credential.authorization())
	if operationKey != "" {
		request.Header.Set("Idempotency-Key", operationKey)
	}
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

func decodeResponse(response *http.Response, into any) error {
	reader := io.LimitReader(response.Body, maxResponseBytes+1)
	raw, err := io.ReadAll(reader)
	if err != nil || len(raw) > maxResponseBytes {
		return ErrInvalidResponse
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(into) != nil || decoder.Decode(new(any)) != io.EOF {
		return ErrInvalidResponse
	}
	return nil
}

func responseError(response *http.Response) error {
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if decodeResponse(response, &body) == nil {
		switch body.Error.Code {
		case "intent_denied":
			return ErrDenied
		case "intent_expired":
			return ErrExpired
		case "credential_revoked", "unauthorized":
			return ErrCredentialMissing
		}
	}
	return ErrUnavailable
}

func validApprovalPath(path, id string) bool {
	parsed, err := url.Parse(path)
	return err == nil && !parsed.IsAbs() && parsed.RawQuery == "" && parsed.Fragment == "" &&
		parsed.Path == "/drafts/incoming/"+id
}

func pendingResult(profile Profile, record ledgerRecord) SendResult {
	return SendResult{
		State: SendPending, IntentID: record.IntentID,
		ApprovalURL: strings.TrimSuffix(profile.WebOrigin, "/") + record.ApprovalPath,
		Message:     "Waiting for the signed-in owner to review and approve this exact snapshot in Preview.",
	}
}

func savedResult(record ledgerRecord) SendResult {
	return SendResult{State: SendSaved, ReceiptID: record.ReceiptID, HostedDraftID: record.HostedDraftID,
		Message: "Saved to Preview as an unreviewed draft."}
}

func safeError(err error) error {
	if err == context.Canceled || err == context.DeadlineExceeded {
		return err
	}
	return ErrUnavailable
}

// Package localclient implements the outbound-only Preview draft bridge.
//
// It never opens a listener, accepts a credential through arguments or the
// environment, or fetches recipe source URLs. Runtime construction is kept
// separate from the MCP adapter so the bridge is absent unless an approved
// credential store and an exact pinned profile are explicitly injected.
package localclient

import (
	"context"
	"errors"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

const (
	PreviewEnvironment = "preview"
	PreviewOrigin      = "https://athome-meals-dev.vercel.app"
)

var (
	ErrUnavailable       = errors.New("the Preview draft bridge is unavailable")
	ErrCredentialMissing = errors.New("the Preview send credential is unavailable")
	ErrInvalidResponse   = errors.New("the Preview draft service returned an invalid response")
	ErrDenied            = errors.New("the owner denied this draft send")
	ErrExpired           = errors.New("the draft send approval expired")
	ErrRevisionChanged   = errors.New("the local draft changed before it could be sent")
)

// Profile is a compiled, non-secret destination. Validate accepts only the
// single approved Preview origin; there is deliberately no Production profile.
type Profile struct {
	Environment string
	APIOrigin   string
	WebOrigin   string
	BindingID   string
}

func (p Profile) Validate() error {
	if p.Environment != PreviewEnvironment || p.APIOrigin != PreviewOrigin ||
		p.WebOrigin != PreviewOrigin || !safeBindingID(p.BindingID) {
		return ErrUnavailable
	}
	return nil
}

// CredentialKey identifies one exact environment/origin/account binding. It
// contains no credential and is safe to persist.
type CredentialKey struct {
	Environment string
	APIOrigin   string
	WebOrigin   string
	BindingID   string
}

// CredentialStore is the only source of send grants. Implementations must not
// use a plaintext file fallback. The native macOS implementation remains
// explicitly unwired until signing/ACL behavior is independently verified.
type CredentialStore interface {
	Get(context.Context, CredentialKey) (Credential, error)
	Put(context.Context, CredentialKey, Credential) error
	Delete(context.Context, CredentialKey) error
}

// UnavailableCredentialStore is the production-safe default before the OS
// credential adapter and its ACL/signing evidence are approved.
type UnavailableCredentialStore struct{}

func (UnavailableCredentialStore) Get(context.Context, CredentialKey) (Credential, error) {
	return Credential{}, ErrCredentialMissing
}
func (UnavailableCredentialStore) Put(context.Context, CredentialKey, Credential) error {
	return ErrUnavailable
}
func (UnavailableCredentialStore) Delete(context.Context, CredentialKey) error { return ErrUnavailable }

// Sender is the narrow interface injected into the MCP adapter. Implementations
// choose the account/profile and obtain the credential internally; tool callers
// can provide only a local draft ID and revision.
type Sender interface {
	Send(context.Context, draft.Draft) (SendResult, error)
}

type SendState string

const (
	SendPending SendState = "pending_owner_approval"
	SendSaved   SendState = "saved_unreviewed_draft"
)

// SendResult is deliberately safe for an MCP result. It never includes a grant,
// polling secret, operation key, credential-store account, request body or recipe
// content.
type SendResult struct {
	State         SendState `json:"state"`
	IntentID      string    `json:"intent_id,omitempty"`
	ApprovalURL   string    `json:"approval_url,omitempty"`
	ReceiptID     string    `json:"receipt_id,omitempty"`
	HostedDraftID string    `json:"hosted_draft_id,omitempty"`
	Message       string    `json:"message"`
}

type ledgerScope struct {
	Environment string
	APIOrigin   string
	BindingID   string
	LocalID     string
	Revision    int64
	ContentHash string
}

type ledgerRecord struct {
	Version       int         `json:"version"`
	Scope         ledgerScope `json:"scope"`
	OperationKey  string      `json:"operation_key"`
	IntentID      string      `json:"intent_id,omitempty"`
	ApprovalPath  string      `json:"approval_path,omitempty"`
	State         string      `json:"state"`
	ReceiptID     string      `json:"receipt_id,omitempty"`
	HostedDraftID string      `json:"hosted_draft_id,omitempty"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

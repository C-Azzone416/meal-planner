package draft

import "time"

const (
	// SchemaVersion is the version of the stored and exchanged data contract. It is unrelated to
	// the application version in internal/core/version.
	SchemaVersion = 1
	// StatusDraft is the only status: every saved object is a draft.
	StatusDraft = "draft"
	// ValidationScope is the scope of every validation result.
	ValidationScope = "structural"
	// ValidationNotice is attached to every validation result.
	ValidationNotice = "Structural check only. This is not food-safety verification, recipe review, or approval."
)

// SupportedSchemaVersions lists the versions this build can read.
var SupportedSchemaVersions = []int{SchemaVersion}

// Draft is the stored and returned object: server-assigned envelope plus Content.
// Content is embedded so the JSON form is flat.
type Draft struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	Revision      int64     `json:"revision"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Content
}

// Content is the client-supplied part of a draft (create/update/validate input).
type Content struct {
	Title          string          `json:"title"`
	Yield          string          `json:"yield,omitempty"`
	Ingredients    []Ingredient    `json:"ingredients"`
	Instructions   []Step          `json:"instructions"`
	Notes          string          `json:"notes,omitempty"`
	Sources        []Source        `json:"sources"`
	Estimates      *Estimates      `json:"estimates,omitempty"`
	PhaseEstimates *PhaseEstimates `json:"phase_estimates,omitempty"`
	Guidance       []Guidance      `json:"guidance"`
}

// Ingredient is an ingredient as written. Nothing in it is interpreted or normalized.
type Ingredient struct {
	Text     string `json:"text"`
	Quantity string `json:"quantity,omitempty"`
	Unit     string `json:"unit,omitempty"`
	Name     string `json:"name,omitempty"`
	Note     string `json:"note,omitempty"`
}

// Phase is the optional three-part breakdown tag on an instruction step.
type Phase string

const (
	PhasePrepAhead   Phase = "prep_ahead"
	PhasePrepTonight Phase = "prep_tonight"
	PhaseCookTonight Phase = "cook_tonight"
)

// Step is one instruction step. PhaseBasis is required when Phase is PhasePrepAhead and
// rejected when Phase is empty.
type Step struct {
	Text       string `json:"text"`
	Phase      Phase  `json:"phase,omitempty"`
	PhaseBasis *Basis `json:"phase_basis,omitempty"`
}

// BasisKind says where a claim comes from.
type BasisKind string

const (
	// BasisAgentEstimate is the agent's own suggestion, not checked against anything.
	BasisAgentEstimate BasisKind = "agent_estimate"
	// BasisSourced is a claim by the submitter that it comes from a source. It is never verified.
	BasisSourced BasisKind = "sourced"
)

// Basis labels an estimate, a prep-ahead step or a guidance statement. SourceRef, when set, must
// be the ID of a Source in the same draft.
type Basis struct {
	Kind      BasisKind `json:"kind"`
	SourceRef string    `json:"source_ref,omitempty"`
	Note      string    `json:"note,omitempty"`
}

// SourceKind is the kind of a provenance entry.
type SourceKind string

const (
	SourceURL            SourceKind = "url"
	SourcePublication    SourceKind = "publication"
	SourceUserSupplied   SourceKind = "user_supplied"
	SourceAgentGenerated SourceKind = "agent_generated"
)

// Source is a provenance entry. A URL is data: nothing in this system fetches it.
type Source struct {
	ID      string     `json:"id"`
	Kind    SourceKind `json:"kind"`
	Label   string     `json:"label,omitempty"`
	URL     string     `json:"url,omitempty"`
	Locator string     `json:"locator,omitempty"`
}

// Level is an ordinal label (not a measurement) for attention, cleanup and variability.
type Level string

const (
	LevelLow    Level = "low"
	LevelMedium Level = "medium"
	LevelHigh   Level = "high"
)

// MinutesEstimate is a minutes value with its basis.
// Value is a pointer so a missing value is detectable (and reported as
// "required") rather than silently decoding to zero.
type MinutesEstimate struct {
	Value *int   `json:"value"`
	Basis *Basis `json:"basis"`
}

// LevelEstimate is a level value with its basis.
type LevelEstimate struct {
	Value Level  `json:"value"`
	Basis *Basis `json:"basis"`
}

// Estimates holds optional estimates.
// A nil pointer means "unknown". It is never the same as a present zero.
type Estimates struct {
	ActiveMinutes *MinutesEstimate `json:"active_minutes,omitempty"`
	TotalMinutes  *MinutesEstimate `json:"total_minutes,omitempty"`
	Attention     *LevelEstimate   `json:"attention,omitempty"`
	Cleanup       *LevelEstimate   `json:"cleanup,omitempty"`
	Variability   *LevelEstimate   `json:"variability,omitempty"`
}

// PhaseEstimates holds optional estimates per phase.
type PhaseEstimates struct {
	PrepAhead   *Estimates `json:"prep_ahead,omitempty"`
	PrepTonight *Estimates `json:"prep_tonight,omitempty"`
	CookTonight *Estimates `json:"cook_tonight,omitempty"`
}

// GuidanceTopic is the subject of a guidance statement.
type GuidanceTopic string

const (
	TopicPrepAhead  GuidanceTopic = "prep_ahead"
	TopicStorage    GuidanceTopic = "storage"
	TopicFoodSafety GuidanceTopic = "food_safety"
	TopicOther      GuidanceTopic = "other"
)

// Guidance is a storage, prep-ahead or food-safety statement. It always carries a basis and is
// never inferred or validated by the application.
type Guidance struct {
	Topic GuidanceTopic `json:"topic"`
	Text  string        `json:"text"`
	Basis *Basis        `json:"basis"`
}

// Issue is one validation finding. Used for both errors and warnings.
type Issue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// ValidationResult is shared by core, CLI and MCP. Scope is always
// ValidationScope and Notice is always ValidationNotice.
type ValidationResult struct {
	Scope             string  `json:"scope"`
	StructurallyValid bool    `json:"structurally_valid"`  // len(Errors) == 0
	Errors            []Issue `json:"errors"`              // never nil
	Warnings          []Issue `json:"warnings"`            // never nil
	Truncated         bool    `json:"truncated,omitempty"` // true if either list was capped
	Notice            string  `json:"notice"`
}

// Summary is the list view of a draft.
type Summary struct {
	ID                 string    `json:"id"`
	Title              string    `json:"title"`
	Status             string    `json:"status"`
	Revision           int64     `json:"revision"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	IngredientCount    int       `json:"ingredient_count"`
	InstructionCount   int       `json:"instruction_count"`
	SourceCount        int       `json:"source_count"`
	AgentEstimateCount int       `json:"agent_estimate_count"` // bases of kind agent_estimate anywhere in the draft
}

// SkippedEntry describes a stored entry that could not be used.
type SkippedEntry struct {
	ID       string   `json:"id,omitempty"` // present when the file name was a well-formed draft file name
	Category Category `json:"category"`     // corrupt or unsupported_schema or io
	Message  string   `json:"message"`      // never contains paths or recipe text
}

// ListOptions selects a page of drafts.
type ListOptions struct {
	Limit  int // 0 means DefaultListLimit; negative or > MaxListLimit is invalid_limit
	Offset int // >= 0; negative is invalid_offset
}

// ListPage is one page of draft summaries.
type ListPage struct {
	Drafts       []Summary      `json:"drafts"` // never nil
	Total        int            `json:"total"`  // readable drafts in the store (may be below the MaxStoredDrafts count)
	Offset       int            `json:"offset"`
	Limit        int            `json:"limit"`
	HasMore      bool           `json:"has_more"`
	Skipped      []SkippedEntry `json:"skipped"`       // at most MaxSkippedReported entries
	SkippedTotal int            `json:"skipped_total"` // all unreadable entries
}

// WriteResult is the outcome of a successful Create or Update.
type WriteResult struct {
	Draft      Draft            `json:"draft"`
	Validation ValidationResult `json:"validation"` // Errors is always empty here; warnings only
}

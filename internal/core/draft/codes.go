package draft

// Validation issue codes (contract section 5). Errors block a save; warnings never do.
const (
	// Errors.
	IssueMalformedJSON    = "malformed_json"
	IssueInputTooLarge    = "input_too_large"
	IssueUnknownField     = "unknown_field"
	IssueInvalidType      = "invalid_type"
	IssueRequired         = "required"
	IssueInvalidValue     = "invalid_value"
	IssueInvalidChars     = "invalid_characters"
	IssueTooLong          = "too_long"
	IssueTooManyItems     = "too_many_items"
	IssueDraftTooLarge    = "draft_too_large"
	IssueDuplicateID      = "duplicate_id"
	IssueDuplicateKey     = "duplicate_key"
	IssueUnknownSourceRef = "unknown_source_ref"
	IssueBasisRequired    = "basis_required"

	// Warnings.
	WarnNoIngredients           = "no_ingredients"
	WarnNoInstructions          = "no_instructions"
	WarnNoSources               = "no_sources"
	WarnSourcedWithoutReference = "sourced_without_reference"
	WarnActiveExceedsTotal      = "active_exceeds_total"
	WarnPartialPhaseAssignment  = "partial_phase_assignment"
)

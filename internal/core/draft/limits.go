package draft

import "time"

// Limits (contract section 6). Runes are Unicode code points. They are exported so adapters and
// tests share them.
const (
	MaxDraftBytes   = 131072 // compact JSON encoding of Content
	MaxInputBytes   = 262144 // raw bytes a CLI reader takes from a file or stdin
	MaxStoredDrafts = 1000   // every draft-named file in a data root, readable or not

	MaxTitleRunes = 200
	MaxYieldRunes = 100
	MaxNotesRunes = 4000

	MaxIngredients  = 100
	MaxInstructions = 100
	MaxSources      = 20
	MaxGuidance     = 20

	MaxIngredientText  = 300 // ingredients[].text and .note
	MaxIngredientField = 200 // ingredients[].name
	MaxIngredientShort = 50  // ingredients[].quantity and .unit
	MaxStepRunes       = 2000
	MaxGuidanceRunes   = 1000
	MaxLabelRunes      = 200 // sources[].label and .locator
	MaxURLBytes        = 2048
	MaxNoteRunes       = 300 // basis.note
	MaxMinutes         = 10080

	DefaultListLimit   = 25
	MaxListLimit       = 100
	MaxSkippedReported = 20
	MaxIssuesReported  = 50 // errors and warnings each, per validation

	// MaxRevision is the largest revision a draft can have: 2^53-1, the largest integer every
	// JSON client represents exactly. An update from this revision is refused (revision_limit).
	MaxRevision int64 = 1<<53 - 1

	// MaxFutureSkew is how far past the local clock a stored created_at or updated_at may be
	// before the file is treated as corrupt. It tolerates a clock that was briefly ahead (another
	// machine, a synced folder, a manual change) without accepting an obviously wrong date.
	MaxFutureSkew = 24 * time.Hour

	// MaxStoredFileBytes is the store read cap: a larger file is treated as corrupt without being
	// fully read.
	MaxStoredFileBytes = 524288
)

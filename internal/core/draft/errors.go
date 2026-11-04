package draft

// Category is the stable error category shared by core, CLI and MCP.
type Category string

const (
	CategoryInvalidArgument   Category = "invalid_argument"
	CategoryNotFound          Category = "not_found"
	CategoryConflict          Category = "conflict"
	CategoryCorrupt           Category = "corrupt"
	CategoryUnsupportedSchema Category = "unsupported_schema"
	CategoryLimitExceeded     Category = "limit_exceeded"
	CategoryIO                Category = "io"
	CategoryInternal          Category = "internal"
)

// Error codes (contract section 8), grouped by category.
const (
	CodeValidationFailed = "validation_failed"
	CodeInvalidID        = "invalid_id"
	CodeInvalidRevision  = "invalid_revision"
	CodeInvalidLimit     = "invalid_limit"
	CodeInvalidOffset    = "invalid_offset"
	CodeInvalidDataDir   = "invalid_data_dir"
	CodeExactlyOneOf     = "exactly_one_of"

	CodeDraftNotFound = "draft_not_found"

	CodeRevisionMismatch = "revision_mismatch"
	CodeAlreadyExists    = "already_exists"

	CodeInvalidJSON    = "invalid_json"
	CodeInvalidContent = "invalid_content"
	CodeIDMismatch     = "id_mismatch"
	CodeNotRegularFile = "not_regular_file"
	CodeFileTooLarge   = "file_too_large"

	CodeSchemaUnsupported = "schema_unsupported"

	CodeStoreFull = "store_full"

	CodeIOError            = "io_error"
	CodeLockTimeout        = "lock_timeout"
	CodeCanceled           = "canceled" // context cancelled while waiting (for example on the lock)
	CodeDataDirUnavailable = "data_dir_unavailable"

	CodeInternal           = "internal"
	CodeIDGenerationFailed = "id_generation_failed"
	// CodeEncodedTooLarge is returned by a Store (category internal) when the encoded stored file
	// would exceed MaxStoredFileBytes.
	CodeEncodedTooLarge = "encoded_too_large"
)

// Error is the one error type shared by core, CLI and MCP. Every error returned by the Service and
// by any Store is (or wraps, so errors.As works) a *Error.
//
// Message is safe to show to an agent: it must not contain filesystem paths, recipe text, titles
// or source URLs. Err may contain paths and is for stderr logs only.
type Error struct {
	Category        Category `json:"category"`
	Code            string   `json:"code"`
	Message         string   `json:"message"`
	Issues          []Issue  `json:"issues,omitempty"`
	Warnings        []Issue  `json:"warnings,omitempty"`
	CurrentRevision int64    `json:"current_revision,omitempty"`
	Err             error    `json:"-"` // underlying cause, for logs and errors.Is/As; never serialized
}

// Error returns "<category>: <message>".
func (e *Error) Error() string { return string(e.Category) + ": " + e.Message }

// Unwrap returns the underlying cause.
func (e *Error) Unwrap() error { return e.Err }

package localstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

var (
	idRe       = regexp.MustCompile(`^drf_[0-9a-f]{20}$`)
	fileNameRe = regexp.MustCompile(`^(drf_[0-9a-f]{20})\.json$`)
)

// encode renders d as a stored file: indented, struct field order, trailing newline. Nil lists are
// written as [] so a file never contains null for a list.
func encode(d draft.Draft) ([]byte, error) {
	d = normalize(d)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(d); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// normalize replaces nil list fields with empty lists, on a shallow copy.
func normalize(d draft.Draft) draft.Draft {
	if d.Ingredients == nil {
		d.Ingredients = []draft.Ingredient{}
	}
	if d.Instructions == nil {
		d.Instructions = []draft.Step{}
	}
	if d.Sources == nil {
		d.Sources = []draft.Source{}
	}
	if d.Guidance == nil {
		d.Guidance = []draft.Guidance{}
	}
	return d
}

// readDraftFile reads and fully checks one stored draft. It never modifies anything. A missing file
// is not_found; a symlink or non-regular file, an oversize file, bad JSON, an unknown schema, or a
// failed envelope check is corrupt or unsupported_schema.
var errEntryChanged = errors.New("entry changed while opening")

func readDraftFile(dir *os.Root, path, id string) (draft.Draft, error) {
	// Atomic replacement by another legitimate writer can race the metadata check.
	// Retry without reading the new handle until its identity has been verified.
	for i := 0; i < 3; i++ {
		d, err := readDraftFileOnce(dir, path, id)
		if !errors.Is(err, errEntryChanged) {
			return d, err
		}
	}
	return draft.Draft{}, ioError(draft.CodeIOError, "the draft entry kept changing while opening", nil)
}
func readDraftFileOnce(dir *os.Root, path, id string) (draft.Draft, error) {
	fi, err := dir.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return draft.Draft{}, notFound()
		}
		return draft.Draft{}, ioError(draft.CodeIOError, "cannot read the draft file", err)
	}
	if !fi.Mode().IsRegular() {
		return draft.Draft{}, corrupt(draft.CodeNotRegularFile, "the draft entry is not a regular file", nil)
	}
	if fi.Size() > draft.MaxStoredFileBytes {
		return draft.Draft{}, corrupt(draft.CodeFileTooLarge, "the draft file is larger than the allowed size", nil)
	}
	f, err := dir.OpenFile(path, os.O_RDONLY|openNoFollow|openNonblock, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return draft.Draft{}, notFound()
		}
		if fi2, lerr := dir.Lstat(path); lerr == nil && !fi2.Mode().IsRegular() {
			return draft.Draft{}, corrupt(draft.CodeNotRegularFile, "the draft entry is not a regular file", nil)
		}
		return draft.Draft{}, ioError(draft.CodeIOError, "cannot read the draft file", err)
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !actual.Mode().IsRegular() {
		return draft.Draft{}, corrupt(draft.CodeNotRegularFile, "the draft entry changed while opening", err)
	}
	if !os.SameFile(fi, actual) {
		return draft.Draft{}, errEntryChanged
	}
	raw, err := io.ReadAll(io.LimitReader(f, draft.MaxStoredFileBytes+1))
	if err != nil {
		return draft.Draft{}, ioError(draft.CodeIOError, "cannot read the draft file", err)
	}
	if len(raw) > draft.MaxStoredFileBytes {
		return draft.Draft{}, corrupt(draft.CodeFileTooLarge, "the draft file is larger than the allowed size", nil)
	}
	return decode(raw, id)
}

// decode reads schema_version first and only then decodes the rest strictly, so a file written by
// a newer schema is reported as unsupported rather than corrupt.
func decode(raw []byte, id string) (draft.Draft, error) {
	if !utf8.Valid(raw) || !json.Valid(raw) {
		return draft.Draft{}, corrupt(draft.CodeInvalidJSON, "the draft file is not valid JSON", nil)
	}
	version, ok := draft.StoredSchemaVersion(raw)
	if !ok {
		return draft.Draft{}, corrupt(draft.CodeInvalidContent, "the draft file has an invalid schema header", nil)
	}
	ver, err := strconv.Atoi(string(version))
	if err != nil {
		// The header already verified the integer spelling. Only a version
		// outside the representable range can fail here.
		return draft.Draft{}, newError(draft.CategoryUnsupportedSchema, draft.CodeSchemaUnsupported,
			"the draft was written by an unsupported schema version", nil)
	}
	if !slices.Contains(draft.SupportedSchemaVersions, ver) {
		return draft.Draft{}, newError(draft.CategoryUnsupportedSchema, draft.CodeSchemaUnsupported,
			fmt.Sprintf("draft was written by schema version %d; this build reads %v", ver, draft.SupportedSchemaVersions), nil)
	}
	if !draft.ValidStoredDraftShape(raw) {
		return draft.Draft{}, corrupt(draft.CodeInvalidContent, "the draft file does not match the schema", nil)
	}
	var d draft.Draft
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return draft.Draft{}, corrupt(draft.CodeInvalidContent, "the draft file does not match the schema", err)
	}
	if d.ID != id {
		return draft.Draft{}, corrupt(draft.CodeIDMismatch, "the draft id does not match its file name", nil)
	}
	if d.Status != draft.StatusDraft {
		return draft.Draft{}, corrupt(draft.CodeInvalidContent, "the draft file has an invalid status", nil)
	}
	if !validStoredEnvelope(raw, d) {
		return draft.Draft{}, corrupt(draft.CodeInvalidContent, "the draft file has an invalid revision or timestamp", nil)
	}
	if !draft.Validate(d.Content).StructurallyValid {
		return draft.Draft{}, corrupt(draft.CodeInvalidContent, "the draft file has invalid content", nil)
	}
	return normalize(d), nil
}

// validStoredEnvelope checks the revision and timestamp fields of a decoded stored draft, including
// the exact spelling of both timestamps in the raw file.
func validStoredEnvelope(raw []byte, d draft.Draft) bool {
	var env struct {
		CreatedAt json.RawMessage `json:"created_at"`
		UpdatedAt json.RawMessage `json:"updated_at"`
	}
	if json.Unmarshal(raw, &env) != nil {
		return false
	}
	if _, ok := parseStoredTime(env.CreatedAt); !ok {
		return false
	}
	if _, ok := parseStoredTime(env.UpdatedAt); !ok {
		return false
	}
	return validEnvelope(d, time.Now())
}

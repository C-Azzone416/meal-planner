package mcpadapter

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

// outcome is what a tool body produces: a success value or a core error, plus the draft ID to log
// when there is one (IDs are random and carry no content).
type outcome struct {
	value any
	err   error
	id    string
}

// call runs body, logs one line (tool, outcome, duration, draft id) and maps the outcome to a tool
// result. Domain errors become isError results; the handler itself never returns a protocol error.
func (h *handlers) call(ctx context.Context, tool string, body func() outcome) *mcp.CallToolResult {
	start := time.Now()
	o := body()
	var res *mcp.CallToolResult
	status := "ok"
	if o.err != nil {
		e := asDraftError(o.err)
		status = string(e.Category) + "/" + e.Code
		res = result(map[string]any{"error": e}, true)
	} else {
		res = result(o.value, false)
	}
	attrs := []any{"tool", tool, "outcome", status, "duration_ms", time.Since(start).Milliseconds()}
	if draft.ValidID(o.id) { // never log caller-supplied text
		attrs = append(attrs, "id", o.id)
	}
	h.cfg.logger.LogAttrs(ctx, 0, "tool call", slogAttrs(attrs)...)
	return res
}

// result returns v as structuredContent and, identically, as compact JSON in one text block.
func result(v any, isError bool) *mcp.CallToolResult {
	b, err := json.Marshal(v)
	if err != nil { // cannot happen for the types used here
		b = []byte(`{"error":{"category":"internal","code":"internal","message":"unexpected internal error"}}`)
		isError = true
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(b)}},
		StructuredContent: json.RawMessage(b),
		IsError:           isError,
	}
}

// asDraftError returns the *draft.Error in err's chain, or an internal error. Only the wire fields
// of the result are serialized, so Err (which may hold paths) never leaves the process.
func asDraftError(err error) *draft.Error {
	var e *draft.Error
	if errors.As(err, &e) {
		return e
	}
	return &draft.Error{Category: draft.CategoryInternal, Code: draft.CodeInternal, Message: "unexpected internal error"}
}

// argumentsError is the tool error for arguments that do not match the input schema (a missing
// required property, a wrong JSON type, an unknown property). Like the core errors it carries no
// input text. The code is specific to the adapter.
func argumentsError() *draft.Error {
	return &draft.Error{
		Category: draft.CategoryInvalidArgument,
		Code:     CodeInvalidArguments,
		Message:  "the arguments do not match the tool's input schema: send every required property, with the documented JSON types, and no other properties",
	}
}

// decodeError is the validation_failed error for a candidate that DecodeContent rejected, shaped
// like the one the core returns for content that fails Validate.
func decodeError(issues []draft.Issue) *draft.Error {
	return &draft.Error{
		Category: draft.CategoryInvalidArgument,
		Code:     draft.CodeValidationFailed,
		Message:  "the draft failed structural validation; see issues",
		Issues:   issues,
	}
}

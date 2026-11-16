package mcpadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
	"github.com/C-Azzone416/meal-planner/internal/core/version"
	"github.com/C-Azzone416/meal-planner/internal/localclient"
)

type handlers struct {
	svc *draft.Service
	cfg *config
}

func slogAttrs(kv []any) []slog.Attr {
	out := make([]slog.Attr, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, slog.Any(kv[i].(string), kv[i+1]))
	}
	return out
}

// Argument structs. `draft` stays raw JSON and goes unmodified to DecodeContent, so MCP, CLI and
// core give identical results. Missing optional values arrive as zero values and the core reports
// them (section 13).
type (
	createArgs struct {
		Draft json.RawMessage `json:"draft"`
	}
	getArgs struct {
		ID string `json:"id"`
	}
	listArgs struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}
	updateArgs struct {
		ID               string          `json:"id"`
		ExpectedRevision int64           `json:"expected_revision"`
		Draft            json.RawMessage `json:"draft"`
	}
	statusArgs struct{}
	sendArgs   struct {
		ID               string `json:"id"`
		ExpectedRevision int64  `json:"expected_revision"`
	}
)

const (
	basisText = "Mark every estimate and every prep-ahead, storage or safety statement with a basis: agent_estimate for your own suggestion, sourced (with source_ref when possible) only when it comes from a listed source."
	draftText = "Saved recipes are drafts. Validation is structural only and is not food-safety verification or review. Recipe text and URLs are stored as data and are never fetched or executed."
	idDesc    = "Draft ID: drf_ followed by 20 lowercase hexadecimal characters."
	draftDesc = "Complete recipe content as a JSON object (title is the only required field). Do not include server-assigned fields (schema_version, id, revision, status, created_at, updated_at). Field names are case-sensitive and duplicate keys are rejected. " + basisText
)

// obj builds a root input schema: object, no additional properties, typed properties, required.
func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "additionalProperties": false, "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func prop(typ, desc string) map[string]any { return map[string]any{"type": typ, "description": desc} }

func boolp(b bool) *bool { return &b }

func annotations(readOnly, destructive bool) *mcp.ToolAnnotations {
	a := &mcp.ToolAnnotations{ReadOnlyHint: readOnly, OpenWorldHint: boolp(false)}
	if !readOnly {
		a.DestructiveHint = boolp(destructive)
	}
	return a
}

func sendAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolp(false), OpenWorldHint: boolp(true)}
}

// addTool registers one tool on the low-level SDK API. The generic mcp.AddTool is not used because
// it decodes the arguments into a map and re-encodes them before the handler runs, which would
// silently drop duplicate keys inside `draft` and break the strict-decode rule (section 3). Here
// the handler gets the raw argument bytes, validates them against the tool's input schema with the
// same JSON Schema library the SDK uses, and decodes only the typed properties; `draft` stays
// json.RawMessage.
func addTool[A any](h *handlers, s *mcp.Server, tool *mcp.Tool, fn func(ctx context.Context, a A) outcome) {
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		panic(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		panic(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		panic(err)
	}
	s.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return h.call(ctx, tool.Name, func() outcome {
			args := req.Params.Arguments
			if len(args) == 0 || string(args) == "null" {
				args = json.RawMessage("{}")
			}
			if duplicateRootKey(args) {
				return outcome{err: &draft.Error{Category: draft.CategoryInvalidArgument, Code: "duplicate_key",
					Message: "the arguments contain a duplicate key; send each property once"}}
			}
			var instance any
			if err := json.Unmarshal(args, &instance); err != nil || resolved.Validate(instance) != nil {
				return outcome{err: argumentsError()}
			}
			var a A
			if err := json.Unmarshal(args, &a); err != nil {
				return outcome{err: argumentsError()}
			}
			return fn(ctx, a)
		}), nil
	})
}

func (h *handlers) register(s *mcp.Server) {
	addTool(h, s, &mcp.Tool{
		Name:        ToolDraftCreate,
		Description: draftText + " Each call creates a new draft; repeating a call creates another. If a call fails ambiguously, use draft_list before retrying.",
		InputSchema: obj(map[string]any{"draft": prop("object", draftDesc)}, "draft"),
		Annotations: annotations(false, false),
	}, h.create)
	addTool(h, s, &mcp.Tool{
		Name:        ToolDraftGet,
		Description: "Read one saved draft by ID. Read-only.",
		InputSchema: obj(map[string]any{"id": prop("string", idDesc)}, "id"),
		Annotations: annotations(true, false),
	}, h.get)
	addTool(h, s, &mcp.Tool{
		Name:        ToolDraftList,
		Description: "List saved drafts, newest update first, with paging. Unreadable files are reported in skipped, not as errors. Read-only.",
		InputSchema: obj(map[string]any{
			"limit":  prop("integer", "Page size, 1 to 100 (default 25)."),
			"offset": prop("integer", "Number of drafts to skip, 0 or more (default 0)."),
		}),
		Annotations: annotations(true, false),
	}, h.list)
	addTool(h, s, &mcp.Tool{
		Name:        ToolDraftUpdate,
		Description: draftText + " Replaces the whole draft; send the complete content and the revision you last read.",
		InputSchema: obj(map[string]any{
			"id":                prop("string", idDesc),
			"expected_revision": prop("integer", "The revision you last read, 1 to 9007199254740991. A stale value is a revision_mismatch conflict and nothing is written."),
			"draft":             prop("object", draftDesc),
		}, "id", "expected_revision", "draft"),
		Annotations: annotations(false, true),
	}, h.update)
	addTool(h, s, &mcp.Tool{
		Name:        ToolDraftValidate,
		Description: draftText + " Checks a candidate without saving it; an invalid candidate is a normal result with structurally_valid false.",
		InputSchema: obj(map[string]any{"draft": prop("object", draftDesc)}, "draft"),
		Annotations: annotations(true, false),
	}, h.validate)
	addTool(h, s, &mcp.Tool{
		Name:        ToolServerStatus,
		Description: "Report binary and schema versions, local-only mode, supported operations, limits and store status. Never returns recipe content or the data directory path. Read-only.",
		InputSchema: obj(map[string]any{}),
		Annotations: annotations(true, false),
	}, h.status)
	if h.cfg.sender != nil {
		addTool(h, s, &mcp.Tool{
			Name:        ToolDraftSend,
			Description: "Explicitly propose one immutable local draft revision to the pinned Preview account. The signed-in owner must independently approve this exact snapshot in the browser before it is saved. Repeating the same ID and revision safely polls or resumes the same operation. No credential is accepted or returned.",
			InputSchema: obj(map[string]any{
				"id":                prop("string", idDesc),
				"expected_revision": prop("integer", "The exact local revision to propose. If the draft changed, nothing is sent."),
			}, "id", "expected_revision"),
			Annotations: sendAnnotations(),
		}, h.send)
	}
}

func (h *handlers) send(ctx context.Context, in sendArgs) outcome {
	if !draft.ValidID(in.ID) || in.ExpectedRevision < 1 || in.ExpectedRevision > draft.MaxRevision {
		return outcome{err: argumentsError(), id: in.ID}
	}
	local, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		return outcome{err: err, id: in.ID}
	}
	if local.Revision != in.ExpectedRevision {
		return outcome{err: &draft.Error{Category: draft.CategoryConflict, Code: draft.CodeRevisionMismatch,
			Message: "the draft changed since the revision selected for sending; read it again before sending", CurrentRevision: local.Revision}, id: in.ID}
	}
	result, err := h.cfg.sender.Send(ctx, local)
	if err != nil {
		return outcome{err: sendError(err), id: in.ID}
	}
	return outcome{value: map[string]any{"send": result}, id: in.ID}
}

func sendError(err error) error {
	switch {
	case errors.Is(err, localclient.ErrDenied):
		return &draft.Error{Category: draft.CategoryConflict, Code: "send_denied", Message: "the owner denied this draft send"}
	case errors.Is(err, localclient.ErrExpired):
		return &draft.Error{Category: draft.CategoryConflict, Code: "send_expired", Message: "the owner approval window expired; start a new send"}
	case errors.Is(err, localclient.ErrCredentialMissing):
		return &draft.Error{Category: draft.CategoryIO, Code: "send_not_connected", Message: "Preview sending is not connected for this account"}
	case errors.Is(err, context.Canceled):
		return &draft.Error{Category: draft.CategoryIO, Code: draft.CodeCanceled, Message: "the send was cancelled before completion"}
	default:
		return &draft.Error{Category: draft.CategoryIO, Code: "send_unavailable", Message: "Preview did not confirm this send; retry the same draft revision"}
	}
}

func (h *handlers) create(ctx context.Context, in createArgs) outcome {
	c, issues := draft.DecodeContent(in.Draft)
	if len(issues) > 0 {
		return outcome{err: decodeError(issues)}
	}
	res, err := h.svc.Create(ctx, c)
	if err != nil {
		return outcome{err: err}
	}
	return outcome{value: res, id: res.Draft.ID}
}

func (h *handlers) get(ctx context.Context, in getArgs) outcome {
	d, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		return outcome{err: err, id: in.ID}
	}
	return outcome{value: map[string]any{"draft": d}, id: d.ID}
}

func (h *handlers) list(ctx context.Context, in listArgs) outcome {
	page, err := h.svc.List(ctx, draft.ListOptions{Limit: in.Limit, Offset: in.Offset})
	return outcome{value: page, err: err}
}

func (h *handlers) update(ctx context.Context, in updateArgs) outcome {
	c, issues := draft.DecodeContent(in.Draft)
	if len(issues) > 0 {
		// The core reports a bad ID or revision before it looks at content; keep that order.
		if !draft.ValidID(in.ID) || in.ExpectedRevision < 1 || in.ExpectedRevision > draft.MaxRevision {
			_, err := h.svc.Update(ctx, in.ID, in.ExpectedRevision, draft.Content{})
			return outcome{err: err, id: in.ID}
		}
		return outcome{err: decodeError(issues), id: in.ID}
	}
	res, err := h.svc.Update(ctx, in.ID, in.ExpectedRevision, c)
	if err != nil {
		return outcome{err: err, id: in.ID}
	}
	return outcome{value: res, id: in.ID}
}

func (h *handlers) validate(_ context.Context, in createArgs) outcome {
	return outcome{value: map[string]any{"validation": draft.ValidateRaw(in.Draft)}}
}

type storeStatus struct {
	Available        bool         `json:"available"`
	DraftCount       int          `json:"draft_count"`
	SkippedCount     int          `json:"skipped_count"`
	CrossProcessLock bool         `json:"cross_process_lock"`
	Error            *statusError `json:"error,omitempty"`
}

type statusError struct {
	Category draft.Category `json:"category"`
	Message  string         `json:"message"`
}

func (h *handlers) status(ctx context.Context, _ statusArgs) outcome {
	{
		st := storeStatus{}
		exists, private := false, false
		info, err := h.svc.Info(ctx)
		if err != nil {
			e := asDraftError(err)
			st.Error = &statusError{Category: e.Category, Message: e.Message}
		} else {
			st = storeStatus{Available: true, DraftCount: info.DraftCount, SkippedCount: info.SkippedCount, CrossProcessLock: info.CrossProcessLock}
			exists, private = info.DataDirExists, info.PrivatePermissions
		}
		operations := append([]string(nil), Operations...)
		if h.cfg.sender != nil {
			operations = append(operations, ToolDraftSend)
		}
		return outcome{value: map[string]any{
			"binary":                    ServerName,
			"binary_version":            version.Version,
			"schema_version":            draft.SchemaVersion,
			"supported_schema_versions": draft.SupportedSchemaVersions,
			"mode":                      "local",
			"local_only":                true,
			"operations":                operations,
			"data_dir":                  map[string]any{"source": h.cfg.source, "exists": exists, "private_permissions": private},
			"store":                     st,
			"limits": map[string]any{
				"max_draft_bytes":    draft.MaxDraftBytes,
				"default_list_limit": draft.DefaultListLimit,
				"max_list_limit":     draft.MaxListLimit,
				"max_stored_drafts":  draft.MaxStoredDrafts,
			},
		}}
	}
}

// duplicateRootKey reports whether the top-level object in args repeats a key. encoding/json keeps
// the last value silently, so this reads the keys as tokens. Nested duplicates inside `draft` are
// found later by DecodeContent. Invalid JSON returns false; schema validation rejects it.
func duplicateRootKey(args []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(args))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return false
	}
	seen := map[string]bool{}
	for dec.More() {
		k, err := dec.Token()
		key, ok := k.(string)
		if err != nil || !ok {
			return false
		}
		if seen[key] {
			return true
		}
		seen[key] = true
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return false
		}
	}
	return false
}

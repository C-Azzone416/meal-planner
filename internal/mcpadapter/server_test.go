package mcpadapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
	"github.com/C-Azzone416/meal-planner/internal/core/draft/memstore"
	"github.com/C-Azzone416/meal-planner/internal/mcpadapter"
)

const secretTitle = "Synthetic Secret Title 7f3a"

type env struct {
	t     *testing.T
	cs    *mcp.ClientSession
	store *memstore.Store
	logs  *bytes.Buffer
}

func setupStore(t *testing.T, store draft.Store, opts ...mcpadapter.Option) *env {
	t.Helper()
	logs := &bytes.Buffer{}
	svc := draft.NewService(store)
	opts = append([]mcpadapter.Option{mcpadapter.WithLogger(slog.New(slog.NewTextHandler(logs, nil)))}, opts...)
	srv := mcpadapter.New(svc, opts...)
	ct, st := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	ms, _ := store.(*memstore.Store)
	return &env{t: t, cs: cs, store: ms, logs: logs}
}

func setup(t *testing.T, opts ...mcpadapter.Option) *env {
	return setupStore(t, memstore.New(), opts...)
}

// call returns the result; err is a protocol-level error.
func (e *env) call(name string, args any) (*mcp.CallToolResult, error) {
	e.t.Helper()
	return e.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
}

// ok calls a tool that must succeed and returns its structured content.
func (e *env) ok(name string, args any) map[string]any {
	e.t.Helper()
	res, err := e.call(name, args)
	if err != nil || res.IsError {
		e.t.Fatalf("%s: err=%v result=%s", name, err, text(res))
	}
	return structured(e.t, res)
}

// fail calls a tool that must return a domain error and returns the error object.
func (e *env) fail(name string, args any) map[string]any {
	e.t.Helper()
	res, err := e.call(name, args)
	if err != nil {
		e.t.Fatalf("%s: protocol error %v, want isError result", name, err)
	}
	if !res.IsError {
		e.t.Fatalf("%s: want isError, got %s", name, text(res))
	}
	m := structured(e.t, res)
	em, ok := m["error"].(map[string]any)
	if !ok {
		e.t.Fatalf("%s: result has no error object: %s", name, text(res))
	}
	return em
}

func text(res *mcp.CallToolResult) string {
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(*mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

// structured checks structuredContent and the text block carry identical JSON and returns it.
func structured(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	if len(res.Content) != 1 {
		t.Fatalf("want exactly one content block, got %d", len(res.Content))
	}
	sb, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if err := json.Unmarshal(sb, &a); err != nil {
		t.Fatalf("structuredContent: %v", err)
	}
	if err := json.Unmarshal([]byte(text(res)), &b); err != nil {
		t.Fatalf("text block is not JSON: %v", err)
	}
	if !jsonEqual(a, b) {
		t.Fatalf("structuredContent and text differ:\n%s\n%s", sb, text(res))
	}
	if strings.Contains(text(res), "\n") {
		t.Fatalf("text block is not compact: %q", text(res))
	}
	m, _ := a.(map[string]any)
	return m
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func (e *env) listTotal() int {
	e.t.Helper()
	return int(e.ok("draft_list", map[string]any{})["total"].(float64))
}

func (e *env) create(title string) string {
	e.t.Helper()
	r := e.ok("draft_create", map[string]any{"draft": map[string]any{"title": title}})
	return r["draft"].(map[string]any)["id"].(string)
}

func TestToolsAndCapabilities(t *testing.T) {
	e := setup(t)
	caps := e.cs.InitializeResult().Capabilities
	if caps.Tools == nil || caps.Prompts != nil || caps.Resources != nil || caps.Logging != nil || caps.Completions != nil {
		t.Fatalf("capabilities = %+v, want tools only", caps)
	}
	if e.cs.InitializeResult().ServerInfo.Name != "mealplanner-mcp" {
		t.Fatalf("server name = %q", e.cs.InitializeResult().ServerInfo.Name)
	}
	lt, err := e.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		readOnly, destructive bool
		required              []string
	}
	wants := map[string]want{
		"draft_create":   {false, false, []string{"draft"}},
		"draft_get":      {true, false, []string{"id"}},
		"draft_list":     {true, false, nil},
		"draft_update":   {false, true, []string{"id", "expected_revision", "draft"}},
		"draft_validate": {true, false, []string{"draft"}},
		"server_status":  {true, false, nil},
	}
	if len(lt.Tools) != len(wants) {
		t.Fatalf("got %d tools, want %d", len(lt.Tools), len(wants))
	}
	for _, tool := range lt.Tools {
		w, ok := wants[tool.Name]
		if !ok {
			t.Errorf("unexpected tool %q", tool.Name)
			continue
		}
		a := tool.Annotations
		if a == nil || a.ReadOnlyHint != w.readOnly || a.OpenWorldHint == nil || *a.OpenWorldHint || a.IdempotentHint {
			t.Errorf("%s: annotations %+v", tool.Name, a)
		}
		if !w.readOnly && (a.DestructiveHint == nil || *a.DestructiveHint != w.destructive) {
			t.Errorf("%s: destructive hint %+v", tool.Name, a.DestructiveHint)
		}
		if tool.OutputSchema != nil {
			t.Errorf("%s: outputSchema must not be declared", tool.Name)
		}
		raw, _ := json.Marshal(tool.InputSchema)
		var schema map[string]any
		_ = json.Unmarshal(raw, &schema)
		if schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Errorf("%s: root schema = %s", tool.Name, raw)
		}
		for _, kw := range []string{"minimum", "maximum", "pattern", "enum", "oneOf", "anyOf", "allOf", "minLength", "maxLength", "minItems", "maxItems", "default"} {
			if strings.Contains(string(raw), `"`+kw+`"`) {
				t.Errorf("%s: schema uses forbidden keyword %q: %s", tool.Name, kw, raw)
			}
		}
		var req []string
		if r, ok := schema["required"].([]any); ok {
			for _, v := range r {
				req = append(req, v.(string))
			}
		}
		if strings.Join(req, ",") != strings.Join(w.required, ",") {
			t.Errorf("%s: required = %v, want %v", tool.Name, req, w.required)
		}
		props, _ := schema["properties"].(map[string]any)
		for name, p := range props {
			if p.(map[string]any)["description"] == nil {
				t.Errorf("%s.%s: missing description", tool.Name, name)
			}
		}
	}
	for _, name := range []string{"draft_create", "draft_update", "draft_validate"} {
		for _, tool := range lt.Tools {
			if tool.Name == name && !strings.Contains(tool.Description, "Validation is structural only and is not food-safety verification or review") {
				t.Errorf("%s: description lacks the structural-only notice", name)
			}
		}
	}
}

func TestCreateGetListUpdateValidateStatus(t *testing.T) {
	e := setup(t, mcpadapter.WithDataDirSource("flag"))
	created := e.ok("draft_create", map[string]any{"draft": map[string]any{"title": secretTitle}})
	d := created["draft"].(map[string]any)
	id := d["id"].(string)
	if d["revision"].(float64) != 1 || d["status"] != "draft" {
		t.Fatalf("created = %v", d)
	}
	v := created["validation"].(map[string]any)
	if v["structurally_valid"] != true || len(v["errors"].([]any)) != 0 || len(v["warnings"].([]any)) != 3 {
		t.Fatalf("validation = %v", v)
	}

	got := e.ok("draft_get", map[string]any{"id": id})["draft"].(map[string]any)
	if got["title"] != secretTitle {
		t.Fatalf("get = %v", got)
	}
	// Repeating create makes another draft.
	e.create(secretTitle)
	page := e.ok("draft_list", map[string]any{"limit": 1})
	if page["total"].(float64) != 2 || page["limit"].(float64) != 1 || page["has_more"] != true || len(page["drafts"].([]any)) != 1 {
		t.Fatalf("list = %v", page)
	}

	upd := e.ok("draft_update", map[string]any{"id": id, "expected_revision": 1, "draft": map[string]any{
		"title": "Renamed", "ingredients": []any{map[string]any{"text": "2 slices bread"}}}})
	if upd["draft"].(map[string]any)["revision"].(float64) != 2 {
		t.Fatalf("update = %v", upd)
	}

	// A stale update is a conflict tool error with current_revision, and writes nothing.
	ce := e.fail("draft_update", map[string]any{"id": id, "expected_revision": 1, "draft": map[string]any{"title": "Stale"}})
	if ce["category"] != "conflict" || ce["code"] != "revision_mismatch" || ce["current_revision"].(float64) != 2 {
		t.Fatalf("conflict = %v", ce)
	}
	if got := e.ok("draft_get", map[string]any{"id": id})["draft"].(map[string]any); got["title"] != "Renamed" || got["revision"].(float64) != 2 {
		t.Fatalf("after conflict = %v", got)
	}

	val := e.ok("draft_validate", map[string]any{"draft": map[string]any{"title": "Candidate"}})["validation"].(map[string]any)
	if val["structurally_valid"] != true || val["scope"] != "structural" {
		t.Fatalf("validate = %v", val)
	}
	if e.listTotal() != 2 {
		t.Fatal("validate must not store anything")
	}

	st := e.ok("server_status", map[string]any{})
	if st["binary"] != "mealplanner-mcp" || st["mode"] != "local" || st["local_only"] != true || st["schema_version"].(float64) != 1 {
		t.Fatalf("status = %v", st)
	}
	if dd := st["data_dir"].(map[string]any); dd["source"] != "flag" {
		t.Fatalf("data_dir = %v", dd)
	}
	if ops := st["operations"].([]any); len(ops) != 6 {
		t.Fatalf("operations = %v", ops)
	}
	if s := st["store"].(map[string]any); s["available"] != true || s["draft_count"].(float64) != 2 {
		t.Fatalf("store = %v", s)
	}
	if strings.Contains(text(mustCall(t, e, "server_status")), secretTitle) {
		t.Fatal("status leaked a title")
	}
}

func mustCall(t *testing.T, e *env, name string) *mcp.CallToolResult {
	t.Helper()
	res, err := e.call(name, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestDomainErrorsAreToolErrorsAndDoNotMutate(t *testing.T) {
	e := setup(t)
	id := e.create("Keep me")
	cases := []struct {
		name, tool string
		args       map[string]any
		category   string
		code       string
	}{
		{"get bad id", "draft_get", map[string]any{"id": "nope"}, "invalid_argument", "invalid_id"},
		{"get empty id", "draft_get", map[string]any{"id": ""}, "invalid_argument", "invalid_id"},
		{"get unknown", "draft_get", map[string]any{"id": "drf_00000000000000000000"}, "not_found", "draft_not_found"},
		{"list limit too big", "draft_list", map[string]any{"limit": 101}, "invalid_argument", "invalid_limit"},
		{"list negative limit", "draft_list", map[string]any{"limit": -1}, "invalid_argument", "invalid_limit"},
		{"list negative offset", "draft_list", map[string]any{"offset": -1}, "invalid_argument", "invalid_offset"},
		{"update bad id", "draft_update", map[string]any{"id": "x", "expected_revision": 1, "draft": map[string]any{"title": "T"}}, "invalid_argument", "invalid_id"},
		{"update zero revision", "draft_update", map[string]any{"id": id, "expected_revision": 0, "draft": map[string]any{"title": "T"}}, "invalid_argument", "invalid_revision"},
		{"update above-cap revision with invalid content", "draft_update", map[string]any{"id": id, "expected_revision": 9007199254740992, "draft": map[string]any{"title": "X", "unknown": 1}}, "invalid_argument", "invalid_revision"},
		{"update unknown", "draft_update", map[string]any{"id": "drf_00000000000000000000", "expected_revision": 1, "draft": map[string]any{"title": "T"}}, "not_found", "draft_not_found"},
		{"update invalid content", "draft_update", map[string]any{"id": id, "expected_revision": 1, "draft": map[string]any{"title": " "}}, "invalid_argument", "validation_failed"},
		{"create blank title", "draft_create", map[string]any{"draft": map[string]any{"title": " "}}, "invalid_argument", "validation_failed"},
		{"create unknown field", "draft_create", map[string]any{"draft": map[string]any{"title": "T", "status": "reviewed"}}, "invalid_argument", "validation_failed"},
		{"create exact case", "draft_create", map[string]any{"draft": map[string]any{"Title": "T"}}, "invalid_argument", "validation_failed"},
	}
	for _, c := range cases {
		er := e.fail(c.tool, c.args)
		if er["category"] != c.category || er["code"] != c.code {
			t.Errorf("%s: error = %v", c.name, er)
		}
		if msg, _ := er["message"].(string); msg == "" {
			t.Errorf("%s: empty message", c.name)
		}
	}
	// Decode issues come back in issues[].
	er := e.fail("draft_create", map[string]any{"draft": map[string]any{"title": "T", "status": "reviewed"}})
	if is := er["issues"].([]any); len(is) != 1 || is[0].(map[string]any)["code"] != "unknown_field" {
		t.Fatalf("issues = %v", er["issues"])
	}
	if e.listTotal() != 1 {
		t.Fatal("a failed call created a draft")
	}
	if got := e.ok("draft_get", map[string]any{"id": id})["draft"].(map[string]any); got["title"] != "Keep me" || got["revision"].(float64) != 1 {
		t.Fatalf("a failed call changed the draft: %v", got)
	}
}

func TestDraftArgumentIsPassedRawToTheStrictDecoder(t *testing.T) {
	e := setup(t)
	// A duplicate key would be lost by decoding into a map or struct first; it must reach DecodeContent.
	res, err := e.call("draft_create", json.RawMessage(`{"draft":{"title":"A","title":"B"}}`))
	if err != nil || !res.IsError {
		t.Fatalf("err=%v res=%s", err, text(res))
	}
	er := structured(t, res)["error"].(map[string]any)
	if is := er["issues"].([]any); er["code"] != "validation_failed" || is[0].(map[string]any)["code"] != "duplicate_key" {
		t.Fatalf("error = %v", er)
	}
	if e.listTotal() != 0 {
		t.Fatal("a draft was stored")
	}
}

func TestValidateInvalidCandidateIsASuccessfulResult(t *testing.T) {
	e := setup(t)
	for name, raw := range map[string]string{
		"semantic":  `{"draft":{"title":"  ","instructions":[{"text":"x","phase":"prep_ahead"}]}}`,
		"unknown":   `{"draft":{"title":"T","bogus":1}}`,
		"duplicate": `{"draft":{"title":"A","title":"B"}}`,
		"wrongtype": `{"draft":{"title":5}}`,
	} {
		res, err := e.call("draft_validate", json.RawMessage(raw))
		if err != nil || res.IsError {
			t.Fatalf("%s: err=%v res=%s", name, err, text(res))
		}
		v := structured(t, res)["validation"].(map[string]any)
		if v["structurally_valid"] != false || len(v["errors"].([]any)) == 0 || v["warnings"] == nil || v["notice"] == "" {
			t.Errorf("%s: validation = %v", name, v)
		}
	}
	if e.listTotal() != 0 {
		t.Fatal("validate stored something")
	}
}

func TestSchemaRejectionsStoreNothing(t *testing.T) {
	e := setup(t)
	id := e.create("Keep me")
	// Arguments that fail the input schema (missing required property, wrong JSON type, unknown
	// root property) are an isError result with the adapter's invalid_arguments code, produced
	// before any handler logic runs. The low-level SDK API does not validate for us, so the adapter
	// does (see addTool).
	for name, c := range map[string]struct {
		tool string
		args string
	}{
		"missing required":  {"draft_get", `{}`},
		"wrong type":        {"draft_get", `{"id":5}`},
		"unknown root prop": {"draft_get", `{"id":"` + id + `","path":"/etc/passwd"}`},
		"draft not object":  {"draft_create", `{"draft":"title"}`},
		"draft null":        {"draft_validate", `{"draft":null}`},
		"update missing":    {"draft_update", `{"id":"` + id + `","draft":{"title":"T"}}`},
		"float revision":    {"draft_update", `{"id":"` + id + `","expected_revision":1.5,"draft":{"title":"T"}}`},
		"list wrong type":   {"draft_list", `{"limit":"ten"}`},
		"status extra":      {"server_status", `{"verbose":true}`},
	} {
		res, err := e.call(c.tool, json.RawMessage(c.args))
		if err != nil || !res.IsError {
			t.Errorf("%s: err=%v, want an isError result: %s", name, err, text(res))
			continue
		}
		if er := structured(t, res)["error"].(map[string]any); er["code"] != "invalid_arguments" || er["category"] != "invalid_argument" {
			t.Errorf("%s: error = %v", name, er)
		}
	}
	if e.listTotal() != 1 {
		t.Fatal("store changed")
	}
	if got := e.ok("draft_get", map[string]any{"id": id})["draft"].(map[string]any); got["revision"].(float64) != 1 || got["title"] != "Keep me" {
		t.Fatalf("draft changed: %v", got)
	}
}

func TestUnknownToolIsAProtocolError(t *testing.T) {
	e := setup(t)
	res, err := e.call("draft_delete", map[string]any{"id": "drf_00000000000000000000"})
	if err == nil {
		t.Fatalf("unknown tool succeeded: %v", res)
	}
}

type brokenInfoStore struct{ *memstore.Store }

func (brokenInfoStore) Info(context.Context) (draft.StoreInfo, error) {
	return draft.StoreInfo{}, &draft.Error{Category: draft.CategoryIO, Code: draft.CodeDataDirUnavailable,
		Message: "the data directory is unavailable", Err: context.DeadlineExceeded}
}

func TestStatusDoesNotFailWhenTheStoreDoes(t *testing.T) {
	e := setupStore(t, brokenInfoStore{memstore.New()})
	st := e.ok("server_status", map[string]any{})
	s := st["store"].(map[string]any)
	er, _ := s["error"].(map[string]any)
	if s["available"] != false || er["category"] != "io" || er["message"] == "" {
		t.Fatalf("store = %v", s)
	}
	if st["data_dir"].(map[string]any)["source"] != "unknown" {
		t.Fatalf("data_dir = %v", st["data_dir"])
	}
}

func TestLogsCarryNoPayload(t *testing.T) {
	e := setup(t)
	id := e.create(secretTitle)
	e.fail("draft_get", map[string]any{"id": "bad id " + secretTitle})
	e.ok("draft_get", map[string]any{"id": id})
	logs := e.logs.String()
	if strings.Contains(logs, secretTitle) {
		t.Fatalf("logs contain a payload:\n%s", logs)
	}
	for _, want := range []string{"tool=draft_create", "tool=draft_get", "outcome=ok", "outcome=invalid_argument/invalid_id", "id=" + id, "duration_ms="} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs lack %q:\n%s", want, logs)
		}
	}
}

func TestErrorsAreMappedFromAnyStoreError(t *testing.T) {
	// A non-draft error from a store is mapped to internal and never echoed.
	e := setupStore(t, plainErrStore{memstore.New()})
	er := e.fail("draft_get", map[string]any{"id": "drf_00000000000000000000"})
	if er["category"] != "internal" || strings.Contains(er["message"].(string), "/secret/path") {
		t.Fatalf("error = %v", er)
	}
}

type plainErrStore struct{ *memstore.Store }

func (plainErrStore) Get(context.Context, string) (draft.Draft, error) {
	return draft.Draft{}, errors.New("open /secret/path: boom")
}

func TestDuplicateRootKeyIsAToolError(t *testing.T) {
	e := setup(t)
	id := e.create("Keep me")
	for _, raw := range []string{
		`{"id":"` + id + `","id":"drf_00000000000000000000"}`,
		`{"draft":{"title":"A"},"draft":{"title":"B"}}`,
	} {
		tool := "draft_get"
		if strings.Contains(raw, `"draft"`) {
			tool = "draft_create"
		}
		res, err := e.call(tool, json.RawMessage(raw))
		if err != nil || !res.IsError {
			t.Fatalf("%s: err=%v res=%s", raw, err, text(res))
		}
		if er := structured(t, res)["error"].(map[string]any); er["category"] != "invalid_argument" || er["code"] != "duplicate_key" {
			t.Fatalf("%s: error = %v", raw, er)
		}
	}
	if e.listTotal() != 1 {
		t.Fatal("a duplicate-key call stored a draft")
	}
}

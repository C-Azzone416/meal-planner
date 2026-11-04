package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
	"github.com/C-Azzone416/meal-planner/internal/core/version"
	"github.com/C-Azzone416/meal-planner/internal/localstore"
)

type result struct {
	code        int
	stdout, err string
}

// invoke runs the CLI with an isolated environment: no real stdin, env vars or user config dir.
func invoke(t *testing.T, stdin string, envVars map[string]string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	e := env{
		stdin:  strings.NewReader(stdin),
		stdout: &out,
		stderr: &errb,
		getenv: func(k string) string { return envVars[k] },
		userConfigDir: func() (string, error) {
			if d, ok := envVars["__config"]; ok {
				return d, nil
			}
			return "", errors.New("no config dir in test")
		},
	}
	code := runEnv(args, e)
	return result{code, out.String(), errb.String()}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// seed creates drafts through the real service and store with a fixed clock and ids.
func seed(t *testing.T, root string, contents ...string) []draft.Draft {
	t.Helper()
	st, err := localstore.Open(root, localstore.Options{})
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 4, 9, 30, 0, 0, time.UTC)
	n := 0
	svc := draft.NewService(st,
		draft.WithClock(func() time.Time { clock = clock.Add(time.Minute); return clock }),
		draft.WithIDGenerator(func() (string, error) { n++; return "drf_" + strings.Repeat("0", 19) + string(rune('0'+n)), nil }))
	var out []draft.Draft
	for _, c := range contents {
		content, issues := draft.DecodeContent([]byte(c))
		if len(issues) > 0 {
			t.Fatalf("seed content: %v", issues)
		}
		res, err := svc.Create(context.Background(), content)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, res.Draft)
	}
	return out
}

func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if fi.IsDir() {
			out[rel] = "<dir>"
		} else if b, err := os.ReadFile(p); err == nil {
			out[rel] = string(b)
		}
		return nil
	})
	return out
}

func sameSnapshot(t *testing.T, a, b map[string]string) {
	t.Helper()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("the data directory changed:\nbefore %v\nafter  %v", a, b)
	}
}

// onlyJSON asserts s is exactly one JSON document followed by a newline and decodes it into v.
func onlyJSON(t *testing.T, s string, v any) {
	t.Helper()
	if !strings.HasSuffix(s, "\n") {
		t.Fatalf("stdout does not end in a newline: %q", s)
	}
	dec := json.NewDecoder(strings.NewReader(s))
	if err := dec.Decode(v); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, s)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout holds more than one document: %q", s)
	}
}

func TestVersionAndTopLevelUnchanged(t *testing.T) {
	want := "mealplanner " + version.Version + "\n"
	for _, a := range []string{"version", "--version"} {
		r := invoke(t, "", nil, a)
		if r.code != 0 || r.stdout != want || r.err != "" {
			t.Errorf("%s: %+v", a, r)
		}
	}
	r := invoke(t, "", nil)
	if r.code != 2 || r.stdout != "" || !strings.Contains(r.err, "usage: mealplanner") {
		t.Errorf("no args: %+v", r)
	}
	r = invoke(t, "", nil, "bogus")
	if r.code != 2 || r.stdout != "" || !strings.Contains(r.err, `unknown command "bogus"`) {
		t.Errorf("unknown: %+v", r)
	}
	// The package-level entry point used by existing callers still works.
	var out, errb bytes.Buffer
	if code := run([]string{"version"}, &out, &errb); code != 0 || out.String() != want {
		t.Errorf("run: %d %q", code, out.String())
	}
}

func TestHelp(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"--help"}, []string{"draft"}},
		{[]string{"help"}, []string{"draft"}},
		{[]string{"draft", "--help"}, []string{"validate", "list", "show", "not food-safety"}},
		{[]string{"draft", "validate", "--help"}, []string{"<file|->", "--json", "echo '{\"title\"", "Exit status"}},
		{[]string{"draft", "list", "-h"}, []string{"--limit", "--offset", "--data-dir", "MEALPLANNER_DATA_DIR", "Application Support", "Exit status"}},
		{[]string{"draft", "show", "--help"}, []string{"<id>", "--data-dir", "drf_0123456789abcdef0123", "Exit status"}},
	} {
		r := invoke(t, "", nil, tc.args...)
		if r.code != 0 || r.err != "" {
			t.Errorf("%v: %+v", tc.args, r)
		}
		for _, w := range tc.want {
			if !strings.Contains(r.stdout, w) {
				t.Errorf("%v: help lacks %q:\n%s", tc.args, w, r.stdout)
			}
		}
	}
}

func TestUsageErrors(t *testing.T) {
	id := "drf_0123456789abcdef0123"
	dir := t.TempDir()
	for _, args := range [][]string{
		{"draft"},
		{"draft", "bogus"},
		{"draft", "validate"},
		{"draft", "validate", "a", "b"},
		{"draft", "validate", "-", "--data-dir", dir},
		{"draft", "validate", "-", "--bogus"},
		{"draft", "list", "extra"},
		{"draft", "list", "--bogus"},
		{"draft", "list", "--limit", "0"},
		{"draft", "list", "--limit", "-3"},
		{"draft", "list", "--limit", "101"},
		{"draft", "list", "--limit", "abc"},
		{"draft", "list", "--limit"},
		{"draft", "list", "--offset", "-1"},
		{"draft", "list", "--data-dir", "relative/dir"},
		{"draft", "list", "--data-dir", ""},
		{"draft", "list", "--data-dir", "~/drafts"},
		{"draft", "show"},
		{"draft", "show", id, id},
		{"draft", "show", "nope"},
		{"draft", "show", "../x"},
		{"draft", "show", strings.ToUpper(id)},
		{"draft", "show", id + ".json"},
		{"draft", "show", id, "--bogus"},
	} {
		for _, asJSON := range []bool{false, true} {
			a := append([]string(nil), args...)
			if asJSON {
				a = append(a, "--json")
			}
			r := invoke(t, "", map[string]string{"__config": dir}, a...)
			if r.code != 2 {
				t.Errorf("%v: exit %d, want 2 (%+v)", a, r.code, r)
			}
			if r.stdout != "" {
				t.Errorf("%v: stdout is not empty on a usage error: %q", a, r.stdout)
			}
			if r.err == "" {
				t.Errorf("%v: no diagnostic on stderr", a)
			}
		}
	}
}

func TestValidate(t *testing.T) {
	wantInvalid := []string{"required /title", "required /ingredients/0/text", "basis_required /instructions/0/phase_basis",
		"required /sources/0/url", "duplicate_id /sources/1/id", "invalid_value /estimates/active_minutes/value",
		"unknown_source_ref /estimates/active_minutes/basis/source_ref"}
	tests := []struct {
		name     string
		input    string
		wantCode int
		errs     []string // "code path" in order
		warns    []string
	}{
		{"minimal warns but exits 0", string(fixture(t, "minimal.json")), 0, nil, []string{"no_ingredients", "no_instructions", "no_sources"}},
		{"full is clean", string(fixture(t, "full.json")), 0, nil, nil},
		{"invalid", string(fixture(t, "invalid.json")), 1, wantInvalid, nil},
		{"unknown field", string(fixture(t, "unknown_field.json")), 1, []string{"unknown_field"}, nil},
		{"malformed", string(fixture(t, "malformed.json")), 1, []string{"malformed_json"}, nil},
		{"empty input", "", 1, []string{"malformed_json"}, nil},
		{"whitespace only", " \n\t", 1, []string{"malformed_json"}, nil},
		{"not an object", "[1,2]", 1, []string{"invalid_type"}, nil},
		{"trailing data", `{"title":"A"} {"title":"B"}`, 1, []string{"malformed_json"}, nil},
		{"duplicate key", `{"title":"A","title":"B"}`, 1, []string{"duplicate_key"}, nil},
		{"oversized", `{"title":"` + strings.Repeat("a", draft.MaxInputBytes) + `"}`, 1, []string{"input_too_large"}, nil},
		{"unicode title", `{"title":"Cr\u00e8me br\u00fbl\u00e9e \u4e16\ud83c\udf72"}`, 0, nil, []string{"no_ingredients", "no_instructions", "no_sources"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Parity with the core: the same bytes give the same result.
			var want draft.ValidationResult
			if c, issues := draft.DecodeContent([]byte(tc.input)); len(issues) > 0 {
				want = draft.ValidationResult{Scope: draft.ValidationScope, Errors: issues, Warnings: []draft.Issue{}, Notice: draft.ValidationNotice}
			} else {
				want = draft.Validate(c)
			}

			j := invoke(t, tc.input, nil, "draft", "validate", "-", "--json")
			if j.code != tc.wantCode || j.err != "" {
				t.Fatalf("json: %+v", j)
			}
			var got struct {
				Validation draft.ValidationResult `json:"validation"`
			}
			onlyJSON(t, j.stdout, &got)
			if !reflect.DeepEqual(got.Validation, want) {
				t.Fatalf("CLI result differs from the core:\n got %+v\nwant %+v", got.Validation, want)
			}
			if got.Validation.Scope != "structural" || got.Validation.Notice != draft.ValidationNotice {
				t.Errorf("scope/notice missing: %+v", got.Validation)
			}
			var gotErrs []string
			for _, i := range got.Validation.Errors {
				if len(tc.errs) > 1 {
					gotErrs = append(gotErrs, i.Code+" "+i.Path)
				} else {
					gotErrs = append(gotErrs, i.Code)
				}
			}
			if !reflect.DeepEqual(gotErrs, tc.errs) {
				t.Errorf("errors = %v, want %v", gotErrs, tc.errs)
			}
			var gotWarns []string
			for _, i := range got.Validation.Warnings {
				gotWarns = append(gotWarns, i.Code)
			}
			if !reflect.DeepEqual(gotWarns, tc.warns) {
				t.Errorf("warnings = %v, want %v", gotWarns, tc.warns)
			}

			h := invoke(t, tc.input, nil, "draft", "validate", "-")
			if h.code != tc.wantCode || h.err != "" {
				t.Fatalf("human: %+v", h)
			}
			lines := strings.Split(strings.TrimRight(h.stdout, "\n"), "\n")
			if last := lines[len(lines)-1]; last != draft.ValidationNotice {
				t.Errorf("human output must end with the notice, got %q", last)
			}
			for _, i := range want.Errors {
				if !strings.Contains(h.stdout, i.Code) || !strings.Contains(h.stdout, i.Path) {
					t.Errorf("human output lacks error %s %s:\n%s", i.Code, i.Path, h.stdout)
				}
			}
			for _, i := range want.Warnings {
				if !strings.Contains(h.stdout, "warning") || !strings.Contains(h.stdout, i.Code) {
					t.Errorf("human output lacks warning %s", i.Code)
				}
			}
		})
	}
}

func TestValidateFromFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "in.json")
	if err := os.WriteFile(good, fixture(t, "minimal.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := invoke(t, "", nil, "draft", "validate", good); r.code != 0 {
		t.Errorf("file: %+v", r)
	}
	if r := invoke(t, "", nil, "draft", "validate", "--json", good); r.code != 0 {
		t.Errorf("flag before file: %+v", r)
	}
	// Flags after the file are accepted, and -- ends flag parsing.
	if r := invoke(t, "", nil, "draft", "validate", good, "--json"); r.code != 0 || !strings.HasPrefix(r.stdout, `{"validation"`) {
		t.Errorf("flag after file: %+v", r)
	}
	if r := invoke(t, "", nil, "draft", "validate", "--", good); r.code != 0 {
		t.Errorf("after --: %+v", r)
	}

	for name, path := range map[string]string{"missing": filepath.Join(dir, "absent.json"), "directory": dir} {
		for _, asJSON := range []bool{false, true} {
			args := []string{"draft", "validate", path}
			if asJSON {
				args = append(args, "--json")
			}
			r := invoke(t, "", nil, args...)
			if r.code != 3 || r.stdout != "" || !strings.Contains(r.err, "mealplanner:") {
				t.Errorf("%s json=%v: %+v", name, asJSON, r)
			}
		}
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		unreadable := filepath.Join(dir, "secret.json")
		_ = os.WriteFile(unreadable, []byte(`{"title":"x"}`), 0o000)
		if r := invoke(t, "", nil, "draft", "validate", unreadable); r.code != 3 || r.stdout != "" {
			t.Errorf("unreadable: %+v", r)
		}
	}
}

func TestValidateDoesNotTouchTheDataDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	vars := map[string]string{localstore.EnvDataDir: root}
	invoke(t, string(fixture(t, "full.json")), vars, "draft", "validate", "-")
	invoke(t, "{", vars, "draft", "validate", "-")
	if _, err := os.Lstat(root); err == nil {
		t.Fatal("validate created the data directory")
	}
	// A bad data directory in the environment does not matter to validate.
	if r := invoke(t, string(fixture(t, "minimal.json")), map[string]string{localstore.EnvDataDir: "relative"}, "draft", "validate", "-"); r.code != 0 {
		t.Errorf("validate must ignore the data directory: %+v", r)
	}
}

func TestListEmptyAndMissingStore(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	r := invoke(t, "", nil, "draft", "list", "--data-dir", root)
	if r.code != 0 || r.stdout != "no drafts\n" || r.err != "" {
		t.Errorf("human: %+v", r)
	}
	r = invoke(t, "", nil, "draft", "list", "--data-dir", root, "--json")
	if r.code != 0 {
		t.Fatalf("json: %+v", r)
	}
	want := `{"drafts":[],"total":0,"offset":0,"limit":25,"has_more":false,"skipped":[],"skipped_total":0}` + "\n"
	if r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	if _, err := os.Lstat(root); err == nil {
		t.Error("list created the data directory")
	}
	// An existing but empty root, and an empty drafts directory, are empty stores too.
	_ = os.MkdirAll(filepath.Join(root, "drafts"), 0o700)
	before := snapshot(t, root)
	if r := invoke(t, "", nil, "draft", "list", "--data-dir", root); r.code != 0 || r.stdout != "no drafts\n" {
		t.Errorf("empty drafts dir: %+v", r)
	}
	sameSnapshot(t, before, snapshot(t, root))
}

func TestListOrderPagingAndGolden(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	seed(t, root, `{"title":"First"}`, `{"title":"Second"}`, `{"title":"Third \u00e9"}`)
	before := snapshot(t, root)

	r := invoke(t, "", nil, "draft", "list", "--data-dir", root, "--json", "--limit", "2")
	want := `{"drafts":[` +
		`{"id":"drf_00000000000000000003","title":"Third \u00e9","status":"draft","revision":1,"created_at":"2026-11-04T09:33:00Z","updated_at":"2026-11-04T09:33:00Z","ingredient_count":0,"instruction_count":0,"source_count":0,"agent_estimate_count":0},` +
		`{"id":"drf_00000000000000000002","title":"Second","status":"draft","revision":1,"created_at":"2026-11-04T09:32:00Z","updated_at":"2026-11-04T09:32:00Z","ingredient_count":0,"instruction_count":0,"source_count":0,"agent_estimate_count":0}` +
		`],"total":3,"offset":0,"limit":2,"has_more":true,"skipped":[],"skipped_total":0}` + "\n"
	want = strings.Replace(want, `\u00e9`, "é", 1) // the encoder does not escape non-ASCII
	if r.code != 0 || r.stdout != want {
		t.Fatalf("golden mismatch:\n got %q\nwant %q", r.stdout, want)
	}

	h := invoke(t, "", nil, "draft", "list", "--data-dir", root, "--limit", "2")
	if h.code != 0 || h.err != "" {
		t.Fatalf("%+v", h)
	}
	lines := strings.Split(strings.TrimRight(h.stdout, "\n"), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "drf_00000000000000000003  r1  2026-11-04T09:33:00Z  Third") ||
		!strings.Contains(lines[2], "showing 1-2 of 3; use --offset 2") {
		t.Errorf("human list:\n%s", h.stdout)
	}
	if strings.Contains(h.stdout, root) {
		t.Error("stdout contains the data directory path")
	}

	p2 := invoke(t, "", nil, "draft", "list", "--data-dir", root, "--json", "--limit", "2", "--offset", "2")
	var page draft.ListPage
	onlyJSON(t, p2.stdout, &page)
	if len(page.Drafts) != 1 || page.Drafts[0].Title != "First" || page.HasMore || page.Offset != 2 {
		t.Errorf("page 2: %+v", page)
	}
	beyond := invoke(t, "", nil, "draft", "list", "--data-dir", root, "--offset", "9")
	if beyond.code != 0 || !strings.Contains(beyond.stdout, "no drafts at offset 9 (total 3)") {
		t.Errorf("beyond: %+v", beyond)
	}
	sameSnapshot(t, before, snapshot(t, root))
}

func TestListReportsSkippedEntriesWithoutTouchingThem(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	seed(t, root, `{"title":"Good"}`)
	bad := filepath.Join(root, "drafts", "drf_aaaaaaaaaaaaaaaaaaaa.json")
	future := filepath.Join(root, "drafts", "drf_bbbbbbbbbbbbbbbbbbbb.json")
	_ = os.WriteFile(bad, []byte("{broken"), 0o600)
	_ = os.WriteFile(future, []byte(`{"schema_version":2}`), 0o600)
	before := snapshot(t, root)

	r := invoke(t, "", nil, "draft", "list", "--data-dir", root)
	if r.code != 0 || !strings.Contains(r.stdout, "skipped drf_aaaaaaaaaaaaaaaaaaaa: corrupt") ||
		!strings.Contains(r.stdout, "skipped drf_bbbbbbbbbbbbbbbbbbbb: unsupported_schema") ||
		!strings.Contains(r.stdout, "2 unreadable entries skipped and left untouched") {
		t.Errorf("human: %+v", r)
	}
	j := invoke(t, "", nil, "draft", "list", "--data-dir", root, "--json")
	var page draft.ListPage
	onlyJSON(t, j.stdout, &page)
	if j.code != 0 || page.Total != 1 || page.SkippedTotal != 2 || len(page.Skipped) != 2 {
		t.Errorf("json: %+v", page)
	}
	sameSnapshot(t, before, snapshot(t, root))
}

func TestDataDirPrecedence(t *testing.T) {
	base := t.TempDir()
	flagRoot, envRoot, cfgRoot := filepath.Join(base, "flag"), filepath.Join(base, "env"), filepath.Join(base, "cfg")
	seed(t, flagRoot, `{"title":"from flag"}`)
	seed(t, envRoot, `{"title":"from env"}`)
	seed(t, filepath.Join(cfgRoot, "mealplanner"), `{"title":"from default"}`)
	vars := map[string]string{localstore.EnvDataDir: envRoot, "__config": cfgRoot}

	title := func(r result) string {
		var p draft.ListPage
		onlyJSON(t, r.stdout, &p)
		if r.code != 0 || len(p.Drafts) != 1 {
			t.Fatalf("%+v", r)
		}
		return p.Drafts[0].Title
	}
	if got := title(invoke(t, "", vars, "draft", "list", "--json", "--data-dir", flagRoot)); got != "from flag" {
		t.Errorf("flag: %q", got)
	}
	if got := title(invoke(t, "", vars, "draft", "list", "--json")); got != "from env" {
		t.Errorf("env: %q", got)
	}
	delete(vars, localstore.EnvDataDir)
	if got := title(invoke(t, "", vars, "draft", "list", "--json")); got != "from default" {
		t.Errorf("default: %q", got)
	}
	// A bad higher-precedence value is a usage error and never falls through.
	vars[localstore.EnvDataDir] = "relative"
	if r := invoke(t, "", vars, "draft", "list"); r.code != 2 || r.stdout != "" {
		t.Errorf("relative env: %+v", r)
	}
	// No default available and nothing configured.
	if r := invoke(t, "", nil, "draft", "list"); r.code != 2 || !strings.Contains(r.err, "--data-dir") {
		t.Errorf("no default: %+v", r)
	}
}

func TestListUnreadableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits do not apply")
	}
	root := filepath.Join(t.TempDir(), "data")
	seed(t, root, `{"title":"x"}`)
	dir := filepath.Join(root, "drafts")
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	for _, asJSON := range []bool{false, true} {
		args := []string{"draft", "list", "--data-dir", root}
		if asJSON {
			args = append(args, "--json")
		}
		if r := invoke(t, "", nil, args...); r.code != 3 || r.stdout != "" || r.err == "" {
			t.Errorf("json=%v: %+v", asJSON, r)
		}
	}
	id := "drf_00000000000000000001"
	if r := invoke(t, "", nil, "draft", "show", id, "--data-dir", root, "--json"); r.code != 3 || r.stdout != "" {
		t.Errorf("show: %+v", r)
	}
}

func TestShow(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	ds := seed(t, root, string(fixture(t, "full.json")))
	id := ds[0].ID
	before := snapshot(t, root)

	j := invoke(t, "", nil, "draft", "show", id, "--data-dir", root, "--json")
	if j.code != 0 || j.err != "" {
		t.Fatalf("%+v", j)
	}
	var got struct {
		Draft draft.Draft `json:"draft"`
	}
	onlyJSON(t, j.stdout, &got)
	if !reflect.DeepEqual(got.Draft, ds[0]) {
		t.Fatalf("show --json differs from the stored draft:\n got %+v\nwant %+v", got.Draft, ds[0])
	}
	if !strings.HasPrefix(j.stdout, `{"draft":{"schema_version":1,"id":"`+id+`","revision":1,"status":"draft"`) {
		t.Errorf("unexpected JSON start: %.120s", j.stdout)
	}

	h := invoke(t, "", nil, "draft", "show", "--data-dir", root, id) // flag position does not matter
	if h.code != 0 || h.err != "" {
		t.Fatalf("%+v", h)
	}
	for _, want := range []string{
		id + "  revision 1",
		"status: draft (not reviewed or verified)",
		"title: Synthetic Bean Stew",
		"[prep ahead] Soak the beans overnight. [agent estimate, unverified] (Typical for dried beans; not checked against a source.)",
		"active minutes: 20 [agent estimate, unverified]",
		"total minutes: 90 [sourced: s1]",
		"attention: low [agent estimate, unverified]",
		"cleanup: unknown",
		"variability: unknown",
		"prep ahead: unknown",
		"prep tonight: unknown",
		"cook tonight:",
		"s1  url  Example page  https://example.com/recipes/synthetic-bean-stew",
		"- storage: Leftovers may keep for several days in a fridge. [agent estimate, unverified]",
		"- prep_ahead: Beans may be soaked the night before. [sourced: s1]",
	} {
		if !strings.Contains(h.stdout, want) {
			t.Errorf("human output lacks %q:\n%s", want, h.stdout)
		}
	}
	if strings.Contains(h.stdout, root) {
		t.Error("stdout contains the data directory path")
	}
	sameSnapshot(t, before, snapshot(t, root))
}

func TestShowZeroIsNotUnknown(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	ds := seed(t, root, `{"title":"No cook","estimates":{"active_minutes":{"value":0,"basis":{"kind":"agent_estimate"}}}}`)
	r := invoke(t, "", nil, "draft", "show", ds[0].ID, "--data-dir", root)
	if !strings.Contains(r.stdout, "active minutes: 0 [agent estimate, unverified]") || !strings.Contains(r.stdout, "total minutes: unknown") {
		t.Errorf("zero and unknown must differ:\n%s", r.stdout)
	}
}

func TestShowSourcedWithoutReference(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	ds := seed(t, root, `{"title":"x","estimates":{"cleanup":{"value":"high","basis":{"kind":"sourced"}}}}`)
	r := invoke(t, "", nil, "draft", "show", ds[0].ID, "--data-dir", root)
	if !strings.Contains(r.stdout, "cleanup: high [sourced: no reference]") {
		t.Errorf("%s", r.stdout)
	}
}

func TestShowMissingAndDamaged(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	seed(t, root, `{"title":"x"}`)
	missing := "drf_ffffffffffffffffffff"
	before := snapshot(t, root)

	h := invoke(t, "", nil, "draft", "show", missing, "--data-dir", root)
	if h.code != 1 || h.stdout != "" || !strings.Contains(h.err, "no draft with that id") {
		t.Errorf("human: %+v", h)
	}
	j := invoke(t, "", nil, "draft", "show", missing, "--data-dir", root, "--json")
	var e struct {
		Error draft.Error `json:"error"`
	}
	onlyJSON(t, j.stdout, &e)
	if j.code != 1 || e.Error.Category != draft.CategoryNotFound || e.Error.Code != draft.CodeDraftNotFound {
		t.Errorf("json: %+v %+v", j, e)
	}
	// A missing data directory is also just "not found".
	if r := invoke(t, "", nil, "draft", "show", missing, "--data-dir", filepath.Join(t.TempDir(), "none")); r.code != 1 {
		t.Errorf("no root: %+v", r)
	}

	corrupt := "drf_cccccccccccccccccccc"
	future := "drf_dddddddddddddddddddd"
	_ = os.WriteFile(filepath.Join(root, "drafts", corrupt+".json"), []byte("{broken"), 0o600)
	_ = os.WriteFile(filepath.Join(root, "drafts", future+".json"), []byte(`{"schema_version":9}`), 0o600)
	before = snapshot(t, root)
	for _, id := range []string{corrupt, future} {
		for _, asJSON := range []bool{false, true} {
			args := []string{"draft", "show", id, "--data-dir", root}
			if asJSON {
				args = append(args, "--json")
			}
			r := invoke(t, "", nil, args...)
			if r.code != 3 || r.stdout != "" || r.err == "" {
				t.Errorf("%s json=%v: %+v", id, asJSON, r)
			}
		}
	}
	sameSnapshot(t, before, snapshot(t, root))
}

// hostile returns text that would misbehave in a terminal if printed raw: an ANSI escape, carriage
// return, bell, bidi override and isolate, a line separator, and a tab. tag keeps it identifiable.
func hostile(tag string) string {
	return "\x1b[31m" + tag + "\r\x07\u202e\u2066\u2028\t!"
}

// hostileML is for multi-line fields, where a tab and newlines are legitimate.
func hostileML(tag string) string {
	return "\x1b[31m" + tag + "\r\x07\u202e\u2066\u2028\nsecond line"
}

func assertSafe(t *testing.T, where, out string, tags ...string) {
	t.Helper()
	for _, bad := range []string{"\x1b", "\r", "\x07", "\u202e", "\u2066", "\u2028", "\u2029", "\t"} {
		if strings.Contains(out, bad) {
			t.Errorf("%s: raw %q reached the output:\n%q", where, bad, out)
		}
	}
	for _, tag := range tags {
		if !strings.Contains(out, tag) {
			t.Errorf("%s: %s is missing from the output (escaping must not drop text)", where, tag)
		}
	}
}

func TestShowEscapesEveryStoredString(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	st, _ := localstore.Open(root, localstore.Options{})
	basis := func(tag string) *draft.Basis {
		return &draft.Basis{Kind: draft.BasisSourced, SourceRef: hostile(tag + "ref"), Note: hostile(tag + "note")}
	}
	five := 5
	// Content that validation would reject can still reach a file by hand-editing; show must stay safe.
	c := draft.Content{
		Title:       hostile("title"),
		Yield:       hostile("yield"),
		Ingredients: []draft.Ingredient{{Text: hostile("ingredient"), Quantity: hostile("qty")}},
		Instructions: []draft.Step{{
			Text: hostileML("step"), Phase: draft.Phase(hostile("phase")), PhaseBasis: basis("pb"),
		}},
		Notes:   hostileML("notes"),
		Sources: []draft.Source{{ID: hostile("srcid"), Kind: draft.SourceKind(hostile("srckind")), Label: hostile("srclabel"), URL: hostile("srcurl"), Locator: hostile("srcloc")}},
		Estimates: &draft.Estimates{
			ActiveMinutes: &draft.MinutesEstimate{Value: &five, Basis: basis("am")},
			Attention:     &draft.LevelEstimate{Value: draft.Level(hostile("level")), Basis: basis("att")},
		},
		PhaseEstimates: &draft.PhaseEstimates{
			PrepAhead:   &draft.Estimates{Cleanup: &draft.LevelEstimate{Value: draft.Level(hostile("pacl")), Basis: basis("pa")}},
			CookTonight: &draft.Estimates{TotalMinutes: &draft.MinutesEstimate{Value: &five, Basis: basis("ct")}},
		},
		Guidance: []draft.Guidance{
			{Topic: draft.GuidanceTopic(hostile("topic")), Text: hostileML("gtext"), Basis: basis("g")},
			{Topic: draft.TopicOther, Text: "x", Basis: &draft.Basis{Kind: draft.BasisKind(hostile("hkind"))}},
		},
	}
	d := draft.Draft{SchemaVersion: 1, ID: "drf_0123456789abcdef0123", Revision: 1, Status: draft.StatusDraft,
		CreatedAt: time.Now().UTC().Truncate(time.Second), UpdatedAt: time.Now().UTC().Truncate(time.Second), Content: c}
	if err := st.Create(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	r := invoke(t, "", nil, "draft", "show", d.ID, "--data-dir", root)
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	var tags []string
	for _, tag := range []string{"title", "yield", "ingredient", "phase", "srcid", "srckind", "srclabel", "srcurl", "srcloc", "level", "pacl", "topic", "step", "notes", "gtext", "hkind"} {
		tags = append(tags, tag)
	}
	for _, p := range []string{"pb", "am", "att", "pa", "ct", "g"} {
		tags = append(tags, p+"ref", p+"note")
	}
	assertSafe(t, "show", r.stdout, tags...)
	if !strings.Contains(r.stdout, `\u001b[31m`) || !strings.Contains(r.stdout, `\u202e`) {
		t.Errorf("expected visible escapes:\n%s", r.stdout)
	}
	// JSON mode is the faithful view: the encoder escapes control characters itself.
	j := invoke(t, "", nil, "draft", "show", d.ID, "--data-dir", root, "--json")
	for _, bad := range []string{"\x1b", "\r", "\x07", "\n"[:0] + "\t"} {
		if strings.Contains(j.stdout, bad) {
			t.Errorf("raw %q in JSON output", bad)
		}
	}
	var got struct {
		Draft draft.Draft `json:"draft"`
	}
	onlyJSON(t, j.stdout, &got)
	if got.Draft.Title != c.Title {
		t.Error("JSON mode must preserve the text exactly")
	}
}

func TestListEscapesTitles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	st, _ := localstore.Open(root, localstore.Options{})
	now := time.Now().UTC().Truncate(time.Second)
	d := draft.Draft{SchemaVersion: 1, ID: "drf_0123456789abcdef0123", Revision: 1, Status: draft.StatusDraft,
		CreatedAt: now, UpdatedAt: now, Content: draft.Content{Title: hostile("listtitle")}}
	if err := st.Create(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	r := invoke(t, "", nil, "draft", "list", "--data-dir", root)
	assertSafe(t, "list", r.stdout, "listtitle")
	if !strings.Contains(r.stdout, now.Format(time.RFC3339)) {
		t.Errorf("timestamps must be RFC 3339 UTC:\n%s", r.stdout)
	}
}

func TestValidateEscapesDecoderOutput(t *testing.T) {
	in := `{"title":"x","\u001b[31mevil\u202e\r\t":1}`
	r := invoke(t, in, nil, "draft", "validate", "-")
	if r.code != 1 {
		t.Fatalf("%+v", r)
	}
	assertSafe(t, "validate", r.stdout)
}

func TestCorruptFileDoesNotEchoFileContentsOnStderr(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	seed(t, root, `{"title":"x"}`)
	id := "drf_cccccccccccccccccccc"
	_ = os.WriteFile(filepath.Join(root, "drafts", id+".json"), []byte(`{"schema_version":1,"SECRETBYTES":1}`), 0o600)
	for _, args := range [][]string{
		{"draft", "show", id, "--data-dir", root},
		{"draft", "show", id, "--data-dir", root, "--json"},
	} {
		r := invoke(t, "", nil, args...)
		if r.code != 3 || r.stdout != "" || r.err == "" {
			t.Fatalf("%v: %+v", args, r)
		}
		if strings.Contains(r.err, "SECRETBYTES") {
			t.Errorf("%v: stderr echoes file contents: %q", args, r.err)
		}
	}
}

func TestInspectionNeverMutates(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	ds := seed(t, root, string(fixture(t, "full.json")), `{"title":"two"}`)
	_ = os.WriteFile(filepath.Join(root, "drafts", "drf_eeeeeeeeeeeeeeeeeeee.json"), []byte("{broken"), 0o600)
	before := snapshot(t, root)
	for _, args := range [][]string{
		{"draft", "list", "--data-dir", root},
		{"draft", "list", "--data-dir", root, "--json", "--limit", "1", "--offset", "1"},
		{"draft", "show", ds[0].ID, "--data-dir", root},
		{"draft", "show", ds[1].ID, "--data-dir", root, "--json"},
		{"draft", "show", "drf_eeeeeeeeeeeeeeeeeeee", "--data-dir", root},
		{"draft", "show", "drf_ffffffffffffffffffff", "--data-dir", root},
	} {
		invoke(t, "", nil, args...)
		sameSnapshot(t, before, snapshot(t, root))
	}
}

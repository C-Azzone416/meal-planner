package draft_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

func TestDecodeContentFailures(t *testing.T) {
	tests := []struct {
		name string
		in   string
		code string
	}{
		{"empty", ``, "malformed_json"},
		{"whitespace only", " \n\t ", "malformed_json"},
		{"truncated", `{"title": `, "malformed_json"},
		{"not json", `title: x`, "malformed_json"},
		{"trailing garbage", `{"title":"x"} x`, "malformed_json"},
		{"two objects", `{"title":"x"}{"title":"y"}`, "malformed_json"},
		{"trailing comma", `{"title":"x",}`, "malformed_json"},
		{"single quotes", `{'title':'x'}`, "malformed_json"},
		{"byte order mark", "\xef\xbb\xbf" + `{"title":"x"}`, "malformed_json"},
		{"invalid UTF-8", "{\"title\":\"a\xffb\"}", "malformed_json"},
		{"unterminated nested", `{"ingredients":[{"text":"x"`, "malformed_json"},
		{"too deeply nested", strings.Repeat(`{"a":`, 200) + `1` + strings.Repeat(`}`, 200), "malformed_json"},
		{"array root", `[]`, "invalid_type"},
		{"string root", `"x"`, "invalid_type"},
		{"number root", `5`, "invalid_type"},
		{"null root", `null`, "invalid_type"},
		{"server-assigned status", `{"title":"x","status":"reviewed"}`, "unknown_field"},
		{"server-assigned id", `{"title":"x","id":"drf_0123456789abcdef0123"}`, "unknown_field"},
		{"server-assigned revision", `{"title":"x","revision":3}`, "unknown_field"},
		{"server-assigned schema_version", `{"title":"x","schema_version":1}`, "unknown_field"},
		{"server-assigned timestamps", `{"title":"x","created_at":"2026-11-04T09:30:00Z"}`, "unknown_field"},
		{"unknown field", `{"title":"x","servings":4}`, "unknown_field"},
		{"unknown nested field", `{"title":"x","ingredients":[{"text":"x","amount":"1"}]}`, "unknown_field"},
		{"unknown deeply nested", `{"title":"x","estimates":{"active_minutes":{"value":1,"basis":{"kind":"sourced","url":"x"}}}}`, "unknown_field"},
		{"capitalised field is unknown", `{"Title":"x"}`, "unknown_field"},
		{"upper case field is unknown", `{"title":"x","INGREDIENTS":[]}`, "unknown_field"},
		{"capitalised nested field is unknown", `{"title":"x","ingredients":[{"Text":"x"}]}`, "unknown_field"},
		{"duplicate top-level key", `{"title":"a","title":"b"}`, "duplicate_key"},
		{"duplicate nested key", `{"title":"a","ingredients":[{"text":"a","text":"b"}]}`, "duplicate_key"},
		{"duplicate deep key", `{"title":"a","estimates":{"active_minutes":{"value":1,"value":2,"basis":{"kind":"sourced"}}}}`, "duplicate_key"},
		{"duplicate of an unknown key", `{"title":"a","x":1,"x":2}`, "duplicate_key"},
		{"title is a number", `{"title":5}`, "invalid_type"},
		{"title is an object", `{"title":{}}`, "invalid_type"},
		{"title is a bool", `{"title":true}`, "invalid_type"},
		{"ingredients is an object", `{"title":"x","ingredients":{}}`, "invalid_type"},
		{"ingredients is a string", `{"title":"x","ingredients":"2 eggs"}`, "invalid_type"},
		{"ingredient is a string", `{"title":"x","ingredients":["2 eggs"]}`, "invalid_type"},
		{"ingredient is null", `{"title":"x","ingredients":[null]}`, "invalid_type"},
		{"ingredient text is a number", `{"title":"x","ingredients":[{"text":2}]}`, "invalid_type"},
		{"value is a string", `{"title":"x","estimates":{"active_minutes":{"value":"20","basis":{"kind":"sourced"}}}}`, "invalid_type"},
		{"value is a fraction", `{"title":"x","estimates":{"active_minutes":{"value":12.5,"basis":{"kind":"sourced"}}}}`, "invalid_type"},
		{"value uses an exponent", `{"title":"x","estimates":{"active_minutes":{"value":1e2,"basis":{"kind":"sourced"}}}}`, "invalid_type"},
		{"value overflows", `{"title":"x","estimates":{"active_minutes":{"value":99999999999999999999,"basis":{"kind":"sourced"}}}}`, "invalid_type"},
		{"basis is a string", `{"title":"x","estimates":{"active_minutes":{"value":1,"basis":"sourced"}}}`, "invalid_type"},
		{"estimates is a list", `{"title":"x","estimates":[]}`, "invalid_type"},
		{"input over the limit", `{"title":"` + strings.Repeat("a", draft.MaxInputBytes) + `"}`, "input_too_large"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, issues := draft.DecodeContent([]byte(tc.in))
			if len(issues) != 1 {
				t.Fatalf("want exactly one issue, got %+v", issues)
			}
			if issues[0].Code != tc.code {
				t.Fatalf("code = %s, want %s", issues[0].Code, tc.code)
			}
			if !reflect.DeepEqual(c, draft.Content{}) {
				t.Fatalf("a failed decode must return zero Content, got %+v", c)
			}
			if issues[0].Path == "" || issues[0].Path[0] != '/' {
				t.Fatalf("path %q is not a JSON pointer", issues[0].Path)
			}
		})
	}
}

// The fixtures from contract section 16.4.
func TestDecodeContentFixtures(t *testing.T) {
	for name, code := range map[string]string{
		"decode_unknown_field.json": "unknown_field",
		"decode_truncated.json":     "malformed_json",
		"decode_empty.json":         "malformed_json",
	} {
		_, issues := draft.DecodeContent(readFixture(t, name))
		if len(issues) != 1 || issues[0].Code != code {
			t.Errorf("%s: got %+v, want one %s", name, issues, code)
		}
	}
}

func TestDecodeContentPaths(t *testing.T) {
	tests := []struct{ in, path string }{
		{`{"title":"x","servings":4}`, "/servings"},
		{`{"title":"x","ingredients":[{"text":"x"},{"text":"y","amount":1}]}`, "/ingredients/1/amount"},
		{`{"title":5}`, "/title"},
		{`{"title":"x","ingredients":[{"text":2}]}`, "/ingredients/0/text"},
		{`{"title":"x","ingredients":[null]}`, "/ingredients/0"},
		{`[]`, "/"},
	}
	for _, tc := range tests {
		_, issues := draft.DecodeContent([]byte(tc.in))
		if len(issues) != 1 || issues[0].Path != tc.path {
			t.Errorf("%s: got %+v, want path %s", tc.in, issues, tc.path)
		}
	}
}

// Input is never echoed: not a value, and not an odd or long key.
func TestDecodeContentNeverEchoes(t *testing.T) {
	const canary = "CANARYxyz"
	inputs := []string{
		`{"title":{"` + canary + `":1}}`,
		`{"` + canary + `":1}`,
		`{"` + canary + strings.Repeat("k", 100) + `":1}`,
		`{"bad key ` + canary + `":1}`,
		`{"title":"` + canary + `","title":"` + canary + `"}`,
		`{"title":"` + canary + `"} ` + canary,
		`{"title":"x","ingredients":["` + canary + `"]}`,
		`{"title":"x","estimates":{"active_minutes":{"value":"` + canary + `"}}}`,
		`{"title":"x","ingredients":[{"` + canary + `~/":1}]}`,
	}
	for _, in := range inputs {
		_, issues := draft.DecodeContent([]byte(in))
		if len(issues) != 1 {
			t.Fatalf("%q: got %+v", in, issues)
		}
		i := issues[0]
		if strings.Contains(i.Message+i.Path, "CANARY") && !strings.HasPrefix(i.Path, "/CANARYxyz") {
			t.Errorf("%q echoes input: %+v", in, i)
		}
		if strings.Contains(i.Message, "CANARY") {
			t.Errorf("%q: message echoes input: %q", in, i.Message)
		}
	}
	// A plain, short key is shown as a path so the agent can find it; a long or odd one is not.
	if _, is := draft.DecodeContent([]byte(`{"` + canary + strings.Repeat("k", 100) + `":1}`)); is[0].Path != "/" {
		t.Errorf("long key path = %q, want /", is[0].Path)
	}
	if _, is := draft.DecodeContent([]byte(`{"a b":1}`)); is[0].Path != "/" {
		t.Errorf("odd key path = %q, want /", is[0].Path)
	}
}

func TestDecodeContentFixturesDecodeFully(t *testing.T) {
	c := decodeFixture(t, "valid_full.json")
	if c.Title != "Synthetic Bean Stew" || len(c.Ingredients) != 3 || len(c.Instructions) != 3 || len(c.Sources) != 2 || len(c.Guidance) != 2 {
		t.Fatalf("decoded content is incomplete: %+v", c)
	}
	if c.Estimates == nil || c.Estimates.ActiveMinutes == nil || *c.Estimates.ActiveMinutes.Value != 20 {
		t.Fatalf("estimates did not decode: %+v", c.Estimates)
	}
	if c.Estimates.Cleanup != nil || c.Estimates.Variability != nil {
		t.Error("absent estimates must stay nil (unknown), not zero")
	}
	if c.PhaseEstimates == nil || c.PhaseEstimates.PrepAhead != nil || c.PhaseEstimates.PrepTonight != nil || c.PhaseEstimates.CookTonight == nil {
		t.Errorf("phase estimates = %+v", c.PhaseEstimates)
	}
}

// Absent, null and [] are the same on input; lists come out non-nil; empty estimate objects vanish.
func TestDecodeContentNormalizes(t *testing.T) {
	inputs := []string{
		`{"title":"x"}`,
		`{"title":"x","ingredients":null,"instructions":[],"sources":null,"guidance":[]}`,
		`{"title":"x","estimates":null,"phase_estimates":null}`,
		`{"title":"x","estimates":{},"phase_estimates":{}}`,
		`{"title":"x","estimates":{"active_minutes":null,"attention":null},"phase_estimates":{"prep_ahead":{},"cook_tonight":null}}`,
	}
	var first draft.Content
	for i, in := range inputs {
		c, issues := draft.DecodeContent([]byte(in))
		if len(issues) != 0 {
			t.Fatalf("%s: %+v", in, issues)
		}
		if c.Ingredients == nil || c.Instructions == nil || c.Sources == nil || c.Guidance == nil {
			t.Errorf("%s: lists must be non-nil: %+v", in, c)
		}
		if c.Estimates != nil || c.PhaseEstimates != nil {
			t.Errorf("%s: empty estimates must normalize to nil: %+v %+v", in, c.Estimates, c.PhaseEstimates)
		}
		if i == 0 {
			first = c
		} else if !reflect.DeepEqual(c, first) {
			t.Errorf("%s decodes differently from the first input", in)
		}
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{`null`, `"estimates"`, `"phase_estimates"`, `{}`} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s: encoded form %s contains %s", in, b, bad)
			}
		}
		for _, want := range []string{`"ingredients":[]`, `"instructions":[]`, `"sources":[]`, `"guidance":[]`} {
			if !strings.Contains(string(b), want) {
				t.Errorf("%s: encoded form %s lacks %s", in, b, want)
			}
		}
	}
	// An estimate with one real field keeps its object; a known zero is preserved as zero.
	c, issues := draft.DecodeContent([]byte(`{"title":"x","estimates":{"active_minutes":{"value":0,"basis":{"kind":"agent_estimate"}}},"phase_estimates":{"cook_tonight":{"attention":{"value":"low","basis":{"kind":"agent_estimate"}}}}}`))
	if len(issues) != 0 {
		t.Fatal(issues)
	}
	if c.Estimates == nil || c.Estimates.ActiveMinutes == nil || c.Estimates.ActiveMinutes.Value == nil || *c.Estimates.ActiveMinutes.Value != 0 {
		t.Errorf("a present zero must stay a zero: %+v", c.Estimates)
	}
	if c.PhaseEstimates == nil || c.PhaseEstimates.CookTonight == nil || c.PhaseEstimates.PrepAhead != nil {
		t.Errorf("phase estimates = %+v", c.PhaseEstimates)
	}
	// An estimate object that is present but incomplete is kept so Validate can report it.
	c, _ = draft.DecodeContent([]byte(`{"title":"x","estimates":{"active_minutes":{}}}`))
	if c.Estimates == nil || c.Estimates.ActiveMinutes == nil || c.Estimates.ActiveMinutes.Value != nil {
		t.Errorf("incomplete estimate was dropped: %+v", c.Estimates)
	}
	if res := draft.Validate(c); !eq(codes(res.Errors), []string{"required /estimates/active_minutes/value", "basis_required /estimates/active_minutes/basis"}) {
		t.Errorf("errors = %v", codes(res.Errors))
	}
}

// Strings are kept exactly as given: no trimming, case folding or Unicode normalization.
func TestDecodeContentKeepsStringsExactly(t *testing.T) {
	nfc, nfd := "café", "café"
	in := `{"title":"` + nfc + `","yield":" ` + nfd + ` ","notes":"line1\r\nline2\t\\ é 😀 </b> &"}`
	c, issues := draft.DecodeContent([]byte(in))
	if len(issues) != 0 {
		t.Fatal(issues)
	}
	if c.Title != nfc || c.Yield != " "+nfd+" " || c.Notes != "line1\r\nline2\t\\ é 😀 </b> &" {
		t.Fatalf("strings changed: %q %q %q", c.Title, c.Yield, c.Notes)
	}
	if c.Title == c.Yield[1:len(c.Yield)-1] {
		t.Fatal("NFC and NFD forms were normalized together")
	}
}

// DecodeContent then Validate gives the same result as validating the equivalent Go value.
func TestDecodeThenValidateMatchesGoValue(t *testing.T) {
	c := decodeFixture(t, "invalid_content.json")
	want := draft.Content{
		Title:        "   ",
		Ingredients:  []draft.Ingredient{{}},
		Instructions: []draft.Step{{Text: "Stir.", Phase: draft.PhasePrepAhead}},
		Sources:      []draft.Source{{ID: "s1", Kind: draft.SourceURL}, {ID: "s1", Kind: draft.SourcePublication, Label: "Example Cookbook"}},
		Estimates: &draft.Estimates{ActiveMinutes: &draft.MinutesEstimate{
			Value: intp(-5), Basis: &draft.Basis{Kind: draft.BasisSourced, SourceRef: "s9"}}},
		Guidance: []draft.Guidance{},
	}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("decoded:\n%+v\nwant:\n%+v", c, want)
	}
}

// An unpaired surrogate escape would be silently replaced by U+FFFD by encoding/json, so it is
// rejected instead. Pairs, escaped backslashes and a literal U+FFFD are fine.
func TestDecodeContentUnpairedSurrogates(t *testing.T) {
	bad := []struct{ name, in, path string }{
		{"lone high", `{"title":"a\ud800b"}`, "/title"},
		{"lone high at end", `{"title":"a\ud83d"}`, "/title"},
		{"lone low", `{"title":"a\udc00b"}`, "/title"},
		{"lone low upper case", `{"title":"a\uDFFFb"}`, "/title"},
		{"high then ordinary escape", `{"title":"\ud800A"}`, "/title"},
		{"high then high", `{"title":"\ud83d😀"}`, "/title"},
		{"low then high (reversed pair)", `{"title":"\ude00\ud83d"}`, "/title"},
		{"high then plain text", `{"title":"\ud83dx"}`, "/title"},
		{"nested field", `{"title":"x","ingredients":[{"text":"ok"},{"text":"\ud800"}]}`, "/ingredients/1/text"},
		{"deeper field", `{"title":"x","estimates":{"attention":{"value":"low","basis":{"kind":"sourced","note":"\udc00"}}}}`, "/estimates/attention/basis/note"},
		{"after an escaped backslash", `{"title":"\\\ud800"}`, "/title"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			c, issues := draft.DecodeContent([]byte(tc.in))
			if len(issues) != 1 || issues[0].Code != "invalid_characters" || issues[0].Path != tc.path {
				t.Fatalf("got %+v, want invalid_characters at %s", issues, tc.path)
			}
			if !reflect.DeepEqual(c, draft.Content{}) {
				t.Fatalf("a failed decode returned content: %+v", c)
			}
			if strings.Contains(issues[0].Message, "ud800") || strings.Contains(issues[0].Message, "\\u") {
				t.Fatalf("message echoes input: %q", issues[0].Message)
			}
		})
	}
	good := []struct{ name, in, title string }{
		{"valid pair", `{"title":"😀"}`, "😀"},
		{"valid pair upper case", `{"title":"😀"}`, "😀"},
		{"two pairs", `{"title":"😀🍲"}`, "😀🍲"},
		{"pair among text", `{"title":"a😀b"}`, "a😀b"},
		{"escaped backslash then u text", `{"title":"\\ud800"}`, `\ud800`},
		{"ordinary escapes", `{"title":"éA\n"}`, "éA\n"},
		{"literal replacement character is kept", "{\"title\":\"a�b\"}", "a�b"},
		{"BMP edge just below the surrogates", `{"title":"퟿"}`, "퟿"},
		{"BMP edge just above the surrogates", `{"title":""}`, ""},
	}
	for _, tc := range good {
		t.Run(tc.name, func(t *testing.T) {
			c, issues := draft.DecodeContent([]byte(tc.in))
			if len(issues) != 0 {
				t.Fatalf("issues: %+v", issues)
			}
			if c.Title != tc.title {
				t.Fatalf("title = %q, want %q", c.Title, tc.title)
			}
		})
	}
	// A key with an unpaired surrogate is simply an unknown field.
	if _, issues := draft.DecodeContent([]byte(`{"title":"x","a\ud800":1}`)); len(issues) != 1 || issues[0].Code != "unknown_field" {
		t.Fatalf("key with surrogate: %+v", issues)
	}
	// Syntax errors and duplicate keys are found in the parse pass, before any shape or text check.
	if _, issues := draft.DecodeContent([]byte(`{"title":"\ud800","title":"x"}`)); len(issues) != 1 || issues[0].Code != "duplicate_key" {
		t.Fatalf("duplicate key before surrogate: %+v", issues)
	}
	if _, issues := draft.DecodeContent([]byte(`{"title":"\ud800","bogus":1}`)); len(issues) != 1 || issues[0].Code != "invalid_characters" {
		t.Fatalf("first problem in document order: %+v", issues)
	}
	if _, issues := draft.DecodeContent([]byte(`{"bogus":1,"title":"\ud800"}`)); len(issues) != 1 || issues[0].Code != "unknown_field" {
		t.Fatalf("first problem in document order: %+v", issues)
	}
}

func TestValidateRaw(t *testing.T) {
	// Decode failures: exactly the decode issue, no warnings, nothing else.
	for name, in := range map[string]string{
		"malformed": `{"title": `, "empty": ``, "unknown field": `{"title":"x","status":"reviewed"}`,
		"duplicate key": `{"title":"a","title":"b"}`, "wrong type": `{"title":5}`, "surrogate": `{"title":"\ud800"}`,
	} {
		t.Run(name, func(t *testing.T) {
			res := draft.ValidateRaw([]byte(in))
			_, issues := draft.DecodeContent([]byte(in))
			want := draft.ValidationResult{Scope: draft.ValidationScope, StructurallyValid: false, Errors: issues, Warnings: []draft.Issue{}, Notice: draft.ValidationNotice}
			if !reflect.DeepEqual(res, want) {
				t.Fatalf("got %+v, want %+v", res, want)
			}
			if len(res.Errors) != 1 || res.Warnings == nil {
				t.Fatalf("want one error and non-nil warnings: %+v", res)
			}
		})
	}
	// Decodable input: the same as Validate of the decoded content.
	for _, name := range []string{"valid_minimal.json", "valid_full.json", "invalid_content.json"} {
		raw := readFixture(t, name)
		got := draft.ValidateRaw(raw)
		want := draft.Validate(decodeFixture(t, name))
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v, want %+v", name, got, want)
		}
	}
	res := draft.ValidateRaw(readFixture(t, "valid_minimal.json"))
	if !res.StructurallyValid || len(res.Warnings) != 3 {
		t.Errorf("minimal: %+v", res)
	}
	if res := draft.ValidateRaw(readFixture(t, "invalid_content.json")); res.StructurallyValid || len(res.Errors) != 7 {
		t.Errorf("invalid: %+v", res)
	}
	// It encodes with [] lists like every other result.
	b, _ := json.Marshal(draft.ValidateRaw([]byte(`{`)))
	if !strings.Contains(string(b), `"warnings":[]`) || strings.Contains(string(b), "null") {
		t.Errorf("encoded: %s", b)
	}
}

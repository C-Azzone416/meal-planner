package draft_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

type expected struct {
	Errors   []draft.Issue `json:"errors"`
	Warnings []draft.Issue `json:"warnings"`
}

func pairs(issues []draft.Issue) []string {
	out := make([]string, 0, len(issues))
	for _, i := range issues {
		out = append(out, i.Code+" "+i.Path)
	}
	return out
}

// The contract's section 16 examples, as shared fixtures, give exactly the expected codes and paths
// in the expected order.
func TestValidateFixtures(t *testing.T) {
	for _, name := range []string{"valid_minimal", "valid_full", "invalid_content"} {
		t.Run(name, func(t *testing.T) {
			c := decodeFixture(t, name+".json")
			var want expected
			if err := json.Unmarshal(readFixture(t, name+".expected.json"), &want); err != nil {
				t.Fatal(err)
			}
			got := draft.Validate(c)
			if !eq(pairs(got.Errors), pairs(want.Errors)) {
				t.Errorf("errors:\n got %v\nwant %v", pairs(got.Errors), pairs(want.Errors))
			}
			if !eq(pairs(got.Warnings), pairs(want.Warnings)) {
				t.Errorf("warnings:\n got %v\nwant %v", pairs(got.Warnings), pairs(want.Warnings))
			}
			if got.StructurallyValid != (len(want.Errors) == 0) {
				t.Errorf("structurally_valid = %v", got.StructurallyValid)
			}
			if got.Scope != draft.ValidationScope || got.Notice != draft.ValidationNotice {
				t.Errorf("scope/notice = %q / %q", got.Scope, got.Notice)
			}
		})
	}
}

func TestValidateTable(t *testing.T) {
	full := func() draft.Content { return decodeFixture(t, "valid_full.json") }
	tests := []struct {
		name     string
		mod      func(c *draft.Content)
		errs     []string
		warnings []string
	}{
		{"valid full has no findings", func(c *draft.Content) {}, nil, nil},

		// title
		{"title blank", func(c *draft.Content) { c.Title = "" }, []string{"required /title"}, nil},
		{"title whitespace only", func(c *draft.Content) { c.Title = " \t  " }, []string{"required /title"}, nil},
		{"title newline", func(c *draft.Content) { c.Title = "a\nb" }, []string{"invalid_characters /title"}, nil},
		{"title NUL", func(c *draft.Content) { c.Title = "a\x00b" }, []string{"invalid_characters /title"}, nil},
		{"title DEL and C1", func(c *draft.Content) { c.Title = "a\u007fb\u0085" }, []string{"invalid_characters /title"}, nil},
		{"title keeps surrounding spaces", func(c *draft.Content) { c.Title = "  padded  " }, nil, nil},
		{"title invalid UTF-8", func(c *draft.Content) { c.Title = "a\xffb" }, []string{"invalid_characters /title"}, nil},
		{"title too long and control char both reported", func(c *draft.Content) { c.Title = runes(201, "a") + "\x01" },
			[]string{"invalid_characters /title", "too_long /title"}, nil},

		// yield, notes
		{"yield too long", func(c *draft.Content) { c.Yield = runes(101, "y") }, []string{"too_long /yield"}, nil},
		{"yield newline", func(c *draft.Content) { c.Yield = "4\n6" }, []string{"invalid_characters /yield"}, nil},
		{"notes multiline ok", func(c *draft.Content) { c.Notes = "a\nb\r\nc\td" }, nil, nil},
		{"notes control char", func(c *draft.Content) { c.Notes = "a\x07" }, []string{"invalid_characters /notes"}, nil},
		{"notes NUL even though multiline", func(c *draft.Content) { c.Notes = "a\x00" }, []string{"invalid_characters /notes"}, nil},
		{"notes too long", func(c *draft.Content) { c.Notes = runes(4001, "n") }, []string{"too_long /notes"}, nil},
		{"notes whitespace only is fine (optional)", func(c *draft.Content) { c.Notes = "   " }, nil, nil},

		// ingredients
		{"ingredient text blank", func(c *draft.Content) { c.Ingredients[1].Text = "  " }, []string{"required /ingredients/1/text"}, nil},
		{"ingredient fields too long", func(c *draft.Content) {
			c.Ingredients[0].Quantity = runes(51, "1")
			c.Ingredients[0].Unit = runes(51, "u")
			c.Ingredients[0].Name = runes(201, "n")
			c.Ingredients[0].Note = runes(301, "x")
			c.Ingredients[0].Text = runes(301, "t")
		}, []string{"too_long /ingredients/0/text", "too_long /ingredients/0/quantity", "too_long /ingredients/0/unit", "too_long /ingredients/0/name", "too_long /ingredients/0/note"}, nil},
		{"ingredient text newline", func(c *draft.Content) { c.Ingredients[2].Text = "a\nb" }, []string{"invalid_characters /ingredients/2/text"}, nil},
		{"no ingredients warns", func(c *draft.Content) { c.Ingredients = nil }, nil, []string{"no_ingredients /ingredients"}},
		{"empty ingredients warns the same", func(c *draft.Content) { c.Ingredients = []draft.Ingredient{} }, nil, []string{"no_ingredients /ingredients"}},
		{"quantity is free text", func(c *draft.Content) { c.Ingredients[0].Quantity = "1 1/2" }, nil, nil},

		// instructions and phases
		{"step text blank", func(c *draft.Content) { c.Instructions[0].Text = "" }, []string{"required /instructions/0/text"}, nil},
		{"step text multiline ok", func(c *draft.Content) { c.Instructions[1].Text = "a\nb" }, nil, nil},
		{"step too long", func(c *draft.Content) { c.Instructions[1].Text = runes(2001, "s") }, []string{"too_long /instructions/1/text"}, nil},
		{"bad phase", func(c *draft.Content) { c.Instructions[1].Phase = "later" }, []string{"invalid_value /instructions/1/phase"}, nil},
		{"prep_ahead without basis", func(c *draft.Content) { c.Instructions[0].PhaseBasis = nil }, []string{"basis_required /instructions/0/phase_basis"}, nil},
		{"phase_basis without phase", func(c *draft.Content) {
			c.Instructions[1].Phase = ""
			c.Instructions[1].PhaseBasis = agent()
		}, []string{"invalid_value /instructions/1/phase_basis"}, []string{"partial_phase_assignment /instructions"}},
		{"phase_basis allowed on other phases", func(c *draft.Content) { c.Instructions[2].PhaseBasis = agent() }, nil, nil},
		{"partial phase assignment warns", func(c *draft.Content) { c.Instructions[1].Phase = "" }, nil, []string{"partial_phase_assignment /instructions"}},
		{"no phases at all is fine", func(c *draft.Content) {
			for i := range c.Instructions {
				c.Instructions[i].Phase = ""
				c.Instructions[i].PhaseBasis = nil
			}
		}, nil, nil},
		{"no instructions warns", func(c *draft.Content) { c.Instructions = nil }, nil, []string{"no_instructions /instructions"}},

		// sources
		{"no sources warns", func(c *draft.Content) {
			c.Sources = nil
			c.Estimates.TotalMinutes.Basis.SourceRef = ""
			c.Guidance[1].Basis.SourceRef = ""
		}, nil, []string{"no_sources /sources", "sourced_without_reference /estimates/total_minutes/basis", "sourced_without_reference /guidance/1/basis"}},
		{"source id blank", func(c *draft.Content) { c.Sources[1].ID = "" }, []string{"required /sources/1/id"}, nil},
		{"source id bad pattern", func(c *draft.Content) { c.Sources[1].ID = "S2" }, []string{"invalid_value /sources/1/id"}, nil},
		{"source id starts with digit", func(c *draft.Content) { c.Sources[1].ID = "2s" }, []string{"invalid_value /sources/1/id"}, nil},
		{"source id 32 chars ok", func(c *draft.Content) { c.Sources[1].ID = "a" + runes(31, "b") }, nil, nil},
		{"source id 33 chars bad", func(c *draft.Content) { c.Sources[1].ID = "a" + runes(32, "b") }, []string{"invalid_value /sources/1/id"}, nil},
		{"source ids may use dash and underscore", func(c *draft.Content) { c.Sources[1].ID = "my_src-2" }, nil, nil},
		{"duplicate source id reported at the second", func(c *draft.Content) { c.Sources[1].ID = "s1" }, []string{"duplicate_id /sources/1/id"}, nil},
		{"source kind missing", func(c *draft.Content) { c.Sources[1].Kind = "" }, []string{"required /sources/1/kind"}, nil},
		{"source kind unknown", func(c *draft.Content) { c.Sources[1].Kind = "blog" }, []string{"invalid_value /sources/1/kind"}, nil},
		{"url source without url", func(c *draft.Content) { c.Sources[0].URL = "" }, []string{"required /sources/0/url"}, nil},
		{"url on non-url source", func(c *draft.Content) { c.Sources[1].URL = "https://example.com/x" }, []string{"invalid_value /sources/1/url"}, nil},
		{"non-url source needs label or locator", func(c *draft.Content) { c.Sources[1].Label = "" }, []string{"required /sources/1/label"}, nil},
		{"non-url source with only a locator is fine", func(c *draft.Content) {
			c.Sources[1].Label = ""
			c.Sources[1].Locator = "page 3"
		}, nil, nil},
		{"url source does not need a label", func(c *draft.Content) { c.Sources[0].Label = "" }, nil, nil},
		{"url ftp scheme", func(c *draft.Content) { c.Sources[0].URL = "ftp://example.com/x" }, []string{"invalid_value /sources/0/url"}, nil},
		{"url javascript scheme", func(c *draft.Content) { c.Sources[0].URL = "javascript:alert(1)" }, []string{"invalid_value /sources/0/url"}, nil},
		{"url relative", func(c *draft.Content) { c.Sources[0].URL = "/recipes/x" }, []string{"invalid_value /sources/0/url"}, nil},
		{"url no host", func(c *draft.Content) { c.Sources[0].URL = "https:///x" }, []string{"invalid_value /sources/0/url"}, nil},
		{"url scheme only", func(c *draft.Content) { c.Sources[0].URL = "https://" }, []string{"invalid_value /sources/0/url"}, nil},
		{"url with userinfo", func(c *draft.Content) { c.Sources[0].URL = "https://user:pw@example.com/x" }, []string{"invalid_value /sources/0/url"}, nil},
		{"url with empty userinfo", func(c *draft.Content) { c.Sources[0].URL = "https://@example.com/x" }, []string{"invalid_value /sources/0/url"}, nil},
		{"url with space", func(c *draft.Content) { c.Sources[0].URL = "https://example.com/a b" }, []string{"invalid_value /sources/0/url"}, nil},
		{"url with control char", func(c *draft.Content) { c.Sources[0].URL = "https://example.com/a\x01" }, []string{"invalid_characters /sources/0/url"}, nil},
		{"url http ok", func(c *draft.Content) { c.Sources[0].URL = "http://example.com:8080/x?q=1#f" }, nil, nil},
		{"url uppercase scheme ok", func(c *draft.Content) { c.Sources[0].URL = "HTTPS://EXAMPLE.com/x" }, nil, nil},
		{"url too long", func(c *draft.Content) { c.Sources[0].URL = "https://example.com/" + runes(2040, "a") }, []string{"too_long /sources/0/url"}, nil},
		{"source label too long", func(c *draft.Content) { c.Sources[0].Label = runes(201, "l") }, []string{"too_long /sources/0/label"}, nil},
		{"source locator too long", func(c *draft.Content) { c.Sources[1].Locator = runes(201, "l") }, []string{"too_long /sources/1/locator"}, nil},

		// bases
		{"estimate basis missing", func(c *draft.Content) { c.Estimates.ActiveMinutes.Basis = nil }, []string{"basis_required /estimates/active_minutes/basis"}, nil},
		{"basis kind missing", func(c *draft.Content) { c.Estimates.Attention.Basis.Kind = "" }, []string{"required /estimates/attention/basis/kind"}, nil},
		{"basis kind unknown", func(c *draft.Content) { c.Estimates.Attention.Basis.Kind = "verified" }, []string{"invalid_value /estimates/attention/basis/kind"}, nil},
		{"unknown source_ref", func(c *draft.Content) { c.Estimates.TotalMinutes.Basis.SourceRef = "s9" }, []string{"unknown_source_ref /estimates/total_minutes/basis/source_ref"}, nil},
		{"agent_estimate with a source_ref is allowed", func(c *draft.Content) { c.Estimates.ActiveMinutes.Basis.SourceRef = "s2" }, nil, nil},
		{"sourced without reference warns", func(c *draft.Content) { c.Estimates.TotalMinutes.Basis.SourceRef = "" }, nil, []string{"sourced_without_reference /estimates/total_minutes/basis"}},
		{"basis note too long", func(c *draft.Content) { c.Estimates.Attention.Basis.Note = runes(301, "n") }, []string{"too_long /estimates/attention/basis/note"}, nil},
		{"phase_basis source_ref checked", func(c *draft.Content) { c.Instructions[0].PhaseBasis.SourceRef = "nope" }, []string{"unknown_source_ref /instructions/0/phase_basis/source_ref"}, nil},

		// estimates: unknown is not zero
		{"zero minutes is a valid estimate", func(c *draft.Content) { c.Estimates.ActiveMinutes.Value = intp(0) }, nil, nil},
		{"minutes at maximum", func(c *draft.Content) {
			c.Estimates.ActiveMinutes.Value = intp(draft.MaxMinutes)
			c.Estimates.TotalMinutes.Value = intp(draft.MaxMinutes)
		}, nil, nil},
		{"minutes above maximum", func(c *draft.Content) { c.Estimates.TotalMinutes.Value = intp(draft.MaxMinutes + 1) }, []string{"invalid_value /estimates/total_minutes/value"}, nil},
		{"negative minutes", func(c *draft.Content) { c.Estimates.ActiveMinutes.Value = intp(-1) }, []string{"invalid_value /estimates/active_minutes/value"}, nil},
		{"minutes value missing", func(c *draft.Content) { c.Estimates.ActiveMinutes.Value = nil }, []string{"required /estimates/active_minutes/value"}, nil},
		{"minutes object empty gives value and basis errors", func(c *draft.Content) { c.Estimates.TotalMinutes = &draft.MinutesEstimate{} },
			[]string{"required /estimates/total_minutes/value", "basis_required /estimates/total_minutes/basis"}, nil},
		{"level value missing", func(c *draft.Content) { c.Estimates.Attention.Value = "" }, []string{"required /estimates/attention/value"}, nil},
		{"level value unknown", func(c *draft.Content) { c.Estimates.Cleanup = &draft.LevelEstimate{Value: "extreme", Basis: agent()} }, []string{"invalid_value /estimates/cleanup/value"}, nil},
		{"level medium and high ok", func(c *draft.Content) {
			c.Estimates.Cleanup = &draft.LevelEstimate{Value: draft.LevelMedium, Basis: agent()}
			c.Estimates.Variability = &draft.LevelEstimate{Value: draft.LevelHigh, Basis: agent()}
		}, nil, nil},
		{"active exceeds total warns", func(c *draft.Content) { c.Estimates.ActiveMinutes.Value = intp(91) }, nil, []string{"active_exceeds_total /estimates/active_minutes/value"}},
		{"active equal to total does not warn", func(c *draft.Content) { c.Estimates.ActiveMinutes.Value = intp(90) }, nil, nil},
		{"active exceeds total in a phase", func(c *draft.Content) { c.PhaseEstimates.CookTonight.ActiveMinutes.Value = intp(61) }, nil, []string{"active_exceeds_total /phase_estimates/cook_tonight/active_minutes/value"}},
		{"phase estimate errors have phase paths", func(c *draft.Content) {
			c.PhaseEstimates.PrepAhead = &draft.Estimates{Attention: &draft.LevelEstimate{Value: "x"}}
		}, []string{"invalid_value /phase_estimates/prep_ahead/attention/value", "basis_required /phase_estimates/prep_ahead/attention/basis"}, nil},

		// guidance
		{"guidance without basis", func(c *draft.Content) { c.Guidance[0].Basis = nil }, []string{"basis_required /guidance/0/basis"}, nil},
		{"guidance topic missing", func(c *draft.Content) { c.Guidance[0].Topic = "" }, []string{"required /guidance/0/topic"}, nil},
		{"guidance topic unknown", func(c *draft.Content) { c.Guidance[0].Topic = "shopping" }, []string{"invalid_value /guidance/0/topic"}, nil},
		{"guidance text blank", func(c *draft.Content) { c.Guidance[1].Text = "\n" }, []string{"required /guidance/1/text"}, nil},
		{"guidance text multiline ok", func(c *draft.Content) { c.Guidance[1].Text = "a\nb" }, nil, nil},
		{"guidance text too long", func(c *draft.Content) { c.Guidance[1].Text = runes(1001, "g") }, []string{"too_long /guidance/1/text"}, nil},
		{"guidance food_safety and other topics ok", func(c *draft.Content) {
			c.Guidance = append(c.Guidance,
				draft.Guidance{Topic: draft.TopicFoodSafety, Text: "x", Basis: agent()},
				draft.Guidance{Topic: draft.TopicOther, Text: "y", Basis: agent()})
		}, nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := full()
			tc.mod(&c)
			got := draft.Validate(c)
			if !eq(codes(got.Errors), tc.errs) && !(len(tc.errs) == 0 && len(got.Errors) == 0) {
				t.Errorf("errors:\n got %v\nwant %v", codes(got.Errors), tc.errs)
			}
			if !eq(codes(got.Warnings), tc.warnings) && !(len(tc.warnings) == 0 && len(got.Warnings) == 0) {
				t.Errorf("warnings:\n got %v\nwant %v", codes(got.Warnings), tc.warnings)
			}
			if got.StructurallyValid != (len(tc.errs) == 0) {
				t.Errorf("structurally_valid = %v with %d errors", got.StructurallyValid, len(got.Errors))
			}
			if got.Errors == nil || got.Warnings == nil {
				t.Error("Errors and Warnings must never be nil")
			}
		})
	}
}

// Empty, partial and zero-value input.
func TestValidateEmptyAndPartial(t *testing.T) {
	got := draft.Validate(draft.Content{})
	if !eq(codes(got.Errors), []string{"required /title"}) {
		t.Errorf("zero Content errors = %v", codes(got.Errors))
	}
	if !eq(codes(got.Warnings), []string{"no_ingredients /ingredients", "no_instructions /instructions", "no_sources /sources"}) {
		t.Errorf("zero Content warnings = %v", codes(got.Warnings))
	}
	got = draft.Validate(draft.Content{Title: "T"})
	if !got.StructurallyValid || len(got.Errors) != 0 || len(got.Warnings) != 3 {
		t.Errorf("title-only content = %+v", got)
	}
	// Partial: only a title and one blank ingredient.
	got = draft.Validate(draft.Content{Title: "T", Ingredients: []draft.Ingredient{{}}})
	if !eq(codes(got.Errors), []string{"required /ingredients/0/text"}) {
		t.Errorf("partial errors = %v", codes(got.Errors))
	}
}

func TestValidateLimits(t *testing.T) {
	tests := []struct {
		name     string
		mod      func(c *draft.Content)
		wantCode string // empty: must be valid
	}{
		{"title at limit", func(c *draft.Content) { c.Title = runes(draft.MaxTitleRunes, "a") }, ""},
		{"title over limit", func(c *draft.Content) { c.Title = runes(draft.MaxTitleRunes+1, "a") }, "too_long"},
		{"ingredients at limit", func(c *draft.Content) {
			c.Ingredients = make([]draft.Ingredient, draft.MaxIngredients)
			fill(c.Ingredients)
		}, ""},
		{"ingredients over limit", func(c *draft.Content) {
			c.Ingredients = make([]draft.Ingredient, draft.MaxIngredients+1)
			fill(c.Ingredients)
		}, "too_many_items"},
		{"instructions at limit", func(c *draft.Content) { c.Instructions = steps(draft.MaxInstructions) }, ""},
		{"instructions over limit", func(c *draft.Content) { c.Instructions = steps(draft.MaxInstructions + 1) }, "too_many_items"},
		{"sources at limit", func(c *draft.Content) { c.Sources = sources(draft.MaxSources) }, ""},
		{"sources over limit", func(c *draft.Content) { c.Sources = sources(draft.MaxSources + 1) }, "too_many_items"},
		{"guidance at limit", func(c *draft.Content) { c.Guidance = guidance(draft.MaxGuidance) }, ""},
		{"guidance over limit", func(c *draft.Content) { c.Guidance = guidance(draft.MaxGuidance + 1) }, "too_many_items"},
		{"url at byte limit", func(c *draft.Content) {
			c.Sources = []draft.Source{{ID: "s", Kind: draft.SourceURL, URL: "https://example.com/" + runes(draft.MaxURLBytes-len("https://example.com/"), "a")}}
		}, ""},
		{"url one byte over", func(c *draft.Content) {
			c.Sources = []draft.Source{{ID: "s", Kind: draft.SourceURL, URL: "https://example.com/" + runes(draft.MaxURLBytes-len("https://example.com/")+1, "a")}}
		}, "too_long"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := draft.Content{Title: "T"}
			tc.mod(&c)
			res := draft.Validate(c)
			if tc.wantCode == "" {
				if !res.StructurallyValid {
					t.Fatalf("want valid, got %v", codes(res.Errors))
				}
				return
			}
			if len(res.Errors) == 0 || res.Errors[0].Code != tc.wantCode {
				t.Fatalf("want first error %s, got %v", tc.wantCode, codes(res.Errors))
			}
		})
	}
}

func fill(in []draft.Ingredient) {
	for i := range in {
		in[i].Text = fmt.Sprintf("ingredient %d", i)
	}
}

func steps(n int) []draft.Step {
	out := make([]draft.Step, n)
	for i := range out {
		out[i].Text = fmt.Sprintf("step %d", i)
	}
	return out
}

func sources(n int) []draft.Source {
	out := make([]draft.Source, n)
	for i := range out {
		out[i] = draft.Source{ID: fmt.Sprintf("s%d", i), Kind: draft.SourceUserSupplied, Label: "l"}
	}
	return out
}

func guidance(n int) []draft.Guidance {
	out := make([]draft.Guidance, n)
	for i := range out {
		out[i] = draft.Guidance{Topic: draft.TopicOther, Text: "g", Basis: agent()}
	}
	return out
}

// Length limits count Unicode code points, not bytes or UTF-16 units; strings are stored as given.
func TestValidateUnicodeRunes(t *testing.T) {
	tests := []struct {
		name  string
		title string
		ok    bool
	}{
		{"200 four-byte runes (800 bytes)", runes(200, "😀"), true},
		{"201 four-byte runes", runes(201, "😀"), false},
		{"200 two-byte runes", runes(200, "é"), true},
		{"200 CJK runes", runes(200, "日"), true},
		{"201 CJK runes", runes(201, "日"), false},
		{"combining marks count as runes", runes(100, "é"), true},
		{"combining marks over the limit", runes(101, "é"), false},
		{"right-to-left text", "שלום עולם مرحبا", true},
		{"zero width joiner sequence", "👩‍👩‍👧", true},
		{"bidi override is not a control character", "a‮b", true},
		{"line separator U+2028 is not a control character", "a b", true},
		{"non-BMP letters", "𝒜𝒷𝒸", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := draft.Validate(draft.Content{Title: tc.title})
			if res.StructurallyValid != tc.ok {
				t.Fatalf("valid = %v, errors %v", res.StructurallyValid, codes(res.Errors))
			}
		})
	}
}

// Validation is pure and tolerant: it never changes its input, and nil and empty lists are the same.
func TestValidateDoesNotMutateInput(t *testing.T) {
	c := decodeFixture(t, "invalid_content.json")
	before := c.Clone()
	draft.Validate(c)
	if !reflect.DeepEqual(c, before) {
		t.Fatal("Validate changed its input")
	}
	a := draft.Validate(draft.Content{Title: "T"})
	b := draft.Validate(draft.Content{Title: "T", Ingredients: []draft.Ingredient{}, Instructions: []draft.Step{}, Sources: []draft.Source{}, Guidance: []draft.Guidance{}})
	if !reflect.DeepEqual(a, b) {
		t.Fatal("nil and empty lists validate differently")
	}
}

// At most MaxIssuesReported errors and warnings are returned, in schema order, with truncated set.
func TestValidateTruncates(t *testing.T) {
	c := draft.Content{Title: "T", Ingredients: make([]draft.Ingredient, 60)} // 60 blank texts
	res := draft.Validate(c)
	if len(res.Errors) != draft.MaxIssuesReported || !res.Truncated || res.StructurallyValid {
		t.Fatalf("errors=%d truncated=%v valid=%v", len(res.Errors), res.Truncated, res.StructurallyValid)
	}
	if res.Errors[0].Path != "/ingredients/0/text" || res.Errors[49].Path != "/ingredients/49/text" {
		t.Fatalf("not in schema order: %s .. %s", res.Errors[0].Path, res.Errors[49].Path)
	}
	// Exactly at the cap is not truncated.
	c.Ingredients = make([]draft.Ingredient, 50)
	if res := draft.Validate(c); res.Truncated || len(res.Errors) != 50 {
		t.Fatalf("at the cap: errors=%d truncated=%v", len(res.Errors), res.Truncated)
	}
	// Warnings are capped separately.
	c = decodeFixture(t, "valid_full.json")
	c.Instructions = nil
	c.Estimates = &draft.Estimates{}
	c.Sources = []draft.Source{{ID: "s", Kind: draft.SourceUserSupplied, Label: "l"}}
	for i := 0; i < 60; i++ {
		c.Guidance = append(c.Guidance, draft.Guidance{Topic: draft.TopicOther, Text: "x", Basis: &draft.Basis{Kind: draft.BasisSourced}})
	}
	res = draft.Validate(c)
	if len(res.Warnings) != draft.MaxIssuesReported {
		t.Fatalf("warnings = %d, want %d", len(res.Warnings), draft.MaxIssuesReported)
	}
	if !res.Truncated {
		t.Error("warnings were capped but truncated is false")
	}
}

func TestValidateDraftTooLarge(t *testing.T) {
	// Every field is within its own limit, but 100 steps of 2000 four-byte runes exceed 128 KiB.
	c := draft.Content{Title: "T", Instructions: make([]draft.Step, 100)}
	for i := range c.Instructions {
		c.Instructions[i].Text = runes(2000, "😀")
	}
	res := draft.Validate(c)
	if !eq(codes(res.Errors), []string{"draft_too_large /"}) {
		t.Fatalf("errors = %v", codes(res.Errors))
	}
	// Just under the limit is fine.
	c.Instructions = c.Instructions[:15] // 15 * 8000 bytes = 120000 < 131072
	if res := draft.Validate(c); !res.StructurallyValid {
		t.Fatalf("under the limit: %v", codes(res.Errors))
	}
	b, _ := json.Marshal(c)
	if len(b) > draft.MaxDraftBytes {
		t.Fatalf("test setup: %d bytes", len(b))
	}
}

// No issue message or path may echo a value from the draft.
func TestValidateNeverEchoesValues(t *testing.T) {
	const canary = "CANARYxyz"
	c := draft.Content{
		Title:       canary + "\x01" + runes(200, "a"),
		Yield:       canary + "\n",
		Ingredients: []draft.Ingredient{{Text: " ", Quantity: canary + runes(60, "q"), Unit: canary + runes(60, "u")}},
		Instructions: []draft.Step{
			{Text: canary + "\x00", Phase: draft.Phase(canary), PhaseBasis: &draft.Basis{Kind: draft.BasisKind(canary), SourceRef: canary, Note: canary + runes(400, "n")}},
		},
		Sources: []draft.Source{
			{ID: canary, Kind: draft.SourceKind(canary), Label: canary + "\x01", URL: canary},
			{ID: "ok", Kind: draft.SourceURL, URL: "https://user:" + canary + "@example.com/"},
			{ID: "ok", Kind: draft.SourceURL, URL: "ftp://" + canary + ".example.com/" + runes(2100, "a")},
		},
		Estimates: &draft.Estimates{
			Attention: &draft.LevelEstimate{Value: draft.Level(canary), Basis: &draft.Basis{Kind: draft.BasisKind(canary), SourceRef: canary}},
		},
		Guidance: []draft.Guidance{{Topic: draft.GuidanceTopic(canary), Text: canary}},
	}
	res := draft.Validate(c)
	if len(res.Errors) < 10 {
		t.Fatalf("test setup: only %d errors", len(res.Errors))
	}
	for _, i := range append(res.Errors, res.Warnings...) {
		if strings.Contains(i.Message, "CANARY") || strings.Contains(i.Path, "CANARY") || strings.Contains(i.Code, "CANARY") {
			t.Errorf("issue echoes a value: %+v", i)
		}
	}
}

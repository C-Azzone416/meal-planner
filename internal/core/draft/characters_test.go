package draft_test

import (
	"fmt"
	"testing"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

// Exercise the policy through each free-text field, including nested bases and
// URL paths. Enums and identifiers retain their separate schema constraints.
func TestValidateTextCharacterPolicy(t *testing.T) {
	fields := []struct {
		name      string
		multiline bool
		set       func(*draft.Content, string)
	}{
		{"title", false, func(c *draft.Content, s string) { c.Title = s }},
		{"yield", false, func(c *draft.Content, s string) { c.Yield = s }},
		{"ingredient text", false, func(c *draft.Content, s string) { c.Ingredients[0].Text = s }},
		{"quantity", false, func(c *draft.Content, s string) { c.Ingredients[0].Quantity = s }},
		{"unit", false, func(c *draft.Content, s string) { c.Ingredients[0].Unit = s }},
		{"name", false, func(c *draft.Content, s string) { c.Ingredients[0].Name = s }},
		{"ingredient note", false, func(c *draft.Content, s string) { c.Ingredients[0].Note = s }},
		{"instruction text", true, func(c *draft.Content, s string) { c.Instructions[0].Text = s }},
		{"phase basis note", false, func(c *draft.Content, s string) { c.Instructions[0].PhaseBasis.Note = s }},
		{"notes", true, func(c *draft.Content, s string) { c.Notes = s }},
		{"source label", false, func(c *draft.Content, s string) { c.Sources[0].Label = s }},
		{"source locator", false, func(c *draft.Content, s string) { c.Sources[0].Locator = s }},
		{"URL path", false, func(c *draft.Content, s string) { c.Sources[0].URL = "https://example.com/" + s }},
		{"minutes basis note", false, func(c *draft.Content, s string) { c.Estimates.ActiveMinutes.Basis.Note = s }},
		{"level basis note", false, func(c *draft.Content, s string) { c.Estimates.Attention.Basis.Note = s }},
		{"phase estimate basis note", false, func(c *draft.Content, s string) { c.PhaseEstimates.CookTonight.TotalMinutes.Basis.Note = s }},
		{"guidance text", true, func(c *draft.Content, s string) { c.Guidance[0].Text = s }},
		{"guidance basis note", false, func(c *draft.Content, s string) { c.Guidance[0].Basis.Note = s }},
	}
	for _, field := range fields {
		for _, r := range []rune{
			'\u200e', '\u200f', '\u061c', // LRM, RLM, ALM
			'\u200b', '\u200c', '\u200d', '\u00ad', // ZWSP, ZWNJ, ZWJ, SHY
			'\u202a', '\u202b', '\u202c', '\u202d', '\u202e', // embeddings, PDF, overrides
			'\u2066', '\u2067', '\u2068', '\u2069', // isolates and PDI
			'\u2028', '\u2029', '\ufeff', '\n', '\r', '\t',
		} {
			t.Run(fmt.Sprintf("%s/U+%04X", field.name, r), func(t *testing.T) {
				c := decodeFixture(t, "valid_full.json")
				field.set(&c, "a"+string(r)+"b")
				allowed := r == '\u200e' || r == '\u200f' || r == '\u061c' ||
					r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\u00ad' ||
					(field.multiline && (r == '\u2028' || r == '\u2029' || r == '\ufeff' || r == '\n' || r == '\r' || r == '\t'))
				res := draft.Validate(c)
				if res.StructurallyValid != allowed {
					t.Fatalf("valid=%v want=%v errors=%v", res.StructurallyValid, allowed, res.Errors)
				}
				if !allowed && (len(res.Errors) != 1 || res.Errors[0].Code != draft.IssueInvalidChars) {
					t.Fatalf("want invalid_characters, got %v", res.Errors)
				}
			})
		}
	}
}

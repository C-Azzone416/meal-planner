package localstore

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

func TestStoredDraftUsesCreateValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		mod  func(*draft.Content)
		code string // empty means valid (warnings do not make a file corrupt)
	}{
		{"full", func(c *draft.Content) {}, ""},
		{"minimal with warnings", func(c *draft.Content) { *c = minimal("T") }, ""},
		{"title at limit", func(c *draft.Content) { c.Title = strings.Repeat("😀", draft.MaxTitleRunes) }, ""},
		{"empty title", func(c *draft.Content) { c.Title = "" }, draft.IssueRequired},
		{"blank title", func(c *draft.Content) { c.Title = " \u2003 " }, draft.IssueRequired},
		{"title over limit", func(c *draft.Content) { c.Title = strings.Repeat("😀", draft.MaxTitleRunes+1) }, draft.IssueTooLong},
		{"phase", func(c *draft.Content) { c.Instructions[0].Phase = "invalid" }, draft.IssueInvalidValue},
		{"prep ahead needs basis", func(c *draft.Content) { c.Instructions[0].PhaseBasis = nil }, draft.IssueBasisRequired},
		{"basis without phase", func(c *draft.Content) { c.Instructions[0].Phase = "" }, draft.IssueInvalidValue},
		{"source kind", func(c *draft.Content) { c.Sources[0].Kind = "invalid" }, draft.IssueInvalidValue},
		{"source id", func(c *draft.Content) { c.Sources[0].ID = "INVALID" }, draft.IssueInvalidValue},
		{"duplicate source id", func(c *draft.Content) { c.Sources = append(c.Sources, c.Sources[0]) }, draft.IssueDuplicateID},
		{"javascript URL", func(c *draft.Content) { c.Sources[0].URL = "javascript:alert(1)" }, draft.IssueInvalidValue},
		{"non URL text", func(c *draft.Content) { c.Sources[0].URL = "not a URL" }, draft.IssueInvalidValue},
		{"missing URL", func(c *draft.Content) { c.Sources[0].URL = "" }, draft.IssueRequired},
		{"relative URL", func(c *draft.Content) { c.Sources[0].URL = "/recipe" }, draft.IssueInvalidValue},
		{"URL credentials", func(c *draft.Content) { c.Sources[0].URL = "https://user:password@example.com/" }, draft.IssueInvalidValue},
		{"non HTTP scheme", func(c *draft.Content) { c.Sources[0].URL = "ftp://example.com/" }, draft.IssueInvalidValue},
		{"URL with space", func(c *draft.Content) { c.Sources[0].URL = "https://example.com/a b" }, draft.IssueInvalidValue},
		{"URL over limit", func(c *draft.Content) {
			c.Sources[0].URL = "https://example.com/" + strings.Repeat("a", draft.MaxURLBytes)
		}, draft.IssueTooLong},
		{"HTTP URL", func(c *draft.Content) { c.Sources[0].URL = "http://example.com/" }, ""},
		{"non URL source with URL", func(c *draft.Content) { c.Sources[0].Kind = draft.SourcePublication }, draft.IssueInvalidValue},
		{"non URL source without label or locator", func(c *draft.Content) { c.Sources[0] = draft.Source{ID: "s1", Kind: draft.SourcePublication} }, draft.IssueRequired},
		{"negative minutes", func(c *draft.Content) { c.Estimates.ActiveMinutes.Value = intp(-1) }, draft.IssueInvalidValue},
		{"minutes over limit", func(c *draft.Content) { c.Estimates.ActiveMinutes.Value = intp(draft.MaxMinutes + 1) }, draft.IssueInvalidValue},
		{"unknown minutes", func(c *draft.Content) { c.Estimates.ActiveMinutes.Value = nil }, draft.IssueRequired},
		{"minutes zero", func(c *draft.Content) { c.Estimates.ActiveMinutes.Value = intp(0) }, ""},
		{"minutes at limit with warning", func(c *draft.Content) { c.Estimates.ActiveMinutes.Value = intp(draft.MaxMinutes) }, ""},
		{"phase estimate negative minutes", func(c *draft.Content) { c.PhaseEstimates.CookTonight.TotalMinutes.Value = intp(-1) }, draft.IssueInvalidValue},
		{"level", func(c *draft.Content) { c.Estimates.Attention.Value = "invalid" }, draft.IssueInvalidValue},
		{"basis kind", func(c *draft.Content) { c.Estimates.ActiveMinutes.Basis.Kind = "invalid" }, draft.IssueInvalidValue},
		{"estimate needs basis", func(c *draft.Content) { c.Estimates.ActiveMinutes.Basis = nil }, draft.IssueBasisRequired},
		{"unknown source ref", func(c *draft.Content) { c.Estimates.ActiveMinutes.Basis.SourceRef = "missing" }, draft.IssueUnknownSourceRef},
		{"guidance topic", func(c *draft.Content) { c.Guidance[0].Topic = "invalid" }, draft.IssueInvalidValue},
		{"guidance needs basis", func(c *draft.Content) { c.Guidance[0].Basis = nil }, draft.IssueBasisRequired},
		{"blank ingredient", func(c *draft.Content) { c.Ingredients[0].Text = " " }, draft.IssueRequired},
		{"blank instruction", func(c *draft.Content) { c.Instructions[0].Text = " " }, draft.IssueRequired},
		{"blank guidance", func(c *draft.Content) { c.Guidance[0].Text = " " }, draft.IssueRequired},
		{"yield over limit", func(c *draft.Content) { c.Yield = strings.Repeat("x", draft.MaxYieldRunes+1) }, draft.IssueTooLong},
		{"too many ingredients", func(c *draft.Content) {
			for len(c.Ingredients) <= draft.MaxIngredients {
				c.Ingredients = append(c.Ingredients, c.Ingredients[0])
			}
		}, draft.IssueTooManyItems},
		{"content bytes over limit", func(c *draft.Content) {
			c.Instructions = make([]draft.Step, 20)
			for i := range c.Instructions {
				c.Instructions[i].Text = strings.Repeat("😀", draft.MaxStepRunes)
			}
		}, draft.IssueDraftTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fullContent()
			tc.mod(&c)
			validation := draft.Validate(c)
			if tc.code == "" {
				if !validation.StructurallyValid {
					t.Fatalf("invalid positive fixture: %v", validation.Errors)
				}
			} else {
				found := false
				for _, issue := range validation.Errors {
					found = found || issue.Code == tc.code
				}
				if !found {
					t.Fatalf("want create error %s, got %v", tc.code, validation.Errors)
				}
			}
			s, root := newStore(t)
			original := mkDraft(testID(1), 1, minimal("original"))
			mustCreate(t, s, original)
			raw, err := encode(mkDraft(original.ID, 1, c))
			if err != nil || len(raw) > draft.MaxStoredFileBytes {
				t.Fatalf("fixture encode: %v (%d bytes)", err, len(raw))
			}
			path := filepath.Join(root, "drafts", original.ID+".json")
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := s.Get(ctxb(), original.ID)
			if tc.code == "" {
				if err != nil || !reflect.DeepEqual(got.Content, normalize(mkDraft(original.ID, 1, c)).Content) {
					t.Fatalf("Get = %+v, %v", got, err)
				}
			} else {
				wantErr(t, err, draft.CategoryCorrupt, draft.CodeInvalidContent, root)
			}
			list, err := s.List(ctxb())
			if err != nil {
				t.Fatal(err)
			}
			if tc.code == "" {
				if len(list.Drafts) != 1 || len(list.Skipped) != 0 {
					t.Fatalf("List = %+v", list)
				}
			} else {
				if len(list.Drafts) != 0 || len(list.Skipped) != 1 || list.Skipped[0].Category != draft.CategoryCorrupt {
					t.Fatalf("List = %+v", list)
				}
				err := s.Replace(ctxb(), original.ID, 1, mkDraft(original.ID, 2, minimal("replacement")))
				wantErr(t, err, draft.CategoryCorrupt, draft.CodeInvalidContent, root)
			}
			if !bytes.Equal(readFile(t, path), raw) {
				t.Fatal("read/list/failed replacement changed file bytes")
			}
		})
	}
}

func TestStoredTextCharacterPolicy(t *testing.T) {
	for _, multiline := range []bool{false, true} {
		for _, r := range []rune{'\u200e', '\u200f', '\u061c', '\u200b', '\u200c', '\u200d', '\u00ad', '\u202a', '\u202b', '\u202c', '\u202d', '\u202e', '\u2066', '\u2067', '\u2068', '\u2069', '\u2028', '\u2029', '\ufeff', '\n', '\r', '\t'} {
			t.Run(fmt.Sprintf("multiline=%v/U+%04X", multiline, r), func(t *testing.T) {
				c := minimal("a" + string(r) + "b")
				if multiline {
					c.Notes, c.Title = c.Title, "T"
				}
				raw, err := encode(mkDraft(testID(1), 1, c))
				if err != nil {
					t.Fatal(err)
				}
				got, err := decode(raw, testID(1))
				allowed := r == '\u200e' || r == '\u200f' || r == '\u061c' || r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\u00ad' ||
					(multiline && (r == '\u2028' || r == '\u2029' || r == '\ufeff' || r == '\n' || r == '\r' || r == '\t'))
				if allowed {
					if err != nil || got.Title != c.Title || got.Notes != c.Notes {
						t.Fatalf("decode = %+v, %v", got, err)
					}
				} else {
					wantErr(t, err, draft.CategoryCorrupt, draft.CodeInvalidContent, "")
				}
			})
		}
	}
}

func TestDirectionalMarksRoundTrip(t *testing.T) {
	s, root := newStore(t)
	service := draft.NewService(s)
	const rtl = "שלום\u200e עולם\u200f مرحبا\u061c"
	c := fullContent()
	c.Title, c.Notes, c.Sources[0].Label = rtl, rtl+"\n"+rtl, rtl
	created, err := service.Create(ctxb(), c)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.Update(ctxb(), created.Draft.ID, 1, c)
	if err != nil || updated.Draft.Revision != 2 {
		t.Fatalf("Update = %+v, %v", updated, err)
	}
	reopened, err := Open(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Get(ctxb(), created.Draft.ID)
	if err != nil || !reflect.DeepEqual(got.Content, updated.Draft.Content) {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	list, err := reopened.List(ctxb())
	if err != nil || len(list.Drafts) != 1 || list.Drafts[0].Title != rtl || len(list.Skipped) != 0 {
		t.Fatalf("List = %+v, %v", list, err)
	}
}

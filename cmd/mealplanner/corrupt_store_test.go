package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

func TestShowAndListRejectAmbiguousStoredDraft(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	seed(t, root, `{"title":"Good"}`)
	const id = "drf_aaaaaaaaaaaaaaaaaaaa"
	const raw = `{"schema_version":1,"id":"` + id + `","revision":1,"status":"draft","title":"first","t\u0069tle":"second"}`
	if err := os.WriteFile(filepath.Join(root, "drafts", id+".json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, root)

	show := invoke(t, "", nil, "draft", "show", id, "--data-dir", root, "--json")
	if show.code != 3 || show.stdout != "" || show.err == "" {
		t.Errorf("show = %+v, want operational failure", show)
	}
	list := invoke(t, "", nil, "draft", "list", "--data-dir", root, "--json")
	var page draft.ListPage
	onlyJSON(t, list.stdout, &page)
	if list.code != 0 || page.Total != 1 || page.SkippedTotal != 1 ||
		len(page.Skipped) != 1 || page.Skipped[0].ID != id || page.Skipped[0].Category != draft.CategoryCorrupt {
		t.Errorf("list code=%d, page=%+v", list.code, page)
	}
	sameSnapshot(t, before, snapshot(t, root))
}

func TestShowAndListRejectInvalidStoredContent(t *testing.T) {
	for name, fields := range map[string]string{
		"blank title":         `"title":"   "`,
		"long title":          `"title":"` + strings.Repeat("a", draft.MaxTitleRunes+1) + `"`,
		"phase":               `"title":"T","instructions":[{"text":"step","phase":"invalid"}]`,
		"javascript URL":      `"title":"T","sources":[{"id":"s","kind":"url","url":"javascript:alert(1)"}]`,
		"non URL text":        `"title":"T","sources":[{"id":"s","kind":"url","url":"not a URL"}]`,
		"negative minutes":    `"title":"T","estimates":{"total_minutes":{"value":-1,"basis":{"kind":"agent_estimate"}}}`,
		"line separator":      `"title":"T\u2028"`,
		"paragraph separator": `"title":"T\u2029"`,
		"BOM":                 `"title":"T\ufeff"`,
	} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "data")
			seed(t, root, `{"title":"Good"}`)
			const id = "drf_aaaaaaaaaaaaaaaaaaaa"
			raw := `{"schema_version":1,"id":"` + id + `","revision":1,"status":"draft",` + fields + "}"
			if err := os.WriteFile(filepath.Join(root, "drafts", id+".json"), []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			before := snapshot(t, root)
			for _, asJSON := range []bool{false, true} {
				args := []string{"draft", "show", id, "--data-dir", root}
				if asJSON {
					args = append(args, "--json")
				}
				show := invoke(t, "", nil, args...)
				if show.code != 3 || show.stdout != "" || !strings.Contains(show.err, "the draft file has invalid content") {
					t.Fatalf("show = %+v", show)
				}
			}
			list := invoke(t, "", nil, "draft", "list", "--data-dir", root, "--json")
			var page draft.ListPage
			onlyJSON(t, list.stdout, &page)
			if list.code != 0 || page.Total != 1 || page.SkippedTotal != 1 || len(page.Skipped) != 1 || page.Skipped[0].ID != id || page.Skipped[0].Category != draft.CategoryCorrupt {
				t.Fatalf("list = %+v, page = %+v", list, page)
			}
			sameSnapshot(t, before, snapshot(t, root))
		})
	}
}
